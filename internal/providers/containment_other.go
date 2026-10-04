//go:build !darwin

package providers

import (
	"context"
	"errors"
	"os/exec"
)

const ContainmentPolicy = "unavailable"

func containedCommand(context.Context, string, string, []string, []string, bool) (*exec.Cmd, error) {
	return nil, errors.New("managed provider containment is not implemented on this platform")
}
func containmentAvailable() bool { return false }
