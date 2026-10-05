package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Optional qualification tracing contains fixed lifecycle names and elapsed
// durations only. Existing files are never overwritten or appended to.
type startupStageTrace struct {
	mu      sync.Mutex
	started time.Time
	file    *os.File
	seen    map[string]bool
	bytes   int
}

var startupTraceOnce sync.Once
var processStartupTrace *startupStageTrace
var startupStages = map[string]bool{
	"process_entry": true, "rollback_channel_ready": true, "wails_run_start": true,
	"native_startup_enter": true, "native_runtime_ready": true, "workspace_open_start": true,
	"workspace_open_complete": true, "startup_complete": true, "dom_ready": true,
	"snapshot_ready": true, "shell_ready": true, "rollback_committed": true,
	"shutdown_begin": true, "shutdown_complete": true,
}

func newStartupStageTrace(path string, started time.Time) *startupStageTrace {
	trace := &startupStageTrace{started: started, seen: map[string]bool{}}
	if path == "" || len(path) > 4096 || !filepath.IsAbs(path) {
		return trace
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return trace
	}
	trace.file = file
	return trace
}
func markStartupStage(stage string) {
	startupTraceOnce.Do(func() { processStartupTrace = newStartupStageTrace(os.Getenv("TROVE_STARTUP_TRACE"), processStarted) })
	processStartupTrace.mark(stage)
}
func (t *startupStageTrace) mark(stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil || !startupStages[stage] || t.seen[stage] {
		return
	}
	t.seen[stage] = true
	raw, err := json.Marshal(struct {
		Stage     string  `json:"stage"`
		ElapsedMS float64 `json:"elapsedMs"`
	}{stage, float64(time.Since(t.started).Microseconds()) / 1000})
	if err != nil || t.bytes+len(raw)+1 > 4096 {
		t.closeLocked()
		return
	}
	raw = append(raw, '\n')
	n, err := t.file.Write(raw)
	t.bytes += n
	if err != nil || n != len(raw) || t.file.Sync() != nil || stage == "shutdown_complete" {
		t.closeLocked()
	}
}
func (t *startupStageTrace) closeLocked() {
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
	}
}
func (t *startupStageTrace) close() { t.mu.Lock(); defer t.mu.Unlock(); t.closeLocked() }
