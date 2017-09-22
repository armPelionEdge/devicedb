package routes_test

import (
    "context"

    . "devicedb/bucket"
    . "devicedb/cluster"
    . "devicedb/data"
    . "devicedb/raft"
)

type MockClusterFacade struct {
    defaultAddNodeResponse error
    defaultRemoveNodeResponse error
    defaultReplaceNodeResponse error
    defaultDecommissionPeerResponse error
    defaultDecommissionResponse error
    localNodeID uint64
    defaultPeerAddress PeerAddress
    defaultAddRelayResponse error
    defaultRemoveRelayResponse error
    defaultMoveRelayResponse error
    defaultAddSiteResponse error
    defaultRemoveSiteResponse error
    defaultBatchResponse error
    defaultLocalBatchResponse error
    defaultGetResponse []*SiblingSet
    defaultGetResponseError error
    defaultLocalGetResponse []*SiblingSet
    defaultLocalGetResponseError error
    defaultGetMatchesResponse SiblingSetIterator
    defaultGetMatchesResponseError error
    defaultLocalGetMatchesResponse SiblingSetIterator
    defaultLocalGetMatchesResponseError error
    addNodeCB func(ctx context.Context, nodeConfig NodeConfig)
    replaceNodeCB func(ctx context.Context, nodeID uint64, replacementNodeID uint64)
    removeNodeCB func(ctx context.Context, nodeID uint64)
    decommisionCB func()
    decommisionPeerCB func(nodeID uint64)
    localBatchCB func(partition uint64, siteID string, bucket string, updateBatch *UpdateBatch)
    localGetCB func(partition uint64, siteID string, bucket string, keys [][]byte)
    localGetMatchesCB func(partition uint64, siteID string, bucket string, keys [][]byte)
}

func (clusterFacade *MockClusterFacade) AddNode(ctx context.Context, nodeConfig NodeConfig) error {
    if clusterFacade.addNodeCB != nil {
        clusterFacade.addNodeCB(ctx, nodeConfig)
    }

    return clusterFacade.defaultAddNodeResponse
}

func (clusterFacade *MockClusterFacade) RemoveNode(ctx context.Context, nodeID uint64) error {
    if clusterFacade.removeNodeCB != nil {
        clusterFacade.removeNodeCB(ctx, nodeID)
    }

    return clusterFacade.defaultRemoveNodeResponse
}

func (clusterFacade *MockClusterFacade) ReplaceNode(ctx context.Context, nodeID uint64, replacementNodeID uint64) error {
    if clusterFacade.replaceNodeCB != nil {
        clusterFacade.replaceNodeCB(ctx, nodeID, replacementNodeID)
    }

    return clusterFacade.defaultReplaceNodeResponse
}

func (clusterFacade *MockClusterFacade) Decommission() error {
    if clusterFacade.decommisionCB != nil {
        clusterFacade.decommisionCB()
    }

    return clusterFacade.defaultDecommissionResponse
}

func (clusterFacade *MockClusterFacade) DecommissionPeer(nodeID uint64) error {
    if clusterFacade.decommisionPeerCB != nil {
        clusterFacade.decommisionPeerCB(nodeID)
    }

    return clusterFacade.defaultDecommissionPeerResponse
}

func (clusterFacade *MockClusterFacade) LocalNodeID() uint64 {
    return clusterFacade.localNodeID
}

func (clusterFacade *MockClusterFacade) PeerAddress(nodeID uint64) PeerAddress {
    return clusterFacade.defaultPeerAddress
}

func (clusterFacade *MockClusterFacade) AddRelay(ctx context.Context, relayID string) error {
    return clusterFacade.defaultAddRelayResponse
}

func (clusterFacade *MockClusterFacade) RemoveRelay(ctx context.Context, relayID string) error {
    return clusterFacade.defaultRemoveRelayResponse
}

