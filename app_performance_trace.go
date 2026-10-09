package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"trove.local/phase0/internal/platformfs"
)

const performanceSampleLimit = 512

var dispatchPerformanceLabels = map[string]string{
	"queryRecords": "database_dispatch", "workspace.snapshot": "snapshot_dispatch",
	"batch": "mutation_dispatch", "search.query": "search_dispatch",
}
var rendererPerformanceLabels = map[string]bool{
	"database_bridge": true, "snapshot_bridge": true, "mutation_bridge": true, "search_bridge": true,
	"database_load_to_frames": true, "database_response_to_frames": true,
	"input_to_frames": true, "scroll_to_frames": true,
}

type performanceSamples struct {
	Milliseconds []float64 `json:"milliseconds"`
	Dropped      uint64    `json:"dropped"`
}

// Qualification only: no paths, request IDs, titles, input values or error text
// enter this fixed-label report. Samples stay in bounded memory until shutdown.
// An empty reserved file means collection did not finish, never a passing run.
type qualificationPerformance struct {
	mu          sync.Mutex
	root        *os.Root
	file        *os.File
	name        string
	samples     map[string]*performanceSamples
	diagnostics *qualificationDiagnostics
}

func newQualificationPerformance(path string) *qualificationPerformance {
	if path == "" || len(path) > 4096 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil
	}
	// Walk from a pinned filesystem root. Reject symlinked ancestors instead of
	// resolving an arbitrary output path through them. The final directory must
	// already be private; diagnostics never chmod user-owned directories.
	volume := filepath.VolumeName(path)
	root, err := os.OpenRoot(volume + string(os.PathSeparator))
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Dir(path), volume+string(os.PathSeparator)), string(os.PathSeparator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		info, statErr := root.Lstat(part)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil
		}
		next, openErr := root.OpenRoot(part)
		root.Close()
		if openErr != nil {
			return nil
		}
		root = next
		pinned, pinErr := root.Stat(".")
		if pinErr != nil || !os.SameFile(info, pinned) {
			root.Close()
			return nil
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil
	}
	err = platformfs.AuditPrivateHandle(dir, true)
	dir.Close()
	if err != nil {
		root.Close()
		return nil
	}
	name := filepath.Base(path)
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		root.Close()
		return nil
	}
	if err = platformfs.AuditPrivateHandle(file, false); err != nil {
		file.Close()
		root.Close()
		return nil
	}
	return &qualificationPerformance{root: root, file: file, name: name, samples: map[string]*performanceSamples{}}
}

func (p *qualificationPerformance) enabled() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.file != nil
}

func (p *qualificationPerformance) record(label string, milliseconds float64) bool {
	if p == nil || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds < 0 || milliseconds > 3600000 {
		return false
	}
	known := rendererPerformanceLabels[label]
	for _, allowed := range dispatchPerformanceLabels {
		known = known || label == allowed
	}
	if !known {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file == nil {
		return false
	}
	entry := p.samples[label]
	if entry == nil {
		entry = &performanceSamples{Milliseconds: []float64{}}
		p.samples[label] = entry
	}
	if len(entry.Milliseconds) >= performanceSampleLimit {
		entry.Dropped++
		return false
	}
	entry.Milliseconds = append(entry.Milliseconds, milliseconds)
	return true
}

func (p *qualificationPerformance) dispatch(method string) func() {
	label := dispatchPerformanceLabels[method]
	if label == "" || !p.enabled() {
		return nil
	}
	started := time.Now()
	return func() { p.record(label, float64(time.Since(started).Microseconds())/1000) }
}

func (p *qualificationPerformance) close() {
	if p == nil {
		return
	}
	p.diagnostics.close()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file == nil {
		return
	}
	defer func() { p.file.Close(); p.root.Close(); p.file = nil }()
	info, err := p.root.Lstat(p.name)
	pinned, statErr := p.file.Stat()
	if err != nil || statErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, pinned) || platformfs.AuditPrivateHandle(p.file, false) != nil {
		return
	}
	// Ordinary bounded write, not a power-loss-durable report. A short/failed
	// write leaves invalid JSON and must be rejected by the collecting driver.
	raw, err := json.Marshal(struct {
		SchemaVersion int                            `json:"schemaVersion"`
		Finalized     bool                           `json:"finalized"`
		SampleLimit   int                            `json:"sampleLimitPerMetric"`
		Metrics       map[string]*performanceSamples `json:"metrics"`
	}{1, true, performanceSampleLimit, p.samples})
	if err == nil && len(raw) <= 512*1024 {
		_, _ = p.file.Write(raw)
	}
}

func (a *App) QualificationPerformanceEnabled() bool {
	if err := a.requireRollbackCommitted(); err != nil {
		return false
	}
	return a.performance.enabled()
}

func (a *App) RecordQualificationPerformance(metric string, milliseconds float64) bool {
	if err := a.requireRollbackCommitted(); err != nil {
		return false
	}
	if !rendererPerformanceLabels[metric] {
		return false
	}
	return a.performance.record(metric, milliseconds)
}
