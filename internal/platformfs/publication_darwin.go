//go:build darwin

package platformfs

import "golang.org/x/sys/unix"

func renamePublication(from, to string, replace bool) error {
	var flags uint32
	if !replace {
		flags = unix.RENAME_EXCL
	}
	return unix.RenamexNp(from, to, flags)
}
