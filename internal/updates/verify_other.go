//go:build !darwin

package updates

import (
	"context"
	"errors"
)

type MacVerifier struct{}

func (MacVerifier) Inspect(context.Context, string) (Application, error) {
	return Application{}, errors.New("verified application updates are not implemented for this platform")
}
