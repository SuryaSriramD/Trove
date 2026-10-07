//go:build !windows

package updates

import (
	"context"
	"errors"
)

func (WindowsVerifier) Inspect(context.Context, string) (Application, error) {
	return Application{}, errors.New("native Windows Authenticode verification requires Windows; no signature fallback is available")
}
