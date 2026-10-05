//go:build !darwin && !linux && !windows

package auth

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return false }
func lockRoot(string) (*os.File, error)   { return nil, ErrUnsupported }
