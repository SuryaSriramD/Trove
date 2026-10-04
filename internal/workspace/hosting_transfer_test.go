package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestP2VerifiedOwnerTransfer(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "desktop")
	destination := filepath.Join(root, "host")
	k, err := Create(source)
	if err != nil {
		t.Fatal(err)
	}
	e := k.OwnerContext()
	page := createPage(t, k, e, "Persist across owner transfer")
	wid := k.ID()
	if _, err = TransferWorkspace(context.Background(), source, destination); err == nil {
		t.Fatal("transferred an active owner")
	}
	k.Close()
	result, err := TransferWorkspace(context.Background(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	resolvedDestination, _ := filepath.EvalSymlinks(destination)
	if result.Destination != resolvedDestination || result.WorkspaceID != wid {
		t.Fatalf("wrong transfer result: %+v", result)
	}
	if old, e := Open(source); e == nil {
		old.Close()
		t.Fatal("old owner reopened after transfer")
	}
	target, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if readPage(t, target, target.OwnerContext(), page)["page"].(map[string]any)["title"] != "Persist across owner transfer" {
		t.Fatal("lost canonical content")
	}
	if _, err = os.Stat(filepath.Join(destination, "owner-transfer.json")); err != nil {
		t.Fatal("missing provenance receipt")
	}
	if _, err = TransferWorkspace(context.Background(), source, filepath.Join(root, "replay")); err == nil {
		t.Fatal("replayed transfer")
	}
}
func TestP2TransferRefusesUnsafeDestinations(t *testing.T) {
	k, _ := testKernel(t)
	source := k.Path()
	k.Close()
	if _, err := TransferWorkspace(context.Background(), source, filepath.Join(source, "child")); err == nil {
		t.Fatal("nested transfer accepted")
	}
	destination := filepath.Join(t.TempDir(), "occupied")
	os.Mkdir(destination, 0700)
	if _, err := TransferWorkspace(context.Background(), source, destination); err == nil {
		t.Fatal("overwrote destination")
	}
	if reopened, err := Open(source); err != nil {
		t.Fatal("failed preflight fenced source")
	} else {
		reopened.Close()
	}
}

func TestP2TransferRetainsRecoveryWhenFenceSyncFails(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	k, err := Create(source)
	if err != nil {
		t.Fatal(err)
	}
	createPage(t, k, k.OwnerContext(), "Retain me after a failed fence fsync")
	k.Close()
	destination := filepath.Join(t.TempDir(), "target")
	_, err = transferWorkspaceWithFence(context.Background(), source, destination, func(path string, raw []byte) error {
		if e := os.WriteFile(path, raw, 0600); e != nil {
			return e
		}
		return errors.New("injected fence fsync failure")
	})
	if err == nil {
		t.Fatal("accepted failed fence persistence")
	}
	raw, err := os.ReadFile(filepath.Join(source, "OWNER_TRANSFERRED"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		RecoveryStage string `json:"recoveryStage"`
	}
	if err = json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBackup(context.Background(), filepath.Join(receipt.RecoveryStage, "snapshot")); err != nil {
		t.Fatal("verified recovery snapshot was lost", err)
	}
	if owner, e := Open(source); e == nil {
		owner.Close()
		t.Fatal("partially fenced owner reopened")
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("published before a durable source fence")
	}
}
