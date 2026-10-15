package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddingIdentityTracksBytesDimensionAndPipeline(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := embeddingIdentitySupervisor(t, m, []byte("verified helper fixture"))
	p := fixturePack([]byte("GGUFfirst"))
	p.Kind = "embedding"
	p.Dimension = 384
	path := filepath.Join(m.root, p.ID+".json")
	if err = atomicJSON(path, p); err != nil {
		t.Fatal(err)
	}
	first, err := s.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Dimension = 768
	if err = atomicJSON(path, p); err != nil {
		t.Fatal(err)
	}
	second, err := s.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || first == second {
		t.Fatal("dimension did not invalidate generation", err)
	}
	p.SHA256 = fixturePack([]byte("GGUFsecond")).SHA256
	if err = atomicJSON(path, p); err != nil {
		t.Fatal(err)
	}
	third, err := s.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || second == third {
		t.Fatal("model bytes did not invalidate generation", err)
	}
	if _, err = s.EmbeddingIdentity(context.Background(), "../outside"); err == nil {
		t.Fatal("path accepted")
	}
	p.Kind = "chat"
	if err = atomicJSON(path, p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EmbeddingIdentity(context.Background(), p.ID); err == nil {
		t.Fatal("chat model used for embeddings")
	}
}

// The old worker forcibly mean-pooled even GGUFs declaring CLS. Those stored
// coordinates must not be queried using the repaired model-selected pipeline.
func TestEmbeddingIdentityInvalidatesForcedMeanPipeline(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := fixturePack([]byte("GGUFsame-model-bytes"))
	p.Kind, p.Dimension = "embedding", 384
	if err = atomicJSON(filepath.Join(m.root, p.ID+".json"), p); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]any{p.ID, p.SHA256, p.Dimension, p.RuntimeCommit, p.Format, p.Architecture, "chunk384-context512-mean-normalized-v1"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(legacy)
	oldIdentity := hex.EncodeToString(digest[:])
	s := embeddingIdentitySupervisor(t, m, []byte("verified helper fixture"))
	current, err := s.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || current == oldIdentity {
		t.Fatalf("old forced-mean vectors remain compatible with repaired pooling: %s error=%v", current, err)
	}
	again, err := s.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || again != current {
		t.Fatal("same model and pipeline identity is not stable", err)
	}
}

// No helper process or model runs here. Like NewSupervisor, this fixture hashes
// the on-disk helper before retaining its record; Run verifies it again at use.
func embeddingIdentitySupervisor(t *testing.T, m *Manager, helperBytes []byte) *Supervisor {
	t.Helper()
	root := t.TempDir()
	sum := sha256.Sum256(helperBytes)
	h := Helper{Name: "llama-worker", SHA256: hex.EncodeToString(sum[:]), Runtime: "llama.cpp", RuntimeCommit: LlamaCommit, Architecture: runtime.GOARCH}
	if err := os.WriteFile(filepath.Join(root, helperFilename(h.Name)), helperBytes, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{bundle: root, manager: m, helpers: map[string]Helper{}}
	if err := s.verifyHelper(h); err != nil {
		t.Fatal(err)
	}
	s.helpers[h.Name] = h
	return s
}

func TestEmbeddingIdentityBindsVerifiedHelper(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := fixturePack([]byte("unchanged embedding model"))
	p.Kind, p.Dimension = "embedding", 384
	if err = atomicJSON(filepath.Join(m.root, p.ID+".json"), p); err != nil {
		t.Fatal(err)
	}
	oldHelper := embeddingIdentitySupervisor(t, m, []byte("old forced-mean helper bytes"))
	newHelper := embeddingIdentitySupervisor(t, m, []byte("new model-selected helper bytes"))
	oldID, err := oldHelper.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := newHelper.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || newID == oldID {
		t.Fatalf("different verified helpers share embedding coordinates: old=%s new=%s error=%v", oldID, newID, err)
	}
	loadedAgain := embeddingIdentitySupervisor(t, m, []byte("new model-selected helper bytes"))
	again, err := loadedAgain.EmbeddingIdentity(context.Background(), p.ID)
	if err != nil || again != newID {
		t.Fatal("same helper bytes/model changed identity after reload", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Helper)
	}{
		{"missing", func(h *Helper) { *h = Helper{} }},
		{"wrong_name", func(h *Helper) { h.Name = "vision-worker" }},
		{"malformed_hash", func(h *Helper) { h.SHA256 = "unverified" }},
		{"wrong_runtime", func(h *Helper) { h.Runtime = "other.cpp" }},
		{"wrong_runtime_commit", func(h *Helper) { h.RuntimeCommit = "other-runtime" }},
		{"wrong_architecture", func(h *Helper) { h.Architecture = "other-architecture" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHelper.helpers["llama-worker"]
			tc.mutate(&h)
			invalid := &Supervisor{manager: m, helpers: map[string]Helper{"llama-worker": h}}
			if id, err := invalid.EmbeddingIdentity(context.Background(), p.ID); err == nil || id != "" {
				t.Fatal("unavailable helper established vector compatibility", id, err)
			}
		})
	}
}
