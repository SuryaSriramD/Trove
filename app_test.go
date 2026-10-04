package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestDesktopBootstrapUsesTrustedWorkspaceIdentity(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx = context.Background()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.completeStartup(nil)
	var response struct {
		OK     bool `json:"ok"`
		Result struct {
			WorkspaceID  string `json:"workspaceId"`
			ScopeGrantID string `json:"scopeGrantId"`
		} `json:"result"`
	}
	if err = json.Unmarshal([]byte(a.Dispatch("workspace.snapshot", "{}")), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Result.WorkspaceID != k.ID() || response.Result.ScopeGrantID != string(a.execution.ScopeGrantId) {
		t.Fatalf("invalid desktop bootstrap: %+v", response)
	}
}

func TestDesktopUploadDescriptorSurvivesTemporaryPathCleanup(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx = context.Background()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.completeStartup(nil)
	content := []byte("Pasted attachment — తెలుగు 中文 العربية 🌱")
	handle, err := a.StageUpload("../../clipboard.txt", "text/plain", base64.StdEncoding.EncodeToString(content))
	if err != nil {
		t.Fatal(err)
	}
	leftovers, err := filepath.Glob(filepath.Join(os.TempDir(), "trove-upload-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary upload path leaked: %v %v", leftovers, err)
	}
	request := contracts.ServicePrepareFileRequest{ContractVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), WorkspaceId: contracts.DomainId(k.ID()), SelectionHandle: contracts.DomainId(handle)}
	raw, _ := json.Marshal(request)
	var response contracts.ServicePrepareFileResponse
	if err = json.Unmarshal([]byte(a.Dispatch("prepareFile", string(raw))), &response); err != nil {
		t.Fatal(err)
	}
	success, ok := response.Value.(contracts.ServicePrepareFileResponseVariant1)
	if !ok {
		t.Fatalf("prepare after unlink failed: %#v", response.Value)
	}
	expected := sha256.Sum256(content)
	if string(success.Result.ContentHash) != hex.EncodeToString(expected[:]) {
		t.Fatal("uploaded bytes changed")
	}
}
