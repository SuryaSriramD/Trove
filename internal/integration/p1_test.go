package integration

import (
	"context"
	"path/filepath"
	"testing"
	"trove.local/phase0/internal/workspace"
)

func TestP1PublicServiceJourney(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workspace")
	state := filepath.Join(root, "p1-state.json")
	k, err := workspace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if r := RunP1(context.Background(), k, k.OwnerContext(), "p1-create", state); !r.Passed {
		t.Fatalf("create: %+v", r)
	}
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	k, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if r := RunP1(context.Background(), k, k.OwnerContext(), "p1-verify", state); !r.Passed {
		t.Fatalf("restart/restore: %+v", r)
	}
}
