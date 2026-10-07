//go:build !darwin && !linux && !windows

package platformfs

import (
	"errors"
	"os"
)

func unsupported() error {
	return errors.New("workspace filesystem primitives are not implemented on this operating system")
}
func openRegular(string) (*os.File, error)    { return nil, unsupported() }
func openLock(string) (*os.File, error)       { return nil, unsupported() }
func lockFile(*os.File) error                 { return unsupported() }
func unlockFile(*os.File) error               { return unsupported() }
func FileIdentity(*os.File) (string, error)   { return "", unsupported() }
func auditPrivateHandle(*os.File, bool) error { return unsupported() }
