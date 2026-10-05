//go:build darwin

package updates

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
)

// LaunchRollback starts only the executable in a previously verified bundle.
// The native adapter supplies a scrubbed environment and explicit workspace.
// This must not be CommandContext: completing the handoff cancels the operation,
// but the new application must remain running after the old process quits.
func LaunchRollback(ctx context.Context, bundle string, environment []string, target RollbackTarget) error {
	if !filepath.IsAbs(bundle) || filepath.Ext(bundle) != ".app" {
		return errors.New("invalid verified rollback application location")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(bundle, "Contents", "MacOS", "Trove"))
	cmd.Env = environment
	cmd.Dir = "/"
	return launchRollbackProcess(ctx, cmd, target)
}
