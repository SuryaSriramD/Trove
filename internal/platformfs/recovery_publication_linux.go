//go:build linux

package platformfs

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameRecoveryAt(from *os.File, source string, to *os.File, target string) error {
	return unix.Renameat2(int(from.Fd()), source, int(to.Fd()), target, unix.RENAME_NOREPLACE)
}
