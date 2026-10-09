package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"trove.local/phase0/internal/platformfs"
)

const qualificationDiagnosticLimit = 4096
const qualificationCounterLimit = 65535

var qualificationCounterNames = map[string]bool{
	"probes_requested": true, "probes_scheduled": true, "probes_completed": true,
	"probes_cancelled": true, "probes_hidden": true, "probes_limited": true,
	"probes_missing_start": true, "records_attempted": true, "records_accepted": true,
	"records_rejected": true, "records_failed": true, "records_capped": true,
	"input_trusted": true, "input_ignored": true, "scroll_trusted": true, "scroll_ignored": true,
}

type qualificationRendererState struct {
	Ready             bool              `json:"ready"`
	Visible           bool              `json:"visible"`
	Focused           bool              `json:"focused"`
	Stopped           bool              `json:"stopped"`
	PendingFrames     uint32            `json:"pendingFrames"`
	PendingTransports uint32            `json:"pendingTransports"`
	Counters          map[string]uint32 `json:"counters"`
}

type qualificationDiagnostics struct {
	// Reuse the timing sink's private/exclusive anchored reservation. This sink
	// has its own JSONL writer and never calls the timing-format close method.
	sink     *qualificationPerformance
	sequence uint32
	last     qualificationRendererState
	failed   bool
}

func newQualificationDiagnostics(path string) *qualificationDiagnostics {
	sink := newQualificationPerformance(path)
	if sink == nil {
		return nil
	}
	return &qualificationDiagnostics{sink: sink, last: qualificationRendererState{Counters: map[string]uint32{}}}
}

func (d *qualificationDiagnostics) writeLocked(finalized bool) bool {
	s := d.sink
	if s.file == nil || d.failed || (!finalized && d.sequence >= qualificationDiagnosticLimit) {
		return false
	}
	info, err := s.root.Lstat(s.name)
	pinned, statErr := s.file.Stat()
	if err != nil || statErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, pinned) || platformfs.AuditPrivateHandle(s.file, false) != nil {
		d.failed = true
		return false
	}
	d.sequence++
	raw, err := json.Marshal(struct {
		SchemaVersion int    `json:"schemaVersion"`
		Sequence      uint32 `json:"sequence"`
		Finalized     bool   `json:"finalized"`
		Capped        bool   `json:"capped"`
		qualificationRendererState
	}{1, d.sequence, finalized, d.sequence > qualificationDiagnosticLimit, d.last})
	if err != nil || len(raw) > 4096 {
		d.failed = true
		return false
	}
	raw = append(raw, '\n')
	n, err := s.file.Write(raw)
	if err != nil || n != len(raw) {
		d.failed = true
		return false
	}
	return true
}

func (d *qualificationDiagnostics) record(raw string) bool {
	if d == nil || len(raw) > 4096 {
		return false
	}
	var state qualificationRendererState
	fields, err := qualificationObject(raw)
	if err != nil || len(fields) != 7 {
		return false
	}
	for _, key := range []string{"ready", "visible", "focused", "stopped"} {
		if string(fields[key]) != "true" && string(fields[key]) != "false" {
			return false
		}
	}
	for _, key := range []string{"pendingFrames", "pendingTransports"} {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return false
		}
	}
	counters, err := qualificationObject(string(fields["counters"]))
	if err != nil || len(counters) != len(qualificationCounterNames) {
		return false
	}
	for _, value := range counters {
		if string(value) == "null" {
			return false
		}
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || state.PendingFrames > 16 || state.PendingTransports > 4104 || state.Counters == nil || len(state.Counters) != len(qualificationCounterNames) {
		return false
	}
	for key, count := range state.Counters {
		if !qualificationCounterNames[key] || count > qualificationCounterLimit {
			return false
		}
	}
	d.sink.mu.Lock()
	defer d.sink.mu.Unlock()
	for key, count := range d.last.Counters {
		if state.Counters[key] < count {
			return false
		}
	}
	d.last = state
	return d.writeLocked(false)
}

func qualificationObject(raw string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid qualification object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		key, valid := token.(string)
		if err != nil || !valid || fields[key] != nil {
			return nil, errors.New("invalid qualification key")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, errors.New("invalid qualification value")
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid qualification end")
	}
	return fields, nil
}

func (d *qualificationDiagnostics) close() {
	if d == nil {
		return
	}
	d.sink.mu.Lock()
	defer d.sink.mu.Unlock()
	if d.sink.file == nil {
		return
	}
	if d.writeLocked(true) {
		_ = d.sink.file.Sync()
	}
	d.sink.file.Close()
	d.sink.root.Close()
	d.sink.file = nil
}

func (a *App) RecordQualificationPerformanceDiagnostics(raw string) bool {
	if err := a.requireRollbackCommitted(); err != nil {
		return false
	}
	if a.performance == nil || !a.performance.enabled() {
		return false
	}
	return a.performance.diagnostics.record(raw)
}
