package providers

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type claudeLoginControl struct {
	Version     int                `json:"version"`
	Attempt     ClaudeLoginAttempt `json:"attempt"`
	Runtime     Runtime            `json:"runtime"`
	RuntimePath string             `json:"runtimePath"`
	BillingMode string             `json:"billingMode"`
}
type claudeLoginReceipt struct {
	Version     int       `json:"version"`
	AttemptID   string    `json:"attemptId"`
	State       string    `json:"state"`
	Message     string    `json:"message"`
	CompletedAt time.Time `json:"completedAt"`
}

// RunClaudeLoginHelper is the native app's early CLI entry point. No Wails
// bridge is started. Only the vendor process receives the terminal descriptors;
// Trove never reads, buffers, parses or logs terminal input or output.
func RunClaudeLoginHelper(controlPath string) int {
	root, err := DefaultClaudeProfilesRoot()
	if err != nil {
		return 1
	}
	return runClaudeLoginHelper(root, controlPath, os.Stdin, os.Stdout, os.Stderr, ClaudeAuthLoginCommand)
}
func runClaudeLoginHelper(root, controlPath string, input io.Reader, output, diagnostic io.Writer, login func(context.Context, Runtime, string, string) (*exec.Cmd, error)) int {
	control, dir, err := validateClaudeLoginControl(root, controlPath)
	if err != nil {
		return 1
	}
	// The app's profile lock remains owned by its UI process. This independent,
	// exclusive claim prevents a terminal retry from running the same login twice.
	claim, err := os.OpenFile(filepath.Join(dir, "started"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 1
	}
	if claim.Close() != nil {
		return 1
	}
	receipt := claudeLoginReceipt{Version: 1, AttemptID: control.Attempt.ID, State: "failed", Message: "The official runtime did not complete sign-in", CompletedAt: time.Now().UTC()}
	finish := func(code int) int {
		receipt.CompletedAt = time.Now().UTC()
		if writePrivateClaudeJSON(filepath.Join(dir, "receipt.json"), receipt) != nil {
			return 1
		}
		return code
	}
	cancelled := func() bool {
		_, err := os.Lstat(filepath.Join(dir, "cancel"))
		return err == nil || !os.IsNotExist(err)
	}
	if cancelled() {
		receipt.State, receipt.Message = "cancelled", "Vendor login was cancelled"
		return finish(1)
	}
	terminal, stopTerminal := claudeTerminalContext()
	defer stopTerminal()
	ctx, cancel := context.WithDeadline(terminal, control.Attempt.ExpiresAt)
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		timer := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-timer.C:
				if cancelled() {
					cancel()
					return
				}
			}
		}
	}()
	control.Runtime.Path = control.RuntimePath
	state := filepath.Join(root, "profiles", control.Attempt.ProfileID, "state")
	cmd, err := login(ctx, control.Runtime, state, control.BillingMode)
	if err == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, diagnostic
		err = cmd.Run()
	}
	if cancelled() || ctx.Err() != nil {
		receipt.State, receipt.Message = "cancelled", "Vendor login was cancelled or reached its time limit"
		return finish(1)
	}
	if err != nil {
		return finish(1)
	}
	receipt.State, receipt.Message = "completed", "Vendor login command finished. Refresh public account status before selecting this profile."
	return finish(0)
}
func validateClaudeLoginControl(root, path string) (claudeLoginControl, string, error) {
	var control claudeLoginControl
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return control, "", errors.New("Invalid private login path")
	}
	directory := filepath.Dir(path)
	id := filepath.Base(directory)
	if uuid.Validate(id) != nil || path != filepath.Join(root, "attempts", id, "control.json") {
		return control, "", errors.New("Login control is outside private host storage")
	}
	for _, p := range []string{root, filepath.Join(root, "attempts"), directory} {
		if err := validatePrivateClaudeFile(p, true, 0); err != nil {
			return control, "", err
		}
	}
	if err := readPrivateClaudeJSON(path, 16<<10, &control); err != nil {
		return control, "", err
	}
	if control.Version != 1 || control.Attempt.ID != id || uuid.Validate(control.Attempt.ProfileID) != nil || control.Attempt.State != "waiting" || !time.Now().Before(control.Attempt.ExpiresAt) || time.Until(control.Attempt.ExpiresAt) > 10*time.Minute || !claudeVersionSupported(control.Runtime) || !filepath.IsAbs(control.RuntimePath) || strings.ContainsAny(control.RuntimePath, "\x00\r\n") || (control.BillingMode != "claude_subscription" && control.BillingMode != "anthropic_api") {
		return control, "", errors.New("Native login control is invalid or expired")
	}
	for _, p := range []string{filepath.Join(root, "profiles"), filepath.Join(root, "profiles", control.Attempt.ProfileID), filepath.Join(root, "profiles", control.Attempt.ProfileID, "state")} {
		if err := validatePrivateClaudeFile(p, true, 0); err != nil {
			return control, "", err
		}
	}
	// An attempt must still be the selected profile's current request.
	var meta claudeProfileMetadata
	if err := readPrivateClaudeJSON(filepath.Join(root, "profiles.json"), 256<<10, &meta); err != nil {
		return control, "", err
	}
	matched := false
	for _, profile := range meta.Profiles {
		if profile.ID == control.Attempt.ProfileID && profile.AttemptID == id && profile.BillingMode == control.BillingMode {
			matched = true
		}
	}
	if meta.Version != 1 || !matched {
		return control, "", errors.New("Login attempt no longer belongs to the selected profile")
	}
	return control, directory, nil
}
