package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ClaudeAuth contains only the public fields returned by `claude auth status`.
// Trove never opens, copies, parses or migrates vendor credential files.
type ClaudeAuth struct {
	LoggedIn     bool   `json:"loggedIn"`
	Method       string `json:"method"`
	Account      string `json:"account,omitempty"`
	AccountLabel string `json:"accountLabel,omitempty"`
	BillingMode  string `json:"billingMode,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func claudeVersionSupported(r Runtime) bool {
	match := regexp.MustCompile(`^2\.1\.([0-9]+)$`).FindStringSubmatch(r.Version)
	if len(match) != 2 {
		return false
	}
	patch, _ := strconv.Atoi(match[1])
	return r.Available && r.Provider == "claude_code" && patch >= 268
}
func claudeEnv(state string) []string {
	return append(isolatedEnv(state), "CLAUDE_CONFIG_DIR="+filepath.Join(state, "vendor"), "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "ENABLE_TOOL_SEARCH=false")
}

// QualifyClaudeRuntime checks actual contained startup, not just --version.
// It uses a fresh empty profile, no network and no account or inference command.
func QualifyClaudeRuntime(ctx context.Context, r Runtime) Runtime {
	if !claudeVersionSupported(r) {
		r.Available = false
		r.Reason = "Claude Code requires the supported 2.1.268+ protocol and native macOS containment"
		return r
	}
	digest, err := runtimeDigest(r.Path)
	if err != nil || digest != r.SHA256 {
		r.Available = false
		r.Reason = "Claude runtime changed; discover and review again"
		return r
	}
	state, err := os.MkdirTemp("", "trove-claude-readiness-")
	if err != nil {
		r.Available = false
		r.Reason = "Could not create a private Claude startup probe"
		return r
	}
	defer os.RemoveAll(state)
	state, _ = filepath.EvalSymlinks(state)
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd, err := containedCommand(bounded, r.Path, state, []string{"auth", "login", "--help"}, claudeEnv(state), false)
	if err != nil {
		r.Available = false
		r.Reason = "Claude containment is unavailable"
		return r
	}
	var output limitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil || !strings.Contains(output.String(), "auth login") {
		r.Available = false
		r.Reason = "Claude Code cannot complete native startup inside the required containment. Managed login and execution are unavailable; no unsandboxed fallback is used."
		return r
	}
	r.Reason = ""
	return r
}

func prepareClaudeState(r Runtime, state string) (string, string, error) {
	if !claudeVersionSupported(r) || !filepath.IsAbs(state) || strings.ContainsAny(state, "\x00\n\r") {
		return "", "", errors.New("invalid private Claude runtime profile")
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return "", "", err
	}
	state, err := filepath.EvalSymlinks(state)
	if err != nil {
		return "", "", err
	}
	if err = os.MkdirAll(filepath.Join(state, "vendor"), 0700); err != nil {
		return "", "", err
	}
	binary, err := SnapshotRuntime(filepath.Join(filepath.Dir(state), "runtimes"), r)
	return binary, state, err
}
func decodeClaudeAuth(raw []byte) (ClaudeAuth, error) {
	var source struct {
		LoggedIn       bool   `json:"loggedIn"`
		AuthMethod     string `json:"authMethod"`
		APIProvider    string `json:"apiProvider"`
		Email          string `json:"email"`
		OrgID          string `json:"orgId"`
		OrganizationID string `json:"organizationId"`
		AccountID      string `json:"accountId"`
	}
	var result ClaudeAuth
	if len(raw) > 64<<10 || json.Unmarshal(raw, &source) != nil {
		return result, errors.New("invalid public Claude authentication status")
	}
	result.LoggedIn, result.Method = source.LoggedIn, source.AuthMethod
	if !source.LoggedIn {
		result.Reason = "Sign in through the official runtime in this private profile"
		return result, nil
	}
	switch source.AuthMethod {
	case "claude.ai":
		result.BillingMode = "claude_subscription"
	case "api_key":
		result.BillingMode = "anthropic_api"
	default:
		result.Reason = "This vendor authentication method is not qualified for managed execution"
		return result, nil
	}
	if source.APIProvider != "firstParty" {
		result.Reason = "This vendor inference provider has not been qualified"
		return result, nil
	}
	organization := source.OrgID
	if organization == "" {
		organization = source.OrganizationID
	}
	if source.Email == "" && source.AccountID == "" {
		result.Reason = "The vendor did not return a stable public account identity"
		return result, nil
	}
	if len(source.Email) > 512 || len(source.AccountID) > 256 || len(organization) > 256 {
		return result, errors.New("invalid public Claude account identity")
	}
	label := strings.ToLower(strings.TrimSpace(source.Email))
	if label == "" {
		label = source.AccountID
	}
	hash := sha256.Sum256([]byte(source.AuthMethod + "\x00" + source.APIProvider + "\x00" + source.AccountID + "\x00" + label + "\x00" + organization))
	result.Account, result.AccountLabel = hex.EncodeToString(hash[:]), label
	return result, nil
}
func ClaudeAuthStatus(ctx context.Context, r Runtime, state string) (ClaudeAuth, error) {
	binary, state, err := prepareClaudeState(r, state)
	if err != nil {
		return ClaudeAuth{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd, err := containedCommand(bounded, binary, state, []string{"auth", "status"}, claudeEnv(state), false)
	if err != nil {
		return ClaudeAuth{}, err
	}
	var output limitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	status, err := decodeClaudeAuth([]byte(output.String()))
	if err != nil {
		return status, err
	}
	if runErr != nil && status.LoggedIn {
		return ClaudeAuth{}, errors.New("Claude authentication status failed")
	}
	return status, nil
}

// ClaudeAuthLoginCommand is an explicit native terminal hook, not a Trove OAuth
// implementation. The host must attach vendor-owned terminal/PTY I/O directly,
// never persist/inspect its input or output, and cancel this context on close.
// No command is returned when the actual contained startup probe fails.
func ClaudeAuthLoginCommand(ctx context.Context, r Runtime, state, billing string) (*exec.Cmd, error) {
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Minute {
		return nil, errors.New("native Claude login requires a cancellable deadline of at most ten minutes")
	}
	if billing != "claude_subscription" && billing != "anthropic_api" {
		return nil, errors.New("select an explicit Claude billing mode")
	}
	qualified := QualifyClaudeRuntime(ctx, r)
	if !qualified.Available {
		return nil, errors.New(qualified.Reason)
	}
	binary, state, err := prepareClaudeState(r, state)
	if err != nil {
		return nil, err
	}
	args := []string{"auth", "login"}
	if billing == "anthropic_api" {
		args = append(args, "--console")
	}
	return claudeLoginCommand(ctx, binary, state, args, claudeEnv(state))
}

// Login uses the same deny-inbound containment as ordinary runs. The vendor's
// documented manual browser-code fallback must stay entirely in its native
// terminal. That flow remains unqualified until tested with a supported runtime:
// a Seatbelt localhost bind rule also admits wildcard listeners on this OS.
func claudeLoginCommand(ctx context.Context, binary, state string, args, env []string) (*exec.Cmd, error) {
	return containedCommand(ctx, binary, state, args, env, true)
}
func ClaudeAuthLogout(ctx context.Context, r Runtime, state string) error {
	binary, state, err := prepareClaudeState(r, state)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd, err := containedCommand(bounded, binary, state, []string{"auth", "logout"}, claudeEnv(state), false)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err = cmd.Run(); err != nil {
		return errors.New("The official runtime could not complete logout inside containment")
	}
	return nil
}
