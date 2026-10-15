package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"trove.local/phase0/internal/native"
)

func TestModelOperationCancellationWaitsForCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const id = "01990000-0000-7000-8000-000000000105"
	n := &nativeState{cancellations: map[string]context.CancelFunc{id: cancel}}
	started := n.startModelOperationLocked(id, "acquiring")
	cancelled, ok := n.cancelModelOperation(id)
	if !ok || ctx.Err() == nil || cancelled.State != "cancellation_requested" || cancelled.Revision <= started.Revision {
		t.Fatal("cancellation not retained", cancelled)
	}
	copied := n.updateModelOperation(id, "acquiring", 128, "")
	if copied.State != "cancellation_requested" || copied.Bytes != 128 || n.cancellations[id] == nil {
		t.Fatal("copy progress cleared pending cancellation", copied)
	}
	completed := n.updateModelOperation(id, "interrupted", -1, "cancelled")
	if completed.Revision <= copied.Revision || completed.Bytes != 128 || n.cancellations[id] != nil {
		t.Fatal("completion not acknowledged", completed)
	}
	raced, ok := n.cancelModelOperation(id)
	if !ok || raced != completed {
		t.Fatal("late cancellation rewound completion", raced)
	}
	n.cancellations[id] = cancel
	restarted := n.startModelOperationLocked(id, "acquiring")
	if restarted.AcquisitionID == started.AcquisitionID || restarted.Revision <= completed.Revision || restarted.Bytes != 0 {
		t.Fatal("restart did not receive a fresh identity", restarted)
	}
}
func TestModelListRetainsActiveOperationAndBlocksEarlyRemoval(t *testing.T) {
	manager, err := native.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const id = "01990000-0000-7000-8000-000000000105"
	a := NewApp()
	a.native = &nativeState{manager: manager, cancellations: map[string]context.CancelFunc{id: cancel}}
	a.native.startModelOperationLocked(id, "importing")
	var listed struct {
		Result struct {
			Operations []modelOperation `json:"operations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(a.nativeDispatch("models.list", nil)), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Operations) != 1 || listed.Result.Operations[0].State != "importing" {
		t.Fatal("view reopening loses active operation", listed)
	}
	var removed struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(a.nativeDispatch("models.remove", json.RawMessage(`{"packId":"`+id+`"}`))), &removed); err != nil {
		t.Fatal(err)
	}
	if removed.OK {
		t.Fatal("removal admitted before acquisition worker claimed model")
	}
	if ctx.Err() != nil {
		t.Fatal("removal cancelled active acquisition")
	}
}

func TestModelOperationReadyCannotHideDamagedInstalledFile(t *testing.T) {
	data := []byte("GGUFstatus verification")
	hash := sha256.Sum256(data)
	p := native.Pack{ID: native.NewID(), SchemaVersion: 1, Name: "status fixture", Kind: "chat", Format: "gguf", SHA256: hex.EncodeToString(hash[:]), SizeBytes: int64(len(data)), License: "MIT", LicenseURL: "https://example.com/license", SourceRevision: "fixture", RuntimeCommit: native.LlamaCommit, Architecture: "portable", ContextTokens: 1024, EstimatedMemoryBytes: 1 << 20}
	root := t.TempDir()
	manager, err := native.NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Import(context.Background(), p, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.native = &nativeState{manager: manager}
	ready := a.native.updateModelOperation(p.ID, "ready", int64(len(data)), "")
	if err = os.WriteFile(filepath.Join(root, p.ID+".model"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Result struct {
			Operations []modelOperation `json:"operations"`
		} `json:"result"`
	}
	if err = json.Unmarshal([]byte(a.nativeDispatch("models.list", nil)), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Operations) != 1 || listed.Result.Operations[0].State != "corrupt" || listed.Result.Operations[0].Revision <= ready.Revision {
		t.Fatal("prior completion masked damaged installation", listed)
	}
	if err = os.WriteFile(filepath.Join(root, p.ID+".model"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(a.nativeDispatch("models.list", nil)), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Operations) != 1 || listed.Result.Operations[0].State != "ready" {
		t.Fatal("recovered installation retained stale corrupt status", listed)
	}
}

func TestModelDiskSnapshotCannotRewindConcurrentCompletion(t *testing.T) {
	n := &nativeState{}
	n.startModelOperationLocked("pack", "acquiring")
	observed := n.operationRevision
	done := n.updateModelOperation("pack", "ready", 42, "")
	// The empty disk listing began before this completion was published.
	listed := n.modelOperations(nil, observed)
	if len(listed) != 1 || listed[0] != done {
		t.Fatal("stale disk snapshot overwrote completed operation", listed)
	}
}
