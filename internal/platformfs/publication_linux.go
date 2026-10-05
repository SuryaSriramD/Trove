//go:build linux

package platformfs

import "golang.org/x/sys/unix"

func renamePublication(from, to string, replace bool) error {
	var flags uint
	if !replace {
		flags = unix.RENAME_NOREPLACE
	}
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, flags)
}
