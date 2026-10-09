package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func diagnosticState() qualificationRendererState {
	counters := map[string]uint32{}
	for key := range qualificationCounterNames {
		counters[key] = 0
	}
	return qualificationRendererState{Ready: true, Visible: true, Focused: true, Counters: counters}
}

func diagnosticRaw(t *testing.T, state qualificationRendererState) string {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestQualificationDiagnosticsPrivateSeparateAndAcknowledged(t *testing.T) {
	path := performanceTestPath(t)
	p := newQualificationPerformance(path)
	p.diagnostics = newQualificationDiagnostics(path + ".diagnostics")
	a := &App{performance: p}
	state := diagnosticState()
	state.Counters["probes_completed"] = 1
	if !a.RecordQualificationPerformanceDiagnostics(diagnosticRaw(t, state)) {
		t.Fatal("diagnostic snapshot refused")
	}
	live, err := os.ReadFile(path + ".diagnostics")
	if err != nil || !strings.Contains(string(live), `"finalized":false`) {
		t.Fatal("live witness unavailable", err)
	}
	if raw, _ := os.ReadFile(path); len(raw) != 0 {
		t.Fatal("diagnostics altered timing schema")
	}
	p.close()
	if a.RecordQualificationPerformanceDiagnostics(diagnosticRaw(t, state)) {
		t.Fatal("closed collector accepted snapshot")
	}
	raw, err := os.ReadFile(path + ".diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"sequence":2`) || !strings.Contains(lines[1], `"finalized":true`) {
		t.Fatal("missing final sidecar boundary")
	}
	if strings.Contains(string(raw), path) {
		t.Fatal("path entered diagnostic payload")
	}
	if info, err := os.Stat(path + ".diagnostics"); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("nonprivate diagnostic", err)
	}
}

func TestQualificationDiagnosticsRejectsUnknownMissingDuplicateAndUnboundedPayload(t *testing.T) {
	d := newQualificationDiagnostics(performanceTestPath(t))
	defer d.close()
	valid := diagnosticRaw(t, diagnosticState())
	for _, invalid := range []string{
		strings.Replace(valid, `"ready":true`, `"ready":true,"title":"secret"`, 1),
		strings.Replace(valid, `"ready":true`, `"ready":true,"ready":true`, 1),
		strings.Replace(valid, `"ready":true,`, ``, 1),
		strings.Replace(valid, `"ready":true`, `"ready":null`, 1),
		strings.Replace(valid, `"pendingFrames":0`, `"pendingFrames":17`, 1),
		strings.Replace(valid, `"pendingTransports":0`, `"pendingTransports":4105`, 1),
		strings.Replace(valid, `"probes_requested":0`, `"probes_requested":65536`, 1),
		strings.Replace(valid, `"probes_requested":0`, `"probes_requested":null`, 1),
		strings.Replace(valid, `"probes_requested":0`, `"probes_requested":0,"probes_requested":0`, 1),
		valid + ` {}`, strings.Repeat("x", 4097),
	} {
		if d.record(invalid) {
			t.Fatal("invalid diagnostic accepted", invalid)
		}
	}
	state := diagnosticState()
	state.Counters["records_accepted"] = 2
	if !d.record(diagnosticRaw(t, state)) {
		t.Fatal("valid diagnostic rejected")
	}
	if d.record(valid) {
		t.Fatal("decreasing counters accepted")
	}
}

func TestQualificationDiagnosticsRejectsReusedReplacedAndHardLinkedSink(t *testing.T) {
	for _, action := range []string{"replace", "link"} {
		t.Run(action, func(t *testing.T) {
			path := performanceTestPath(t)
			d := newQualificationDiagnostics(path)
			if d == nil {
				t.Fatal("missing collector")
			}
			if newQualificationDiagnostics(path) != nil {
				t.Fatal("reused sink accepted")
			}
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
			if d.record(diagnosticRaw(t, diagnosticState())) {
				t.Fatal("changed sink accepted")
			}
			d.close()
			raw, err := os.ReadFile(path)
			if err != nil || (action == "replace" && string(raw) != "retain replacement") || (action == "link" && len(raw) != 0) {
				t.Fatal("changed sink overwritten", err)
			}
		})
	}
}

func TestQualificationDiagnosticsCapAndDisabledMode(t *testing.T) {
	path := performanceTestPath(t)
	d := newQualificationDiagnostics(path)
	d.sequence = qualificationDiagnosticLimit
	if d.record(diagnosticRaw(t, diagnosticState())) {
		t.Fatal("snapshot cap ignored")
	}
	d.close()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"capped":true`) {
		t.Fatal("cap omitted at close")
	}
	t.Setenv("TROVE_PERFORMANCE_REPORT", "")
	t.Setenv("TROVE_PERFORMANCE_DIAGNOSTICS", path+".disabled")
	a := NewApp()
	if a.RecordQualificationPerformanceDiagnostics(diagnosticRaw(t, diagnosticState())) {
		t.Fatal("disabled telemetry accepted")
	}
	if _, err := os.Stat(path + ".disabled"); !os.IsNotExist(err) {
		t.Fatal("ordinary application created diagnostic file")
	}
}
