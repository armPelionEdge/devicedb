package transfer
//
 // Copyright (c) 2019 ARM Limited.
 //
 // SPDX-License-Identifier: MIT
 //
 // Permission is hereby granted, free of charge, to any person obtaining a copy
 // of this software and associated documentation files (the "Software"), to
 // deal in the Software without restriction, including without limitation the
 // rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
 // sell copies of the Software, and to permit persons to whom the Software is
 // furnished to do so, subject to the following conditions:
 //
 // The above copyright notice and this permission notice shall be included in all
 // copies or substantial portions of the Software.
 //
 // THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 // IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 // FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 // AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 // LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 // OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 // SOFTWARE.
 //


import (
    "context"
    "sync"
    
    . "github.com/armPelionEdge/devicedb/cluster"
)

type PartitionTransferProposer interface {
    QueueTransferProposal(partition uint64, replica uint64, after <-chan int) <-chan error
    CancelTransferProposal(partition uint64, replica uint64)
    CancelTransferProposals(partition uint64)
    PendingProposals(partition uint64) int
    QueuedProposals() map[uint64]map[uint64]bool
}

type TransferProposer struct {
    configController ClusterConfigController
    transferCancelers map[uint64]map[uint64]*Canceler
    lock sync.Mutex
}

func NewTransferProposer(configController ClusterConfigController) *TransferProposer {
    return &TransferProposer{
        configController: configController,
        transferCancelers: make(map[uint64]map[uint64]*Canceler, 0),
    }
}

func (transferProposer *TransferProposer) QueueTransferProposal(partition uint64, replica uint64, after <-chan int) <-chan error {
    transferProposer.lock.Lock()
    defer transferProposer.lock.Unlock()

    result := make(chan error, 1)
    ctx, cancel := context.WithCancel(context.Background())
    canceler := &Canceler{ Cancel: cancel }

    if _, ok := transferProposer.transferCancelers[partition]; !ok {
        transferProposer.transferCancelers[partition] = make(map[uint64]*Canceler)
    }

    transferProposer.transferCancelers[partition][replica] = canceler

    go func() {
        // wait until the preceding operation finishes or context is cancelled
        select {
        case <-after:
        case <-ctx.Done():
            return
        }

        // Problem: All transfers are queued and proposed but ClusterCommand() does not return for some of them
        err := transferProposer.configController.ClusterCommand(ctx, ClusterTakePartitionReplicaBody{ NodeID: transferProposer.configController.ClusterController().LocalNodeID, Partition: partition, Replica: replica })

        transferProposer.lock.Lock()

        defer func() {
            transferProposer.lock.Unlock()
            result <- err
        }()

        if _, ok := transferProposer.transferCancelers[partition]; !ok {
            return
        }

        // It is possible that this proposal was cancelled but then one for the
        // same replica was started before this cleanup function was called. In
        // this case the map might contain a new canceller for a new proposal for
        // the same replica. This requires an equality check so this proposal doesn't
        // step on the toes of another one
        if transferProposer.transferCancelers[partition][replica] == canceler {
            delete(transferProposer.transferCancelers[partition], replica)

            if len(transferProposer.transferCancelers[partition]) == 0 {
                delete(transferProposer.transferCancelers, partition)
            }
        }
    }()

    return result
}

func (transferProposer *TransferProposer) CancelTransferProposal(partition uint64, replica uint64) {
    transferProposer.lock.Lock()
    defer transferProposer.lock.Unlock()

    transferProposer.cancelTransferProposal(partition, replica)
}

func (transferProposer *TransferProposer) cancelTransferProposal(partition uint64, replica uint64) {
    if cancelers, ok := transferProposer.transferCancelers[partition]; ok {
        if canceler, ok := cancelers[replica]; ok {
            canceler.Cancel()
        }

        delete(cancelers, replica)

        if len(cancelers) == 0 {
            delete(transferProposer.transferCancelers, partition)
        }
    }
}

func (transferProposer *TransferProposer) CancelTransferProposals(partition uint64) {
    transferProposer.lock.Lock()
    defer transferProposer.lock.Unlock()

    for replica, _ := range transferProposer.transferCancelers[partition] {
        transferProposer.cancelTransferProposal(partition, replica)
    }
}

func (transferProposer *TransferProposer) PendingProposals(partition uint64) int {
    transferProposer.lock.Lock()
    defer transferProposer.lock.Unlock()

    return len(transferProposer.transferCancelers[partition])
}

func (transferProposer *TransferProposer) QueuedProposals() map[uint64]map[uint64]bool {
    transferProposer.lock.Lock()
    defer transferProposer.lock.Unlock()

    allProposals := make(map[uint64]map[uint64]bool, 0)

    for partition, replicas := range transferProposer.transferCancelers {
        allProposals[partition] = make(map[uint64]bool, 0)

        for replica, _ := range replicas {
            allProposals[partition][replica] = true
        }
    }

    return allProposals
}