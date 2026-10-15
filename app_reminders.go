package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"trove.local/phase0/internal/reminders"
	"trove.local/phase0/internal/workspace"
)

type reminderActivation struct {
	Enabled bool   `json:"enabled"`
	Epoch   string `json:"epoch"`
}
type reminderDesktopFile struct {
	Version        int                           `json:"version"`
	Bindings       map[string]reminderActivation `json:"bindings"`
	CurrentBinding string                        `json:"currentBinding"`
	Published      reminders.Schedule            `json:"published"`
}
type reminderDesktop struct {
	mu          sync.Mutex
	store       *reminders.Store
	data        reminderDesktopFile
	status      ReminderDesktopStatus
	native      func(int) (string, error)
	lastReceipt string
	stop        chan struct{}
	done        chan struct{}
	loopStarted bool
	handoff     bool
}
type ReminderDesktopStatus struct {
	Available    bool   `json:"available"`
	Enabled      bool   `json:"enabled"`
	Registration string `json:"registration"`
	Publication  string `json:"publication"`
	Permission   string `json:"permission"`
	Epoch        string `json:"epoch"`
	Generation   int64  `json:"generation"`
	Error        string `json:"error,omitempty"`
}

func reminderBundleAvailable() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	contents := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(contents) != "Contents" {
		return false
	}
	info, err := os.Lstat(filepath.Join(contents, "Library", "LoginItems", "TroveReminders.app", "Contents", "MacOS", "TroveReminders"))
	return err == nil && info.Mode().IsRegular()
}
func (a *App) reminderDesktopState() (*reminderDesktop, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.shutdownDone:
		return nil, fmt.Errorf("application is shutting down")
	default:
	}
	if a.reminders != nil {
		return a.reminders, nil
	}
	if !reminderBundleAvailable() {
		return nil, fmt.Errorf("Background reminders require the packaged macOS application with its signed login item")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	store, err := reminders.OpenStore(filepath.Join(base, "Trove", "reminders"))
	if err != nil {
		return nil, fmt.Errorf("Reminder publication is unavailable or another Trove process owns it: %w", err)
	}
	s := &reminderDesktop{store: store, native: reminderNativeService, data: reminderDesktopFile{Version: 1, Bindings: map[string]reminderActivation{}}, stop: make(chan struct{}), done: make(chan struct{})}
	err = store.Read("desktop.json", &s.data)
	if err != nil && !os.IsNotExist(err) {
		store.Close()
		return nil, fmt.Errorf("Reminder activation state could not be read: %w", err)
	}
	if s.data.Version != 1 || s.data.Bindings == nil || len(s.data.Bindings) > 1000 {
		store.Close()
		return nil, fmt.Errorf("Reminder activation state is unsupported")
	}
	a.reminders = s
	return s, nil
}
func (a *App) startReminderLoop() {
	if !reminderBundleAvailable() {
		return
	}
	a.startReminderPublisher(a.reminderDesktopState, 3*time.Second)
}
func (a *App) startReminderPublisher(open func() (*reminderDesktop, error), interval time.Duration) {
	a.reminderBootstrap.Do(func() {
		go func() {
			retry := time.NewTicker(interval)
			defer retry.Stop()
			for {
				select {
				case <-a.shutdownDone:
					return
				default:
				}
				a.lifecycle.Lock()
				s, err := open()
				if err == nil {
					s.mu.Lock()
					select {
					case <-a.shutdownDone:
						s.mu.Unlock()
						a.lifecycle.Unlock()
						return
					default:
					}
					if s.loopStarted {
						s.mu.Unlock()
						a.lifecycle.Unlock()
						return
					}
					s.loopStarted = true
					s.status.Available = true
					if _, err = a.syncReminderPublication(s, ""); err != nil {
						s.status.Error = err.Error()
					}
					s.mu.Unlock()
					a.lifecycle.Unlock()
					a.runReminderLoop(s, interval)
					return
				}
				a.lifecycle.Unlock()
				// A predecessor may still own publication until its authenticated
				// handoff commits. Retry without opening the workspace database.
				select {
				case <-a.shutdownDone:
					return
				case <-retry.C:
				}
			}
		}()
	})
}
func (a *App) runReminderLoop(s *reminderDesktop, interval time.Duration) {
	defer close(s.done)
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-a.shutdownDone:
			return
		case <-timer.C:
			a.lifecycle.RLock()
			s.mu.Lock()
			if _, err := a.syncReminderPublication(s, ""); err != nil {
				s.status.Error = err.Error()
			}
			s.mu.Unlock()
			a.lifecycle.RUnlock()
		}
	}
}
func (a *App) stopReminderLoop() {
	a.mu.RLock()
	s := a.reminders
	a.mu.RUnlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	started := s.loopStarted
	s.mu.Unlock()
	// The helper deliberately keeps its last opted-in derived schedule while
	// the main application closes. Only the publication lock is released.
	if started {
		<-s.done
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.store.Close()
}
func (a *App) syncReminderPublication(s *reminderDesktop, authorization string) (ReminderDesktopStatus, error) {
	if s.handoff {
		return s.status, fmt.Errorf("Reminder publication is suspended during workspace handoff")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	k, e := a.kernel, a.execution
	if k == nil {
		return s.status, fmt.Errorf("workspace is not open")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	key, err := k.ReminderRegistrationKey(ctx, e)
	if err != nil {
		return s.status, err
	}
	generated, err := k.Reminders(ctx, e, time.Now())
	if err != nil {
		return s.status, err
	}
	if s.data.CurrentBinding == key && s.data.Published.Epoch != "" {
		if receipt, readErr := s.store.Receipt(s.data.Published); readErr == nil {
			raw, _ := json.Marshal(receipt.Deliveries)
			if string(raw) != s.lastReceipt || generated.Generated > 0 {
				if err = k.ImportReminderReceipt(ctx, e, s.data.Published, receipt); err != nil {
					return s.status, err
				}
				s.lastReceipt = string(raw)
			}
		}
	}
	activation := s.data.Bindings[key]
	if activation.Epoch == "" {
		activation.Epoch = workspace.NewID()
		s.data.Bindings[key] = activation
	}
	native, err := s.native(0)
	if err != nil {
		return s.status, err
	}
	next, err := k.ReminderSchedule(ctx, e)
	if err != nil {
		return s.status, err
	}
	next.Epoch = activation.Epoch
	next.Active = activation.Enabled && native == "enabled"
	next.AuthorizationRequest = authorization
	prior := s.data.Published
	prior.Generation = 0
	prior.AuthorizationRequest = ""
	compare := next
	compare.Generation = 0
	compare.AuthorizationRequest = ""
	oldBytes, _ := json.Marshal(prior)
	newBytes, _ := json.Marshal(compare)
	if string(oldBytes) != string(newBytes) || authorization != "" || s.data.CurrentBinding != key {
		next, err = s.store.Publish(next)
		if err != nil {
			return s.status, err
		}
		s.data.Published = next
		s.data.CurrentBinding = key
		if err = s.store.Write("desktop.json", s.data); err != nil {
			return s.status, err
		}
	}
	s.status = ReminderDesktopStatus{Available: true, Enabled: activation.Enabled, Registration: native, Publication: "pending", Permission: "unknown", Epoch: s.data.Published.Epoch, Generation: s.data.Published.Generation}
	if receipt, readErr := s.store.Receipt(s.data.Published); readErr == nil {
		s.status.Permission = receipt.Permission
		s.status.Publication = "acknowledged"
		s.status.Error = receipt.Error
		ack, _ := time.Parse(time.RFC3339, receipt.AcknowledgedAt)
		if time.Since(ack) > 15*time.Second || ack.After(time.Now().Add(5*time.Second)) {
			s.status.Publication = "unknown"
			s.status.Error = "The helper has not reported recently. Reconcile before relying on background delivery."
		}
	} else if !activation.Enabled {
		s.status.Publication = "inactive"
	}
	return s.status, nil
}
func (a *App) ReminderStatus() (ReminderDesktopStatus, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return ReminderDesktopStatus{}, err
	}
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	s, err := a.reminderDesktopState()
	if err != nil {
		return ReminderDesktopStatus{Available: reminderBundleAvailable(), Registration: "unavailable", Publication: "inactive", Permission: "unknown", Error: err.Error()}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return a.syncReminderPublication(s, "")
}
func (a *App) RefreshReminders() (ReminderDesktopStatus, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return ReminderDesktopStatus{}, err
	}
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	s, err := a.reminderDesktopState()
	if err != nil {
		return ReminderDesktopStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status, err := a.syncReminderPublication(s, "")
	if err != nil || !status.Enabled {
		return status, err
	}
	// Reconcile requires a fresh acknowledgement even if no schedule values
	// changed. An old matching receipt cannot establish a live helper.
	next := s.data.Published
	next.AuthorizationRequest = ""
	if next, err = s.store.Publish(next); err != nil {
		return s.status, err
	}
	s.data.Published = next
	s.status.Generation = next.Generation
	s.status.Publication = "pending"
	if err = s.store.Write("desktop.json", s.data); err != nil {
		return s.status, err
	}
	return a.waitReminderAcknowledgement(s)
}
func reminderAcknowledgementFresh(raw string) bool {
	ack, err := time.Parse(time.RFC3339, raw)
	now := time.Now()
	return err == nil && now.Sub(ack) <= 15*time.Second && !ack.After(now.Add(5*time.Second))
}
func (a *App) waitReminderAcknowledgement(s *reminderDesktop) (ReminderDesktopStatus, error) {
	return a.waitReminderAcknowledgementUntil(s, 8*time.Second)
}
func (a *App) waitReminderAcknowledgementUntil(s *reminderDesktop, timeout time.Duration) (ReminderDesktopStatus, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if r, err := s.store.Receipt(s.data.Published); err == nil && reminderAcknowledgementFresh(r.AcknowledgedAt) {
			s.status.Permission = r.Permission
			s.status.Publication = "acknowledged"
			s.status.Error = r.Error
			return s.status, nil
		}
		select {
		case <-a.shutdownDone:
			s.status.Publication = "unknown"
			return s.status, fmt.Errorf("application closed before reminder acknowledgement")
		case <-deadline.C:
			s.status.Publication = "unknown"
			s.status.Error = "Schedule acknowledgement is unknown. Open Login Items and Notifications settings, then reconcile."
			return s.status, nil
		case <-tick.C:
		}
	}
}
func (a *App) SetReminderBackgroundEnabled(enabled bool) (ReminderDesktopStatus, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return ReminderDesktopStatus{}, err
	}
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	s, err := a.reminderDesktopState()
	if err != nil {
		if !enabled && reminderBundleAvailable() {
			registration, revokeErr := reminderNativeService(2)
			return ReminderDesktopStatus{Available: true, Enabled: false, Registration: registration, Publication: "unknown", Permission: "unknown", Error: err.Error()}, errors.Join(err, revokeErr)
		}
		return ReminderDesktopStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !enabled {
		return a.disableReminderBackground(s)
	}

	if _, err = a.syncReminderPublication(s, ""); err != nil {
		return s.status, err
	}
	key := s.data.CurrentBinding
	activation := s.data.Bindings[key]
	activation.Enabled = enabled
	activation.Epoch = workspace.NewID()
	s.data.Bindings[key] = activation
	if err = s.store.Write("desktop.json", s.data); err != nil {
		return s.status, err
	}
	if _, err = s.native(1); err != nil {
		return s.status, err
	}
	if _, err = a.syncReminderPublication(s, workspace.NewID()); err != nil {
		return s.status, err
	}
	return a.waitReminderAcknowledgement(s)
}
func (a *App) disableReminderBackground(s *reminderDesktop) (ReminderDesktopStatus, error) {
	// Revocation must remain available when the workspace, delivery bound or
	// schedule export has failed. Attempt OS unregistration even after I/O failure.
	for binding, old := range s.data.Bindings {
		old.Enabled = false
		old.Epoch = workspace.NewID()
		s.data.Bindings[binding] = old
	}
	var failures []error
	if err := s.store.Write("desktop.json", s.data); err != nil {
		failures = append(failures, err)
	}
	s.status.Enabled = false
	s.status.Publication = "unknown"
	if next := s.data.Published; next.Epoch != "" {
		next.Active = false
		next.AuthorizationRequest = ""
		published, err := s.store.Publish(next)
		if err != nil {
			failures = append(failures, err)
		} else {
			s.data.Published = published
			if err = s.store.Write("desktop.json", s.data); err != nil {
				failures = append(failures, err)
			}
			if registration, _ := s.native(0); registration == "enabled" {
				_, _ = a.waitReminderAcknowledgement(s)
			} else {
				s.status.Publication = "inactive"
			}
		}
	}
	registration, err := s.native(2)
	s.status.Registration = registration
	failures = append(failures, err)
	return s.status, errors.Join(failures...)
}

func (a *App) suspendReminderWorkspace() error {
	a.mu.RLock()
	s := a.reminders
	a.mu.RUnlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Published.Epoch == "" {
		return nil
	}
	if _, err := a.syncReminderPublication(s, ""); err != nil {
		return err
	}
	next := s.data.Published
	next.Active = false
	next.AuthorizationRequest = ""
	var err error
	next, err = s.store.Publish(next)
	if err != nil {
		return err
	}
	s.data.Published = next
	if registration, _ := s.native(0); registration == "enabled" {
		status, err := a.waitReminderAcknowledgement(s)
		if err != nil {
			return err
		}
		if status.Publication != "acknowledged" {
			return fmt.Errorf("Previous workspace reminder cancellation is not acknowledged; disable background reminders or reconcile before switching")
		}
	}
	s.data.CurrentBinding = ""
	return s.store.Write("desktop.json", s.data)
}

func (a *App) reminderHandoff(active bool) {
	a.mu.RLock()
	s := a.reminders
	a.mu.RUnlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.handoff = active
	s.mu.Unlock()
}
