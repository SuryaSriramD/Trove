package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// EmbeddingIdentity reads the installed, validated manifest, without hashing a
// potentially large model a second time. Run leases and verifies the actual
// model bytes before inference. NewSupervisor verifies the immutable helper
// record before retaining it, and Run re-verifies those exact helper bytes. Bind
// that record as well: identical model bytes can yield different coordinates
// after a helper change. Consumers compare this identity before/after.
func (s *Supervisor) EmbeddingIdentity(ctx context.Context, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.manager == nil || !uuidPattern.MatchString(id) {
		return "", errors.New("embedding model unavailable")
	}
	path := filepath.Join(s.manager.root, id+".json")
	if err := regular(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var p Pack
	if err := json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&p); err != nil {
		return "", err
	}
	if p.ID != id || p.Kind != "embedding" || p.Validate() != nil {
		return "", errors.New("invalid embedding model manifest")
	}
	helper, ok := s.helpers["llama-worker"]
	if !ok || helper.Name != "llama-worker" || !digestPattern.MatchString(helper.SHA256) || helper.Runtime != "llama.cpp" || helper.RuntimeCommit != p.RuntimeCommit || helper.Architecture != runtime.GOARCH {
		return "", errors.New("verified embedding helper unavailable")
	}
	data, _ := json.Marshal([]any{p.ID, p.SHA256, p.Dimension, p.RuntimeCommit, p.Format, p.Architecture, "chunk384-context512-model-pooling-normalized-v2", helper.SHA256, helper.Runtime, helper.RuntimeCommit, helper.Architecture})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
