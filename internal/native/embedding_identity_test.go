package native

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEmbeddingIdentityTracksBytesDimensionAndPipeline(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{manager: m}
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
