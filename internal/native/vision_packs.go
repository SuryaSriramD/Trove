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

// Version 2 vision packs bind the language model to one exact projector. The
// companion has its own durable transfer receipt, never a user-supplied path.
type VisionArtifact struct {
	ID             string `json:"id"`
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"sizeBytes"`
	URL            string `json:"url"`
	License        string `json:"license"`
	LicenseURL     string `json:"licenseUrl"`
	SourceRevision string `json:"sourceRevision"`
}

func (p Pack) validateVisionManifest() error {
	if p.Kind != "vision" {
		if p.SchemaVersion != 1 || p.Projector != nil || p.VisionVersion != 0 {
			return errors.New("MODEL_INCOMPATIBLE: only version 2 vision packs may declare a projector")
		}
		return nil
	}
	if p.SchemaVersion != 2 || p.VisionVersion != 1 || p.Projector == nil || p.Projector.ID == p.ID || p.ContextTokens < 512 || p.ContextTokens > 8192 || p.EstimatedMemoryBytes < p.SizeBytes+p.Projector.SizeBytes {
		return errors.New("MODEL_INCOMPATIBLE: invalid versioned vision pair")
	}
	return p.projectorPack().Validate()
}
func (p Pack) projectorPack() Pack {
	a := p.Projector
	return Pack{SchemaVersion: 1, ID: a.ID, Name: p.Name + " projector", Kind: "vision_projector", Format: "gguf", SHA256: a.SHA256, SizeBytes: a.SizeBytes, URL: a.URL, License: a.License, LicenseURL: a.LicenseURL, SourceRevision: a.SourceRevision, RuntimeCommit: p.RuntimeCommit, Architecture: p.Architecture, EstimatedMemoryBytes: a.SizeBytes}
}
func (p Pack) DownloadBytes() int64 {
	size := p.SizeBytes
	if p.Projector != nil {
		size += p.Projector.SizeBytes
	}
	return size
}
func offsetModelProgress(progress func(int64), offset int64) func(int64) {
	if progress == nil {
		return nil
	}
	return func(n int64) { progress(offset + n) }
}
func (m *Manager) verifyProjector(p Pack) error {
	if p.Projector == nil {
		return nil
	}
	expected := p.projectorPack()
	path, _, meta := m.paths(expected)
	raw, err := os.ReadFile(meta)
	if err != nil {
		return err
	}
	var actual Pack
	if json.Unmarshal(raw, &actual) != nil || actual.Validate() != nil || actual.ID != expected.ID || actual.Kind != "vision_projector" || actual.SHA256 != expected.SHA256 || actual.SizeBytes != expected.SizeBytes || actual.RuntimeCommit != expected.RuntimeCommit {
		return errors.New("MODEL_INCOMPATIBLE: companion manifest mismatch")
	}
	return verifyFile(path, expected)
}
func (m *Manager) projectorDownloaded(p Pack) int64 {
	if p.Projector == nil {
		return 0
	}
	a := p.projectorPack()
	final, part, _ := m.paths(a)
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() && st.Size() == a.SizeBytes {
		return st.Size()
	}
	if st, err := os.Stat(part); err == nil && st.Mode().IsRegular() {
		return min(st.Size(), a.SizeBytes)
	}
	return 0
}
func (m *Manager) ImportVision(ctx context.Context, p Pack, model, projector io.Reader, progress func(int64)) error {
	if p.Kind != "vision" || p.Projector == nil {
		return errors.New("MODEL_INCOMPATIBLE: not a vision pair")
	}
	if err := m.begin(p); err != nil {
		return err
	}
	defer m.end(p.ID)
	if err := m.requireDisk(p.DownloadBytes()); err != nil {
		return err
	}
	_, part, _ := m.paths(p)
	if err := atomicJSON(part+".json", acquisition{Pack: p, State: "importing"}); err != nil {
		return err
	}
	if err := m.Import(ctx, p.projectorPack(), projector, progress); err != nil {
		return err
	}
	return m.importContent(ctx, p, model, offsetModelProgress(progress, p.Projector.SizeBytes))
}
func (m *Manager) leaseProjector(p Pack) (*os.File, func(), error) {
	if p.Kind != "vision" || p.Projector == nil {
		return nil, nil, errors.New("MODEL_INCOMPATIBLE: missing projector")
	}
	actual, file, release, err := m.lease(p.Projector.ID)
	if err != nil {
		return nil, nil, err
	}
	expected := p.projectorPack()
	if actual.Kind != "vision_projector" || actual.SHA256 != expected.SHA256 || actual.SizeBytes != expected.SizeBytes || actual.RuntimeCommit != p.RuntimeCommit {
		release()
		return nil, nil, errors.New("MODEL_INCOMPATIBLE: wrong projector pair")
	}
	return file, release, nil
}

// Called only under m.mu. Other installed/partial pairs may share a companion;
// deleting one never removes a companion still referenced or actively leased.
func (m *Manager) removeUnusedProjector(id string) error {
	var p Pack
	if raw, err := os.ReadFile(filepath.Join(m.root, id+".json")); err == nil {
		_ = json.Unmarshal(raw, &p)
	} else if raw, err = os.ReadFile(filepath.Join(m.root, id+".part.json")); err == nil {
		var a acquisition
		_ = json.Unmarshal(raw, &a)
		p = a.Pack
	}
	if p.Projector == nil || p.Validate() != nil {
		return nil
	}
	child := p.Projector.ID
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), id+".") {
			continue
		}
		raw, er := os.ReadFile(filepath.Join(m.root, entry.Name()))
		if er != nil {
			continue
		}
		var other Pack
		if strings.HasSuffix(entry.Name(), ".part.json") {
			var a acquisition
			_ = json.Unmarshal(raw, &a)
			other = a.Pack
		} else {
			_ = json.Unmarshal(raw, &other)
		}
		if other.Projector != nil && other.Projector.ID == child {
			return nil
		}
	}
	if m.busy[child] || m.leases[child] > 0 {
		return errors.New("RESOURCE_EXHAUSTED: vision companion in use")
	}
	owner, err := m.acquireOwner(child)
	if err != nil {
		return err
	}
	defer platformfs.ReleaseLock(owner)
	for _, suffix := range []string{".json", ".model", ".part", ".part.json"} {
		if err = os.Remove(filepath.Join(m.root, child+suffix)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
