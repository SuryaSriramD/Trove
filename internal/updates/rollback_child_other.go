//go:build !darwin

package updates

import (
	"errors"
	"os"
)

const RollbackHandshakeVersion = 1

var ErrRollbackCleanupUnverified = errors.New("rollback child termination could not be verified")

type RollbackChild struct{}

func ReceiveRollbackChild() (*RollbackChild, error) {
	if os.Getenv("TROVE_ROLLBACK_HANDSHAKE") != "" {
		return nil, errors.New("verified rollback startup is unavailable on this platform")
	}
	return nil, nil
}
func (*RollbackChild) Target() RollbackTarget  { return RollbackTarget{} }
func (*RollbackChild) Failed() <-chan struct{} { return nil }
func (*RollbackChild) Abort()                  {}
func (*RollbackChild) ConfirmReady(RollbackTarget, func() error) error {
	return errors.New("verified rollback launch is unavailable on this platform")
}
