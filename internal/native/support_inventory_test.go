package native

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSupportInventoryBoundedPassiveAndCancellation(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	p := Pack{SchemaVersion: 1, ID: "10000000-0000-7000-8000-000000000001", Name: "SECRET MODEL", Kind: "chat", Format: "gguf", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 4, License: "MIT", LicenseURL: "https://example.test/license", SourceRevision: "test", RuntimeCommit: LlamaCommit, Architecture: "portable", ContextTokens: 4096, EstimatedMemoryBytes: 4}
	raw, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(root, p.ID+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, p.ID+".model"), []byte("GGUF"), 0600); err != nil {
		t.Fatal(err)
	}
	n, err := m.SupportInstalledManifestCount(context.Background())
	if err != nil || n != 1 {
		t.Fatal(n, err)
	} // No integrity claim: digest intentionally does not match.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.SupportInstalledManifestCount(ctx); err == nil {
		t.Fatal("cancel ignored")
	}
	if err = os.Remove(filepath.Join(root, p.ID+".model")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "model")
	if err = os.WriteFile(outside, []byte("GGUF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, p.ID+".model")); err != nil {
		t.Fatal(err)
	}
	if n, err = m.SupportInstalledManifestCount(context.Background()); err != nil || n != 0 {
		t.Fatal("symlink counted", n, err)
	}
}
