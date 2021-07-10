package node
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
	"os"
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"path"
	"sync"

	. "github.com/PelionIoT/devicedb/error"
	. "github.com/PelionIoT/devicedb/logging"
	. "github.com/PelionIoT/devicedb/storage"
)


type Snapshotter struct {
	nodeID uint64
	snapshotsDirectory string
	storageDriver StorageDriver
	ongoingSnapshots map[string]bool
	mu sync.Mutex
}

func (snapshotter *Snapshotter) lazyInit() {
	snapshotter.mu.Lock()
	defer snapshotter.mu.Unlock()	
	
	if snapshotter.ongoingSnapshots != nil {
		return
	}

	snapshotter.ongoingSnapshots = make(map[string]bool)
}

func (snapshotter *Snapshotter) Snapshot(snapshotIndex uint64, snapshotId string) error {
	snapshotter.lazyInit()

	Log.Infof("Local node (id = %d) taking a snapshot of its storage state for a consistent cluster snapshot (id = %s)", snapshotter.nodeID, snapshotId)

    snapshotDir := snapshotter.snapshotsDirectory

    if snapshotDir == "" {
        Log.Warningf("Cannot take snapshot because no snapshot directory is configured")

        return ESnapshotsNotEnabled
    }
    
    snapshotDir = path.Join(snapshotDir, fmt.Sprintf("snapshot-%s-%d", snapshotId, snapshotter.nodeID))

	snapshotter.startSnapshot(snapshotId)
	defer snapshotter.stopSnapshot(snapshotId)

    if err := snapshotter.storageDriver.Snapshot(snapshotDir, []byte{ SnapshotMetadataPrefix }, map[string]string{ SnapshotUUIDKey: snapshotId }); err != nil {
        Log.Errorf("Unable to create a snapshot of node storage at %s: %v", snapshotDir, err)

        return err
	}
	
	Log.Infof("Local node (id = %d) created a snapshot of its local state (id = %s) at %s", snapshotter.nodeID, snapshotId, snapshotDir)

    return nil
}

func (snapshotter *Snapshotter) startSnapshot(snapshotId string) {
	snapshotter.mu.Lock()
	defer snapshotter.mu.Unlock()

	snapshotter.ongoingSnapshots[snapshotId] = true
}

func (snapshotter *Snapshotter) stopSnapshot(snapshotId string) {
	snapshotter.mu.Lock()
	defer snapshotter.mu.Unlock()

	delete(snapshotter.ongoingSnapshots, snapshotId)
}

func (snapshotter *Snapshotter) isSnapshotInProgress(snapshotId string) bool {
	snapshotter.mu.Lock()
	defer snapshotter.mu.Unlock()

	if snapshotter.ongoingSnapshots == nil {
		return false
	}

	return snapshotter.ongoingSnapshots[snapshotId]
}

func (snapshotter *Snapshotter) CheckSnapshotStatus(snapshotId string) error {
	snapshotDir := snapshotter.snapshotsDirectory

    if snapshotDir == "" {
        Log.Warningf("Cannot take snapshot because no snapshot directory is configured")

        return ESnapshotsNotEnabled
    }
    
	snapshotDir = path.Join(snapshotDir, fmt.Sprintf("snapshot-%s-%d", snapshotId, snapshotter.nodeID))
	
	if snapshotter.isSnapshotInProgress(snapshotId) {
		return ESnapshotInProgress
	}

	snapshotStorage, err := snapshotter.storageDriver.OpenSnapshot(snapshotDir)

	if err != nil {
		Log.Warningf("Unable to open snapshot at %s: %v", snapshotDir, err)

		return ESnapshotOpenFailed
	}

	defer snapshotStorage.Close()	

	snapshotMetadata := NewPrefixedStorageDriver([]byte{ SnapshotMetadataPrefix }, snapshotStorage)
	values, err := snapshotMetadata.Get([][]byte{ []byte(SnapshotUUIDKey) })

	if err != nil {
		Log.Warningf("Unable to read snapshot metadata at %s: %v", snapshotDir, err)
		
		return ESnapshotReadFailed
	}

	if len(values[0]) == 0 {
		Log.Warningf("Snapshot metadata incomplete at %s", snapshotDir)

		return ESnapshotReadFailed
	}

	if string(values[0]) != snapshotId {
		Log.Warningf("Snapshot metadata UUID mismatch at %s: %s != %s", snapshotDir, snapshotId, string(values[0]))
		
		return ESnapshotReadFailed
	}

	return nil
}

func (snapshotter *Snapshotter) WriteSnapshot(snapshotId string, w io.Writer) error {
	snapshotDir := path.Join(snapshotter.snapshotsDirectory, fmt.Sprintf("snapshot-%s-%d", snapshotId, snapshotter.nodeID))

	return writeSnapshot(snapshotDir, w)
}

func writeSnapshot(snapshotDirectory string, w io.Writer) error {
	files, err := ioutil.ReadDir(snapshotDirectory)

	if err != nil {
		return err
	}
	
	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, file := range files {
		err := tw.WriteHeader(&tar.Header{
			Name: file.Name(),
			Mode: int64(file.Mode()),
			Size: file.Size(),
		})

		if err != nil {
			return err
		}

		filePath := path.Join(snapshotDirectory, file.Name())
		fileReader, err := os.Open(filePath)

		if err != nil {
			return err
		}

		defer fileReader.Close()		

		_, err = io.Copy(tw, fileReader)

		if err != nil {
			return err
		}
	}

	return nil
}