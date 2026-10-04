//go:build !darwin && !linux

package auth

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return false }
func lockRoot(string) (*os.File, error)   { return nil, ErrUnsupported }
