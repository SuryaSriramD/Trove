package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStartupTraceEmitsActualBoundedStageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.jsonl")
	trace := newStartupStageTrace(path, time.Now())
	defer trace.close()
	var wg sync.WaitGroup
	for stage := range startupStages {
		if stage == "shutdown_complete" {
			continue
		}
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(s string) { defer wg.Done(); trace.mark(s) }(stage)
		}
	}
	wg.Wait()
	trace.mark("workspace=/private/secret?token=hidden")
	trace.mark("shutdown_complete")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("trace was not emitted", err)
	}
	if len(raw) > 4096 {
		t.Fatal("unbounded trace")
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != len(startupStages) {
		t.Fatal("missing or repeated stage", len(lines))
	}
	seen := map[string]bool{}
	previous := -1.0
	for _, line := range lines {
		var row map[string]any
		if err = json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		stage, ok := row["stage"].(string)
		if !ok || !startupStages[stage] || seen[stage] || len(row) != 2 {
			t.Fatal("unexpected trace data", row)
		}
		elapsed, ok := row["elapsedMs"].(float64)
		if !ok || elapsed < previous {
			t.Fatal("non-monotonic timing", row)
		}
		previous = elapsed
		seen[stage] = true
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("trace is not private", err)
	}
}
func TestStartupTraceNeverReplacesEvidenceAndIgnoresBadSink(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.WriteFile(existing, []byte("preserve evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative", existing, link, filepath.Join(root, "absent", "trace")} {
		trace := newStartupStageTrace(path, time.Now())
		trace.mark("process_entry")
		trace.close()
	}
	raw, err := os.ReadFile(existing)
	if err != nil || string(raw) != "preserve evidence" {
		t.Fatal("overwrote prior evidence", err)
	}
}
