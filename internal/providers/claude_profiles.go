package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ClaudeLoginAttempt struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profileId"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type ClaudeProfile struct {
	SignOutIncomplete bool                `json:"signOutIncomplete"`
	ID                string              `json:"id"`
	Label             string              `json:"label"`
	BillingMode       string              `json:"billingMode"`
	CreatedAt         time.Time           `json:"createdAt"`
	Auth              ClaudeAuth          `json:"auth"`
	Attempt           *ClaudeLoginAttempt `json:"attempt,omitempty"`
}
type claudeProfileRecord struct {
	ID                string    `json:"id"`
	Label             string    `json:"label"`
	BillingMode       string    `json:"billingMode"`
	CreatedAt         time.Time `json:"createdAt"`
	AttemptID         string    `json:"attemptId,omitempty"`
	SignOutIncomplete bool      `json:"signOutIncomplete,omitempty"`
}
type claudeProfileMetadata struct {
	Version  int                   `json:"version"`
	Profiles []claudeProfileRecord `json:"profiles"`
}
type ClaudeProfiles struct {
	mu         sync.Mutex
	root       string
	lock       *os.File
	meta       claudeProfileMetadata
	statuses   map[string]ClaudeAuth
	closed     bool
	qualify    func(context.Context, Runtime) Runtime
	status     func(context.Context, Runtime, string) (ClaudeAuth, error)
	logout     func(context.Context, Runtime, string) error
	launch     func(context.Context, string) error
	executable func() (string, error)
}

func DefaultClaudeProfilesRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("Private Claude profile storage is unavailable")
	}
	return filepath.Join(base, "Trove", "provider-claude"), nil
}
func NewClaudeProfiles(root string) (*ClaudeProfiles, error) {
	lock, err := lockClaudeProfileRoot(root)
	if err != nil {
		return nil, err
	}
	m := &ClaudeProfiles{root: root, lock: lock, statuses: map[string]ClaudeAuth{}, qualify: QualifyClaudeRuntime, status: ClaudeAuthStatus, logout: ClaudeAuthLogout, launch: launchClaudeTerminal, executable: os.Executable, meta: claudeProfileMetadata{Version: 1, Profiles: []claudeProfileRecord{}}}
	for _, name := range []string{"profiles", "attempts"} {
		if err = privateClaudeDirectory(filepath.Join(root, name)); err != nil {
			lock.Close()
			return nil, err
		}
	}
	path := filepath.Join(root, "profiles.json")
	if _, err = os.Lstat(path); os.IsNotExist(err) {
		err = m.saveLocked()
	} else if err == nil {
		err = readPrivateClaudeJSON(path, 256<<10, &m.meta)
	}
	if err != nil || m.meta.Version != 1 || len(m.meta.Profiles) > 32 {
		lock.Close()
		return nil, errors.New("Private Claude profile metadata is invalid")
	}
	seen := map[string]bool{}
	for _, p := range m.meta.Profiles {
		if !validClaudeProfileRecord(p) || seen[p.ID] {
			lock.Close()
			return nil, errors.New("Private Claude profile metadata is invalid")
		}
		seen[p.ID] = true
	}
	return m, nil
}
func validClaudeProfileRecord(p claudeProfileRecord) bool {
	return uuid.Validate(p.ID) == nil && len(strings.TrimSpace(p.Label)) > 0 && len(p.Label) <= 120 && utf8.ValidString(p.Label) && !strings.ContainsAny(p.Label, "\x00\r\n") && (p.BillingMode == "claude_subscription" || p.BillingMode == "anthropic_api") && !p.CreatedAt.IsZero() && (p.AttemptID == "" || uuid.Validate(p.AttemptID) == nil)
}
func (m *ClaudeProfiles) saveLocked() error {
	return writePrivateClaudeJSON(filepath.Join(m.root, "profiles.json"), m.meta)
}
func (m *ClaudeProfiles) recordLocked(id string) (*claudeProfileRecord, error) {
	if m.closed {
		return nil, errors.New("Claude profile manager is closed")
	}
	for i := range m.meta.Profiles {
		if m.meta.Profiles[i].ID == id {
			return &m.meta.Profiles[i], nil
		}
	}
	return nil, errors.New("Claude profile was not found")
}
func (m *ClaudeProfiles) statePath(id string) string {
	return filepath.Join(m.root, "profiles", id, "state")
}
func (m *ClaudeProfiles) attemptPath(id string) string { return filepath.Join(m.root, "attempts", id) }
func (m *ClaudeProfiles) publicLocked(p claudeProfileRecord) (ClaudeProfile, error) {
	result := ClaudeProfile{SignOutIncomplete: p.SignOutIncomplete, ID: p.ID, Label: p.Label, BillingMode: p.BillingMode, CreatedAt: p.CreatedAt, Auth: m.statuses[p.ID]}
	if p.SignOutIncomplete {
		result.Auth.Reason = "Vendor logout has not completed; managed execution is disabled"
	} else if result.Auth.Method == "" {
		result.Auth.Reason = "Refresh status through the official runtime"
	}
	if p.AttemptID != "" {
		a, err := m.attemptLocked(p)
		if err != nil {
			return result, err
		}
		result.Attempt = &a
	}
	return result, nil
}
func (m *ClaudeProfiles) List() ([]ClaudeProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("Claude profile manager is closed")
	}
	out := []ClaudeProfile{}
	for _, p := range m.meta.Profiles {
		v, err := m.publicLocked(p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (m *ClaudeProfiles) Create(label, billing string) (ClaudeProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.meta.Profiles) >= 32 {
		return ClaudeProfile{}, errors.New("Claude profile creation is unavailable")
	}
	p := claudeProfileRecord{ID: uuid.NewString(), Label: strings.TrimSpace(label), BillingMode: billing, CreatedAt: time.Now().UTC()}
	if !validClaudeProfileRecord(p) {
		return ClaudeProfile{}, errors.New("Enter a profile label and select an explicit billing mode")
	}
	for _, path := range []string{filepath.Dir(m.statePath(p.ID)), m.statePath(p.ID)} {
		if err := privateClaudeDirectory(path); err != nil {
			return ClaudeProfile{}, err
		}
	}
	m.meta.Profiles = append(m.meta.Profiles, p)
	if err := m.saveLocked(); err != nil {
		m.meta.Profiles = m.meta.Profiles[:len(m.meta.Profiles)-1]
		return ClaudeProfile{}, err
	}
	return m.publicLocked(p)
}
func (m *ClaudeProfiles) Status(ctx context.Context, r Runtime, id string) (ClaudeProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.recordLocked(id)
	if err != nil {
		return ClaudeProfile{}, err
	}
	if err = m.requireIdleLocked(*p); err != nil {
		return ClaudeProfile{}, err
	}
	status, err := m.status(ctx, r, m.statePath(id))
	if err != nil {
		return ClaudeProfile{}, err
	}
	m.statuses[id] = status
	return m.publicLocked(*p)
}
func (m *ClaudeProfiles) requireIdleLocked(p claudeProfileRecord) error {
	if p.AttemptID == "" {
		return nil
	}
	a, err := m.attemptLocked(p)
	if err != nil {
		return err
	}
	if claudeLoginPending(a.State) {
		return errors.New("Finish or cancel the current vendor login before using this profile")
	}
	return nil
}
func claudeLoginPending(state string) bool {
	return state == "waiting" || state == "running" || state == "cancellation_requested"
}
func (m *ClaudeProfiles) Login(ctx context.Context, r Runtime, id string) (ClaudeLoginAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.recordLocked(id)
	if err != nil {
		return ClaudeLoginAttempt{}, err
	}
	if p.SignOutIncomplete {
		return ClaudeLoginAttempt{}, errors.New("Finish vendor logout before signing in to this profile again")
	}
	// One login can own the native terminal at a time, across all private profiles.
	for _, profile := range m.meta.Profiles {
		if err = m.requireIdleLocked(profile); err != nil {
			return ClaudeLoginAttempt{}, err
		}
	}
	qualified := m.qualify(ctx, r)
	if !qualified.Available {
		return ClaudeLoginAttempt{}, errors.New(qualified.Reason)
	}
	exe, err := m.executable()
	if err != nil || !filepath.IsAbs(exe) || strings.ContainsAny(exe, "\x00\r\n") {
		return ClaudeLoginAttempt{}, errors.New("The native Trove login helper is unavailable")
	}
	attempt := ClaudeLoginAttempt{ID: uuid.NewString(), ProfileID: id, State: "waiting", Message: "Complete sign-in in the vendor terminal. Trove does not read codes or terminal output.", ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	directory := m.attemptPath(attempt.ID)
	if err = privateClaudeDirectory(directory); err != nil {
		return ClaudeLoginAttempt{}, err
	}
	control := claudeLoginControl{Version: 1, Attempt: attempt, Runtime: qualified, RuntimePath: qualified.Path, BillingMode: p.BillingMode}
	if err = writePrivateClaudeJSON(filepath.Join(directory, "control.json"), control); err != nil {
		return ClaudeLoginAttempt{}, err
	}
	script := filepath.Join(directory, "Sign in to Claude.command")
	body := "#!/bin/sh\nexec " + quoteClaudeShell(exe) + " --trove-claude-login " + quoteClaudeShell(filepath.Join(directory, "control.json")) + "\n"
	if err = os.WriteFile(script, []byte(body), 0700); err != nil {
		return ClaudeLoginAttempt{}, errors.New("The native login launcher could not be prepared")
	}
	previous := p.AttemptID
	p.AttemptID = attempt.ID
	if err = m.saveLocked(); err != nil {
		p.AttemptID = previous
		return ClaudeLoginAttempt{}, err
	}
	delete(m.statuses, id)
	if err = m.launch(ctx, script); err != nil {
		_ = writePrivateClaudeJSON(filepath.Join(directory, "receipt.json"), claudeLoginReceipt{Version: 1, AttemptID: attempt.ID, State: "failed", Message: "The vendor terminal could not be opened", CompletedAt: time.Now().UTC()})
		return ClaudeLoginAttempt{}, errors.New("The vendor terminal could not be opened")
	}
	return attempt, nil
}
func quoteClaudeShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
func (m *ClaudeProfiles) Cancel(id string) (ClaudeProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.recordLocked(id)
	if err != nil {
		return ClaudeProfile{}, err
	}
	if p.AttemptID != "" {
		if err = m.cancelLocked(*p); err != nil {
			return ClaudeProfile{}, err
		}
	}
	return m.publicLocked(*p)
}
func (m *ClaudeProfiles) cancelLocked(p claudeProfileRecord) error {
	a, err := m.attemptLocked(p)
	if err != nil {
		return err
	}
	if !claudeLoginPending(a.State) {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(m.attemptPath(p.AttemptID), "cancel"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("The vendor login cancellation could not be saved")
	}
	if err = f.Close(); err != nil {
		return err
	}
	if _, err = os.Lstat(filepath.Join(m.attemptPath(p.AttemptID), "started")); os.IsNotExist(err) {
		return writePrivateClaudeJSON(filepath.Join(m.attemptPath(p.AttemptID), "receipt.json"), claudeLoginReceipt{Version: 1, AttemptID: p.AttemptID, State: "cancelled", Message: "Vendor login was cancelled before starting", CompletedAt: time.Now().UTC()})
	}
	return nil
}
func (m *ClaudeProfiles) Logout(ctx context.Context, r Runtime, id string) (ClaudeProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.recordLocked(id)
	if err != nil {
		return ClaudeProfile{}, err
	}
	if err = m.requireIdleLocked(*p); err != nil {
		return ClaudeProfile{}, err
	}
	p.SignOutIncomplete = true
	if err = m.saveLocked(); err != nil {
		return ClaudeProfile{}, err
	}
	// Drop any previously usable status even if the vendor cannot finish logout.
	m.statuses[id] = ClaudeAuth{Reason: "Vendor logout has not completed; managed execution is disabled"}
	if err = m.logout(ctx, r, m.statePath(id)); err != nil {
		return ClaudeProfile{}, err
	}
	status, err := m.status(ctx, r, m.statePath(id))
	if err != nil || status.LoggedIn {
		return ClaudeProfile{}, errors.New("The official runtime did not confirm signed-out status")
	}
	p.SignOutIncomplete = false
	if err = m.saveLocked(); err != nil {
		p.SignOutIncomplete = true
		return ClaudeProfile{}, err
	}
	m.statuses[id] = status
	return m.publicLocked(*p)
}
func (m *ClaudeProfiles) Resolve(ctx context.Context, r Runtime, id, billing string) (ClaudeConfig, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.recordLocked(id)
	if err != nil {
		return ClaudeConfig{}, "", err
	}
	if p.SignOutIncomplete {
		return ClaudeConfig{}, "", errors.New("Finish vendor logout before using this profile")
	}
	if p.BillingMode != billing {
		return ClaudeConfig{}, "", errors.New("Selected Claude billing does not match this profile")
	}
	if err = m.requireIdleLocked(*p); err != nil {
		return ClaudeConfig{}, "", err
	}
	qualified := m.qualify(ctx, r)
	if !qualified.Available {
		return ClaudeConfig{}, "", errors.New(qualified.Reason)
	}
	status, err := m.status(ctx, qualified, m.statePath(id))
	if err != nil {
		return ClaudeConfig{}, "", err
	}
	m.statuses[id] = status
	if !status.LoggedIn || status.Reason != "" || status.Account == "" || status.BillingMode != billing {
		return ClaudeConfig{}, "", errors.New("Refresh or sign in to the selected vendor account and billing mode")
	}
	label := status.AccountLabel
	if label == "" {
		label = p.Label
	}
	return ClaudeConfig{Runtime: qualified, StateDirectory: m.statePath(id), Identity: Identity{Provider: "claude_code", Account: status.Account, AuthMode: "vendor_owned", BillingMode: billing, RuntimeVersion: qualified.Version, AdapterVersion: "2"}}, label, nil
}
func (m *ClaudeProfiles) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	var result error
	for _, p := range m.meta.Profiles {
		if p.AttemptID != "" {
			if err := m.cancelLocked(p); err != nil {
				result = err
			}
		}
	}
	m.closed = true
	if err := m.lock.Close(); err != nil && result == nil {
		result = err
	}
	return result
}
func readPrivateClaudeJSON(path string, limit int64, value any) error {
	if err := validatePrivateClaudeFile(path, false, limit); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) > limit || json.Unmarshal(raw, value) != nil {
		return errors.New("Private Claude control data is invalid")
	}
	return nil
}
func writePrivateClaudeJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 256<<10 {
		return errors.New("Private Claude metadata is invalid")
	}
	if err = validatePrivateClaudeFile(filepath.Dir(path), true, 0); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".claude-*")
	if err != nil {
		return errors.New("Private Claude metadata could not be saved")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("Private Claude metadata could not be saved")
	}
	return nil
}
func (m *ClaudeProfiles) attemptLocked(p claudeProfileRecord) (ClaudeLoginAttempt, error) {
	var control claudeLoginControl
	dir := m.attemptPath(p.AttemptID)
	if err := readPrivateClaudeJSON(filepath.Join(dir, "control.json"), 16<<10, &control); err != nil {
		return ClaudeLoginAttempt{}, err
	}
	a := control.Attempt
	if control.Version != 1 || a.ID != p.AttemptID || a.ProfileID != p.ID || a.ExpiresAt.IsZero() {
		return a, errors.New("Private Claude login identity is invalid")
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	if _, err := os.Lstat(receiptPath); err == nil {
		var receipt claudeLoginReceipt
		if err = readPrivateClaudeJSON(receiptPath, 4096, &receipt); err != nil {
			return a, err
		}
		if receipt.Version != 1 || receipt.AttemptID != a.ID || (receipt.State != "completed" && receipt.State != "failed" && receipt.State != "cancelled") || len(receipt.Message) > 512 {
			return a, errors.New("Private Claude login receipt is invalid")
		}
		a.State, a.Message = receipt.State, receipt.Message
	} else if !os.IsNotExist(err) {
		return a, errors.New("Private Claude login receipt is unavailable")
	} else if !time.Now().Before(a.ExpiresAt) {
		a.State = "interrupted"
		a.Message = "Vendor login expired without a completion receipt"
	} else if _, err = os.Lstat(filepath.Join(dir, "cancel")); err == nil {
		a.State = "cancellation_requested"
		a.Message = "Waiting for the vendor terminal to stop"
	} else if _, err = os.Lstat(filepath.Join(dir, "started")); err == nil {
		a.State = "running"
		a.Message = "Sign in through the vendor terminal; Trove does not receive the login code"
	}
	return a, nil
}
