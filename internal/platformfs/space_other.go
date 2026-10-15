//go:build !darwin && !linux && !windows

package platformfs

import "errors"

func AvailableBytes(path string) (uint64, error) {
	return 0, errors.New("available disk space is unsupported on this platform")
}
