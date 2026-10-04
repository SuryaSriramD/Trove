package integration

import (
	"context"
	"path/filepath"
	"testing"

	"trove.local/phase0/internal/workspace"
)

func TestP0PublicServiceJourney(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workspace")
	statePath := filepath.Join(root, "state.json")
	k, err := workspace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if result := RunP0(context.Background(), k, k.OwnerContext(), "p0-create", statePath); !result.Passed {
		t.Fatalf("create failed: %+v", result)
	}
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	k, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if result := RunP0(context.Background(), k, k.OwnerContext(), "p0-verify", statePath); !result.Passed {
		t.Fatalf("restart/restore failed: %+v", result)
	}
}
