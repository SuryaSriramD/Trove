package main

import (
	"os"
	"path/filepath"
	"testing"

	"trove.local/phase0/internal/workspace"
)

func TestSelectedWorkspaceSurvivesRestartAndMissingDriveIsNotRecreated(t *testing.T) {
	root := t.TempDir()
	preference := filepath.Join(root, "settings", "workspace-selection.json")
	path := filepath.Join(root, "chosen-workspace")
	k, err := workspace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	id := k.ID()
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	if err = saveWorkspaceSelection(preference, path); err != nil {
		t.Fatal(err)
	}
	selected, err := selectedWorkspace(preference)
	if err != nil || selected != path {
		t.Fatalf("selected workspace: %q %v", selected, err)
	}
	k, err = workspace.Open(selected)
	if err != nil {
		t.Fatal(err)
	}
	if k.ID() != id {
		t.Fatal("workspace identity changed")
	}
	k.Close()
	if err = os.Rename(path, path+"-disconnected"); err != nil {
		t.Fatal(err)
	}
	if selected, err = selectedWorkspace(preference); err == nil || selected != "" {
		t.Fatal("missing selected workspace was silently accepted")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing workspace was recreated")
	}
}
