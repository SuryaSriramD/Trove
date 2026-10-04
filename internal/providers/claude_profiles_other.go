//go:build !darwin

package providers

import (
	"context"
	"errors"
	"os"
)

func validatePrivateClaudeFile(string, bool, int64) error {
	return errors.New("Managed Claude profiles require qualified native macOS containment")
}
func privateClaudeDirectory(string) error {
	return errors.New("Managed Claude profiles require qualified native macOS containment")
}
func lockClaudeProfileRoot(string) (*os.File, error) {
	return nil, errors.New("Managed Claude profiles require qualified native macOS containment")
}
func launchClaudeTerminal(context.Context, string) error {
	return errors.New("The native Claude terminal is unavailable on this platform")
}

func claudeTerminalContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
