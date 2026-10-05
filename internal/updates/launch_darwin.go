//go:build darwin

package updates

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"
)

// LaunchRollback starts only the executable in a previously verified bundle.
// The native adapter supplies a scrubbed environment and explicit workspace.
// This must not be CommandContext: completing the handoff cancels the operation,
// but the new application must remain running after the old process quits.
func LaunchRollback(ctx context.Context, bundle string, environment []string) error {
	if !filepath.IsAbs(bundle) || filepath.Ext(bundle) != ".app" {
		return errors.New("invalid verified rollback application location")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(bundle, "Contents", "MacOS", "Trove"))
	cmd.Env = environment
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return errors.New("previous application could not be started; the current app remains open")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
