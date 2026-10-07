package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentDiagnosticAllowlistAndReopen(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs", "operational")
	s := New(root)
	s.Record("secret-component", "secret-operation", "secret-outcome", "secret-token", -time.Second, -7)
	s.Record("model", "prepare", "passed", "none", time.Second, 8192)
	s.Close()
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatal("unsanitized input persisted", string(raw))
	}
	var p Snapshot
	if json.Unmarshal(raw, &p) != nil || len(p.Events) != 2 || p.Events[0].Component != "other" || p.Events[0].DurationMS != 0 || p.Events[1].Count != 8192 {
		t.Fatal(string(raw))
	}
	st, _ := os.Stat(filepath.Join(dir, fileName))
	if st.Mode().Perm()&0077 != 0 {
		t.Fatal("public diagnostics")
	}
	reopened := New(root)
	reopened.Record("application", "open", "passed", "none", 0, 0)
	reopened.Close()
	p = reopened.Snapshot()
	if len(p.Events) != 3 || p.Events[1].Component != "model" {
		t.Fatal("prior events were lost", p)
	}
}

func TestDiagnosticBoundsAndNoArbitraryHistory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs", "operational")
	s := New(root)
	for i := 0; i < Capacity+100; i++ {
		s.Record("search", "query", "passed", "none", 0, int64(i))
	}
	s.Close()
	p := s.Snapshot()
	if len(p.Events) != Capacity || p.Discarded != 100 || p.Events[0].Count != 100 {
		t.Fatal(p)
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil || len(raw) > maxBytes {
		t.Fatal(err, len(raw))
	}
	// Persisted data is untrusted; valid JSON is insufficient for exporting it.
	bad := `{"version":1,"persistence":"ready","discarded":0,"events":[{"at":"2026-01-01T00:00:00Z","component":"model","operation":"prepare","outcome":"passed","code":"private-secret","durationMs":0,"count":0}]}`
	if err = os.WriteFile(filepath.Join(dir, fileName), []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	s = New(root)
	s.Close()
	if p = s.Snapshot(); p.Persistence != "invalid_history" || len(p.Events) != 0 {
		t.Fatal(p)
	}
	untouched, _ := os.ReadFile(filepath.Join(dir, fileName))
	if string(untouched) != bad {
		t.Fatal("invalid prior evidence overwritten")
	}
}

func TestDiagnosticRefusesLinkedOrPublicStorage(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "original")
	if err := os.WriteFile(target, []byte("keep secret bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "logs", "operational")
	os.MkdirAll(dir, 0700)
	if err := os.Symlink(target, filepath.Join(dir, fileName)); err != nil {
		t.Skip(err)
	}
	s := New(root)
	s.Record("application", "open", "passed", "none", 0, 0)
	s.Close()
	if s.Snapshot().Persistence != "unavailable" {
		t.Fatal(s.Snapshot())
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "keep secret bytes" {
		t.Fatal("followed linked destination")
	}
	public := filepath.Join(root, "public")
	os.MkdirAll(filepath.Join(public, "logs"), 0755)
	os.Chmod(filepath.Join(public, "logs"), 0755)
	s = New(public)
	s.Close()
	if s.Snapshot().Persistence != "unavailable" {
		t.Fatal("accepted public diagnostics directory")
	}
}

func TestDiagnosticConcurrentRecordingAndClose(t *testing.T) {
	s := New(t.TempDir())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			s.Record("job", "transition", "running", "none", 0, 0)
			s.Snapshot()
		}
	}()
	s.Close()
	<-done
	s.Close()
	if len(s.Snapshot().Events) > Capacity {
		t.Fatal("unbounded events")
	}
}

func TestDiagnosticRejectsAncestorSymlinkBeforeExternalEffects(t *testing.T) {
	for _, component := range []string{"logs", "operational"} {
		t.Run(component, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "diagnostics-v1.tmp"), []byte("preserved external staging bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			parent := root
			if component == "operational" {
				parent = filepath.Join(root, "logs")
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, filepath.Join(parent, component)); err != nil {
				t.Skip(err)
			}
			s := New(root)
			s.Record("application", "open", "passed", "none", 0, 0)
			s.Close()
			if s.Snapshot().Persistence != "unavailable" {
				t.Fatal(s.Snapshot())
			}
			entries, err := os.ReadDir(outside)
			raw, readErr := os.ReadFile(filepath.Join(outside, "diagnostics-v1.tmp"))
			if err != nil || readErr != nil || len(entries) != 1 || string(raw) != "preserved external staging bytes" {
				t.Fatalf("external destination changed: %v %v %v %q", entries, err, readErr, raw)
			}
		})
	}
}

func TestDiagnosticExpiresEveryUnorderedHistoricalEvent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs", "operational")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	recent := Event{At: time.Now().UTC().Format(time.RFC3339Nano), Component: "model", Operation: "prepare", Outcome: "passed", Code: "none", Count: 11}
	expired := recent
	expired.At, expired.Count = time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339Nano), 99
	raw, _ := json.Marshal(Snapshot{Version: 1, Persistence: "ready", Events: []Event{recent, expired, recent}})
	if err := os.WriteFile(filepath.Join(dir, fileName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	s.Close()
	p := s.Snapshot()
	if p.Discarded != 1 || len(p.Events) != 2 || p.Events[0].Count != 11 || p.Events[1].Count != 11 {
		t.Fatal(p)
	}
}

func TestDiagnosticRejectsHardLinkedPrivateHistory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "logs", "operational")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(outside, "other-workspace-history.json")
	raw, _ := json.Marshal(Snapshot{Version: 1, Persistence: "ready", Events: []Event{{At: time.Now().UTC().Format(time.RFC3339Nano), Component: "model", Operation: "prepare", Outcome: "passed", Code: "none", Count: 9912}}})
	if err := os.WriteFile(external, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(external, filepath.Join(dir, fileName)); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	s.Close()
	p := s.Snapshot()
	if p.Persistence != "unavailable" || len(p.Events) != 0 {
		t.Fatal("read foreign private history", p)
	}
	actual, err := os.ReadFile(external)
	if err != nil || string(actual) != string(raw) {
		t.Fatal("external history changed", err)
	}
}
