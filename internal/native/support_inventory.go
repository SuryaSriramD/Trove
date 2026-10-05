package native

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"trove.local/phase0/internal/platformfs"
)

// SupportInstalledManifestCount inventories at most 4096 entries, with bounded
// manifests, without hashing model weights or exposing names. A matching regular
// model file/size is installation evidence only; integrity is explicitly unknown.
func (m *Manager) SupportInstalledManifestCount(ctx context.Context) (int, error) {
	dir, err := os.Open(m.root)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	count, seen := 0, 0
	for {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		entries, readErr := dir.ReadDir(64)
		for _, entry := range entries {
			seen++
			if seen > 4096 {
				return 0, errors.New("inventory bound exceeded")
			}
			if !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".part.json") || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(m.root, entry.Name())
			f, openErr := platformfs.OpenRegular(path)
			if openErr != nil {
				continue
			}
			raw, readErr := io.ReadAll(io.LimitReader(f, 65537))
			f.Close()
			if readErr != nil || len(raw) > 65536 {
				continue
			}
			var p Pack
			if json.Unmarshal(raw, &p) != nil || p.Validate() != nil || p.Kind == "vision_projector" || entry.Name() != p.ID+".json" {
				continue
			}
			model, _, _ := m.paths(p)
			st, statErr := os.Lstat(model)
			if statErr == nil && st.Mode().IsRegular() && st.Size() == p.SizeBytes {
				count++
			}
		}
		if readErr == io.EOF {
			return count, nil
		}
		if readErr != nil {
			return 0, readErr
		}
	}
}
