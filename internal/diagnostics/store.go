// Package diagnostics retains bounded operational metadata, never arbitrary text.
package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"trove.local/phase0/internal/platformfs"
)

const Capacity = 512
const maxBytes = 256 << 10
const fileName = "diagnostics-v1.json"

type Event struct {
	At         string `json:"at"`
	Component  string `json:"component"`
	Operation  string `json:"operation"`
	Outcome    string `json:"outcome"`
	Code       string `json:"code"`
	DurationMS int64  `json:"durationMs"`
	Count      int64  `json:"count"`
}
type Snapshot struct {
	Version     int     `json:"version"`
	Persistence string  `json:"persistence"`
	Discarded   uint64  `json:"discarded"`
	Events      []Event `json:"events"`
}
type Store struct {
	mu        sync.Mutex
	events    []Event
	discarded uint64
	status    string
	closed    bool
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

func member(v string, values ...string) string {
	for _, x := range values {
		if v == x {
			return v
		}
	}
	return "other"
}
func sanitize(e Event) Event {
	e.Component = member(e.Component, "application", "job", "import", "model", "search")
	e.Operation = member(e.Operation, "open", "initialize", "post_migration_initialize", "close", "migrate", "request", "start", "finish", "worker_finish", "prepare", "transition", "index", "query")
	e.Outcome = member(e.Outcome, "started", "passed", "failed", "cancelled", "completed", "queued", "running", "retry_wait", "cancel_requested", "succeeded", "interrupted")
	e.Code = member(e.Code, "none", "CANCELLED", "INVALID_REQUEST", "NOT_FOUND", "SCOPE_DENIED", "CONFLICT", "RESOURCE_EXHAUSTED", "INTERNAL", "MODEL_INCOMPATIBLE", "MODEL_UNAVAILABLE", "WORKSPACE_LOCKED", "UNAVAILABLE", "WORKER_FAILED", "INTEGRITY_ERROR", "UNSUPPORTED_SCHEMA")
	if e.DurationMS < 0 {
		e.DurationMS = 0
	}
	if e.DurationMS > 86400000 {
		e.DurationMS = 86400000
	}
	if e.Count < 0 {
		e.Count = 0
	}
	if e.Count > 1_000_000_000 {
		e.Count = 1_000_000_000
	}
	return e
}

// New performs all filesystem work on a background goroutine. Diagnostics are
// disposable and must never delay a canonical service or claim crash durability.
func New(workspace string) *Store {
	s := &Store{status: "loading", wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go s.run(workspace)
	return s
}
func (s *Store) Record(component, operation, outcome, code string, duration time.Duration, count int64) {
	if s == nil {
		return
	}
	e := sanitize(Event{At: time.Now().UTC().Format(time.RFC3339Nano), Component: component, Operation: operation, Outcome: outcome, Code: code, DurationMS: duration.Milliseconds(), Count: count})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.events = append(s.events, e)
	s.trim()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Store) trim() {
	kept := s.events[:0]
	oldest := time.Now().Add(-7 * 24 * time.Hour)
	for _, event := range s.events {
		t, err := time.Parse(time.RFC3339Nano, event.At)
		if err == nil && !t.Before(oldest) {
			kept = append(kept, event)
		} else {
			s.discarded++
		}
	}
	if cut := len(kept) - Capacity; cut > 0 {
		s.discarded += uint64(cut)
		kept = append([]Event(nil), kept[cut:]...)
	}
	s.events = kept
}
func (s *Store) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{Version: 1, Persistence: "unavailable", Events: []Event{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trim()
	return Snapshot{Version: 1, Persistence: s.status, Discarded: s.discarded, Events: append([]Event{}, s.events...)}
}
func (s *Store) setStatus(v string) { s.mu.Lock(); s.status = v; s.mu.Unlock() }

// Close is bounded even if an OS filesystem operation stalls. The immutable
// snapshot writer can finish afterward; no kernel/database handle is retained.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.stop)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		s.setStatus("close_timeout")
	}
}

func decode(raw []byte) ([]Event, uint64, bool) {
	if len(raw) > maxBytes {
		return nil, 0, false
	}
	var p Snapshot
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Version != 1 || len(p.Events) > Capacity {
		return nil, 0, false
	}
	for _, e := range p.Events {
		t, err := time.Parse(time.RFC3339Nano, e.At)
		if err != nil || t.After(time.Now().Add(time.Minute)) || e != sanitize(e) {
			return nil, 0, false
		}
	}
	return p.Events, p.Discarded, true
}

// Every descendant creation is relative to a pinned root. Never walk an
// untrusted logs symlink before anchoring effects to the existing workspace.
func privateChild(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	st, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("linked diagnostics directory")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	f, err := child.Open(".")
	if err == nil {
		err = platformfs.AuditPrivateHandle(f, true)
		pinned, statErr := f.Stat()
		if statErr != nil || !os.SameFile(st, pinned) {
			err = errors.New("diagnostics directory changed")
		}
		f.Close()
	}
	if err != nil {
		child.Close()
		return nil, err
	}
	return child, nil
}

func (s *Store) run(workspace string) {
	defer close(s.done)
	base, err := os.OpenRoot(workspace)
	if err != nil {
		s.setStatus("unavailable")
		return
	}
	defer base.Close()
	logs, err := privateChild(base, "logs")
	if err != nil {
		s.setStatus("unavailable")
		return
	}
	defer logs.Close()
	r, err := privateChild(logs, "operational")
	if err != nil {
		s.setStatus("unavailable")
		return
	}
	defer r.Close()
	lock, err := platformfs.AcquireRootLock(r, "owner.lock")
	if err != nil {
		s.setStatus("unavailable")
		return
	}
	defer platformfs.ReleaseLock(lock)
	// The kernel's owner lock serializes workspace sessions. A crash may leave
	// the single staging file; remove only that exact regular file, never links.
	if st, err := r.Lstat("diagnostics-v1.tmp"); err == nil {
		if !st.Mode().IsRegular() || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
			s.setStatus("unavailable")
			return
		}
		if r.Remove("diagnostics-v1.tmp") != nil {
			s.setStatus("unavailable")
			return
		}
	}
	if st, err := r.Lstat(fileName); err == nil {
		if !st.Mode().IsRegular() || st.Size() > maxBytes || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
			s.setStatus("unavailable")
			return
		}
		f, err := r.Open(fileName)
		if err != nil {
			s.setStatus("unavailable")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
		f.Close()
		previous, dropped, valid := decode(raw)
		if err != nil || !valid {
			s.setStatus("invalid_history")
			return
		}
		s.mu.Lock()
		s.events = append(previous, s.events...)
		s.discarded += dropped
		s.trim()
		s.mu.Unlock()
	} else if !os.IsNotExist(err) {
		s.setStatus("unavailable")
		return
	}
	s.setStatus("ready")
	dirty := false
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	flush := func() bool {
		p := s.Snapshot()
		raw, err := json.Marshal(p)
		if err != nil || len(raw) > maxBytes {
			return false
		}
		f, err := r.OpenFile("diagnostics-v1.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return false
		}
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			r.Remove("diagnostics-v1.tmp")
			return false
		}
		if err = r.Rename("diagnostics-v1.tmp", fileName); err != nil {
			r.Remove("diagnostics-v1.tmp")
			return false
		}
		return true
	}
	for {
		select {
		case <-s.wake:
			dirty = true
		case <-tick.C:
			if dirty {
				if !flush() {
					s.setStatus("write_failed")
					return
				}
				dirty = false
			}
		case <-s.stop:
			if !flush() {
				s.setStatus("write_failed")
			}
			return
		}
	}
}
