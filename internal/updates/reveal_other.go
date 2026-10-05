//go:build !darwin

package updates

import (
	"context"
	"errors"
)

func Reveal(context.Context, string) error {
	return errors.New("verified application updates are currently available on macOS")
}
