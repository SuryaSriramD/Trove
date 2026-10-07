package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"trove.local/phase0/internal/workspace"
)

func performanceTestPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "performance.json")
}

func TestQualificationPerformancePrivateFixedSchemaAndBounds(t *testing.T) {
	path := performanceTestPath(t)
	p := newQualificationPerformance(path)
	if !p.enabled() {
		t.Fatal("private collector unavailable")
	}
	a := &App{performance: p}
	if !a.QualificationPerformanceEnabled() || !a.RecordQualificationPerformance("database_bridge", 12.5) {
		t.Fatal("opted-in renderer sample refused")
	}
	for _, metric := range []string{"/private/title?token=secret", "database_dispatch", "", "database_bridge\nsecret"} {
		if a.RecordQualificationPerformance(metric, 1) {
			t.Fatal("renderer supplied an unauthorized metric")
		}
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), 3600001} {
		if a.RecordQualificationPerformance("input_to_frames", value) {
			t.Fatal("invalid duration accepted")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 600; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.RecordQualificationPerformance("input_to_frames", 42) }()
	}
	wg.Wait()
	if finish := p.dispatch("queryRecords"); finish != nil {
		finish()
	} else {
		t.Fatal("database dispatch not measured")
	}
	if p.dispatch("private arbitrary method") != nil {
		t.Fatal("unknown dispatch recorded")
	}
	p.close()
	if a.QualificationPerformanceEnabled() || a.RecordQualificationPerformance("database_bridge", 1) {
		t.Fatal("closed collector accepted writes")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 512*1024 {
		t.Fatal("missing/unbounded report", err)
	}
	var report struct {
		SchemaVersion int                            `json:"schemaVersion"`
		Finalized     bool                           `json:"finalized"`
		SampleLimit   int                            `json:"sampleLimitPerMetric"`
		Metrics       map[string]*performanceSamples `json:"metrics"`
	}
	if err = json.Unmarshal(raw, &report); err != nil || !report.Finalized || report.SchemaVersion != 1 || report.SampleLimit != performanceSampleLimit {
		t.Fatal("invalid completion report", err)
	}
	if len(report.Metrics) != 3 || len(report.Metrics["database_bridge"].Milliseconds) != 1 || report.Metrics["database_bridge"].Milliseconds[0] != 12.5 || len(report.Metrics["database_dispatch"].Milliseconds) != 1 || len(report.Metrics["input_to_frames"].Milliseconds) != performanceSampleLimit || report.Metrics["input_to_frames"].Dropped != 88 {
		t.Fatal("samples lost, unbounded or unexpected payload", report)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("report is not private", err)
	}
}

func TestQualificationPerformanceWrapsActualDesktopDispatchWithoutPayload(t *testing.T) {
	path := performanceTestPath(t)
	t.Setenv("TROVE_PERFORMANCE_REPORT", path)
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx, a.kernel, a.execution = context.Background(), k, k.OwnerContext()
	a.completeStartup(nil)
	var success struct {
		OK bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(a.Dispatch("workspace.snapshot", `{"contentMode":"shell"}`)), &success); err != nil || !success.OK {
		t.Fatal("instrumentation changed a real service response", err)
	}
	// Rejected requests are still elapsed observations; their private payload and
	// errors must not enter the timing report or turn into a successful response.
	var rejected struct {
		OK bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(a.Dispatch("queryRecords", `{"private-secret-title":"must never enter telemetry"}`)), &rejected); err != nil || rejected.OK {
		t.Fatal("invalid request became successful", err)
	}
	a.performance.close()
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte(k.ID())) {
		t.Fatal("private request or identity entered timing report", err)
	}
	var report struct {
		Metrics map[string]*performanceSamples `json:"metrics"`
	}
	if err = json.Unmarshal(raw, &report); err != nil || len(report.Metrics) != 2 || len(report.Metrics["snapshot_dispatch"].Milliseconds) != 1 || len(report.Metrics["database_dispatch"].Milliseconds) != 1 {
		t.Fatal("actual dispatch samples missing", err)
	}
}

func TestQualificationPerformanceRefusesExistingLinkedAndPublicSinks(t *testing.T) {
	path := performanceTestPath(t)
	root := filepath.Dir(path)
	if err := os.WriteFile(path, []byte("retained evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(root, "linked-directory")
	if err := os.Symlink(root, dirLink); err != nil {
		t.Fatal(err)
	}
	public := filepath.Join(root, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"", "relative", path, link, filepath.Join(dirLink, "new"), filepath.Join(public, "new")} {
		if p := newQualificationPerformance(target); p != nil {
			p.close()
			t.Fatal("unsafe/reused sink accepted", target)
		}
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "retained evidence" {
		t.Fatal("earlier evidence overwritten", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatal("symlinked ancestor allowed output")
	}
	a := &App{}
	if a.QualificationPerformanceEnabled() || a.RecordQualificationPerformance("input_to_frames", 10) {
		t.Fatal("disabled application accepted telemetry")
	}
}

func TestQualificationPerformanceRejectsChangedOrHardLinkedOutputAtFlush(t *testing.T) {
	for _, action := range []string{"replace", "link"} {
		t.Run(action, func(t *testing.T) {
			path := performanceTestPath(t)
			p := newQualificationPerformance(path)
			if !p.enabled() {
				t.Fatal("collector unavailable")
			}
			p.record("database_bridge", 10)
			if action == "replace" {
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("retain replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Link(path, path+".alias"); err != nil {
				t.Fatal(err)
			}
			p.close()
			raw, err := os.ReadFile(path)
			if err != nil || (action == "replace" && string(raw) != "retain replacement") || (action == "link" && len(raw) != 0) {
				t.Fatal("unsafe flush modified output", err)
			}
		})
	}
}
