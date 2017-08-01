package cluster

import (
    "errors"
    "encoding/json"

    ddbRaft "devicedb/cloud/raft"
)

var ENoSuchPartition = errors.New("The specified partition does not exist")
var ENoSuchToken = errors.New("The specified token does not exist")
var ENoSuchReplica = errors.New("The specified partition replica does not exist")

type PartitionReplica struct {
    // The partition number. The partition number combined with the total number of partitions and the range of the hash
    // space define a contiguous range in the hash space for which this partition is responsible
    Partition uint64
    // The index of this partition replica. If the replication factor is set to 3 this number will range from 0 to 2
    // The 0th partition replica represents the primary replica for that partition. The owner of the primary replica
    // will be the only node able to accept writes for this partition. The other replicas for this partition serve
    // only as backups
    Replica uint64
    // The ID of the node that holds this partition replica. The holder can differ from the owner if the cluster is in
    // a transitional state and the partition replica is being transferred to a new node. The owner is based only on
    // the current token assignments
    Holder uint64
}

type NodeConfig struct {
    // The network address of the node
    Address ddbRaft.PeerAddress
    // Node capacity in bytes
    Capacity uint64
    // The tokens owned by this node
    Tokens map[uint64]bool
    // a set of partition replicas held by this node. This is derived from the cluster state and is used 
    // only internally for quick lookup. It is not stored or transferred as part of a node's configuration
    PartitionReplicas map[uint64]map[uint64]bool
}

func (nodeConfig *NodeConfig) takePartitionReplica(partition, replica uint64) {
    if _, ok := nodeConfig.PartitionReplicas[partition]; !ok {
        nodeConfig.PartitionReplicas[partition] = make(map[uint64]bool)
    }

    nodeConfig.PartitionReplicas[partition][replica] = true
}

func (nodeConfig *NodeConfig) relinquishPartitionReplica(partition, replica uint64) {
    replicas, ok := nodeConfig.PartitionReplicas[partition]

    if !ok {
        return
    }

    delete(replicas, replica)

    if len(replicas) == 0 {
        delete(nodeConfig.PartitionReplicas, partition)
    }
}

func (nodeConfig *NodeConfig) relinquishToken(token uint64) {
    delete(nodeConfig.Tokens, token)
}

func (nodeConfig *NodeConfig) takeToken(token uint64) {
    nodeConfig.Tokens[token] = true
}

type ClusterState struct {
    // Ring members and their configuration
    Nodes map[uint64]*NodeConfig
    // A mapping between tokens and the node that owns them
    Tokens []uint64
    // The partition replicas in this node
    Partitions [][]*PartitionReplica
    // Global cluster settings that must be initialized before the cluster is
    // initialized
    ClusterSettings ClusterSettings
}

func (clusterState *ClusterState) AddNode(nodeConfig NodeConfig) {
    if clusterState.Nodes == nil {
        // lazy initialization of nodes map
        clusterState.Nodes = make(map[uint64]*NodeConfig)
    }

    // node ID must be non-zero
    if nodeConfig.Address.NodeID == 0 {
        return
    }

    // ignore if this node is already added to the cluster
    if _, ok := clusterState.Nodes[nodeConfig.Address.NodeID]; ok {
        return
    }

    clusterState.Nodes[nodeConfig.Address.NodeID] = &nodeConfig
}

func (clusterState *ClusterState) RemoveNode(node uint64) {
    // ignore if this node doesnt exist in the cluster
    if _, ok := clusterState.Nodes[node]; !ok {
        return
    }

    // any partition that was held by this node is now held by nobody
    for partition, replicas := range clusterState.Nodes[node].PartitionReplicas {
        for replica, _ := range replicas {
            clusterState.Nodes[node].relinquishPartitionReplica(partition, replica)
            clusterState.Partitions[partition][replica].Holder = 0
        }
    }

    // any token that was owned by this node is now owned by nobody
    for token, _ := range clusterState.Nodes[node].Tokens {
        clusterState.Nodes[node].relinquishToken(token)
        clusterState.Tokens[token] = 0
    }
    
    delete(clusterState.Nodes, node)
}

// change the owner of a token
func (clusterState *ClusterState) AssignToken(node, token uint64) error {
    if token >= uint64(len(clusterState.Tokens)) {
        return ENoSuchToken
    }

    if _, ok := clusterState.Nodes[node]; !ok {
        return ENoSuchNode
    }

    currentOwner := clusterState.Tokens[token]

    if currentOwner != 0 {
        clusterState.Nodes[currentOwner].relinquishToken(token)
    }

    // invariant should be maintained that a token is owned by exactly one node at a time
    clusterState.Tokens[token] = node
    clusterState.Nodes[node].takeToken(token)

    return nil
}

// change the owner of a partition replica
func (clusterState *ClusterState) AssignPartitionReplica(partition, replica, node uint64) error {
    if partition >= uint64(len(clusterState.Partitions)) {
        return ENoSuchPartition
    }

    replicas := clusterState.Partitions[partition]
    _, okNode := clusterState.Nodes[node]

    if !okNode {
        return ENoSuchNode
    }

    if replica >= uint64(len(replicas)) {
        return ENoSuchReplica
    }

    currentHolder := replicas[replica].Holder

    if currentHolder != 0 {
        // invariant should be maintained that a partition replica is owned by exactly one node at a time
        clusterState.Nodes[currentHolder].relinquishPartitionReplica(partition, replica)
    }

    replicas[replica].Holder = node
    clusterState.Nodes[node].takePartitionReplica(partition, replica)

    return nil
}

func (clusterState *ClusterState) Initialize() {
    if !clusterState.ClusterSettings.AreInitialized() {
        return
    }

    clusterState.Tokens = make([]uint64, clusterState.ClusterSettings.Partitions)
    clusterState.Partitions = make([][]*PartitionReplica, clusterState.ClusterSettings.Partitions)

    for partition := 0; uint64(partition) < clusterState.ClusterSettings.Partitions; partition++ {
        clusterState.Partitions[partition] = make([]*PartitionReplica, clusterState.ClusterSettings.ReplicationFactor)

        for replica := 0; uint64(replica) < clusterState.ClusterSettings.ReplicationFactor; replica++ {
            clusterState.Partitions[partition][replica] = &PartitionReplica{
                Partition: uint64(partition),
                Replica: uint64(replica),
            }
        }
    }
}

func (clusterState *ClusterState) Snapshot() ([]byte, error) {
    return json.Marshal(clusterState)
}

func (clusterState *ClusterState) Recover(snapshot []byte) error {
    var cs ClusterState
    err := json.Unmarshal(snapshot, &cs)

    if err != nil {
        return err
    }

    *clusterState = cs

    return nil
}

type ClusterSettings struct {
    // The replication factor of this cluster
    ReplicationFactor uint64
    // The number of partitions in the hash space
    Partitions uint64
}

func (clusterSettings *ClusterSettings) AreInitialized() bool {
    return clusterSettings.ReplicationFactor != 0 && clusterSettings.Partitions != 0
}