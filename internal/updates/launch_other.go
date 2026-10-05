//go:build !darwin

package updates

import (
	"context"
	"errors"
)

func LaunchRollback(context.Context, string, []string, RollbackTarget) error {
	return errors.New("verified rollback launch is not implemented for this platform")
}