func (clusterFacade *MockClusterFacade) MoveRelay(ctx context.Context, relayID string, siteID string) error {
    return clusterFacade.defaultMoveRelayResponse
}

func (clusterFacade *MockClusterFacade) AddSite(ctx context.Context, siteID string) error {
    return clusterFacade.defaultAddSiteResponse
}

func (clusterFacade *MockClusterFacade) RemoveSite(ctx context.Context, siteID string) error {
    return clusterFacade.defaultRemoveSiteResponse
}

func (clusterFacade *MockClusterFacade) Batch(siteID string, bucket string, updateBatch *UpdateBatch) error {
    return clusterFacade.defaultBatchResponse
}

func (clusterFacade *MockClusterFacade) LocalBatch(partition uint64, siteID string, bucket string, updateBatch *UpdateBatch) error {
    if clusterFacade.localBatchCB != nil {
        clusterFacade.localBatchCB(partition, siteID, bucket, updateBatch)
    }

    return clusterFacade.defaultLocalBatchResponse
}

func (clusterFacade *MockClusterFacade) Get(siteID string, bucket string, keys [][]byte) ([]*SiblingSet, error) {
    return clusterFacade.defaultGetResponse, clusterFacade.defaultGetResponseError
}

func (clusterFacade *MockClusterFacade) LocalGet(partition uint64, siteID string, bucket string, keys [][]byte) ([]*SiblingSet, error) {
    if clusterFacade.localGetCB != nil {
        clusterFacade.localGetCB(partition, siteID, bucket, keys)
    }

    return clusterFacade.defaultLocalGetResponse, clusterFacade.defaultLocalGetResponseError
}

func (clusterFacade *MockClusterFacade) GetMatches(siteID string, bucket string, keys [][]byte) (SiblingSetIterator, error) {
    return clusterFacade.defaultGetMatchesResponse, clusterFacade.defaultLocalGetMatchesResponseError
}

func (clusterFacade *MockClusterFacade) LocalGetMatches(partition uint64, siteID string, bucket string, keys [][]byte) (SiblingSetIterator, error) {
    if clusterFacade.localGetMatchesCB != nil {
        clusterFacade.localGetMatchesCB(partition, siteID, bucket, keys)
    }

    return clusterFacade.defaultLocalGetMatchesResponse, clusterFacade.defaultLocalGetMatchesResponseError
}

type siblingSetIteratorEntry struct {
    Prefix []byte
    Key []byte
    Value *SiblingSet
    Error error
}

type MemorySiblingSetIterator struct {
    entries []*siblingSetIteratorEntry
    nextEntry *siblingSetIteratorEntry
}

func NewMemorySiblingSetIterator() *MemorySiblingSetIterator {
    return &MemorySiblingSetIterator{
        entries: make([]*siblingSetIteratorEntry, 0),
    }
}

func (iter *MemorySiblingSetIterator) AppendNext(prefix []byte, key []byte, value *SiblingSet, err error) {
    iter.entries = append(iter.entries, &siblingSetIteratorEntry{
        Prefix: prefix,
        Key: key,
        Value: value,
        Error: err,
    })
}

func (iter *MemorySiblingSetIterator) Next() bool {
    iter.nextEntry = nil

    if len(iter.entries) == 0 {
        return false
    }

    iter.nextEntry = iter.entries[0]
    iter.entries = iter.entries[1:]

    if iter.nextEntry.Error != nil {
        return false
    }

    return true
}

func (iter *MemorySiblingSetIterator) Prefix() []byte {
    return iter.nextEntry.Prefix
}

func (iter *MemorySiblingSetIterator) Key() []byte {
    return iter.nextEntry.Key
}

func (iter *MemorySiblingSetIterator) Value() *SiblingSet {
    return iter.nextEntry.Value
}

func (iter *MemorySiblingSetIterator) Release() {
}

func (iter *MemorySiblingSetIterator) Error() error {
    if iter.nextEntry == nil {
        return nil
    }
    
    return iter.nextEntry.Error
}