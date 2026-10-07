//go:build darwin

package platformfs

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameRecoveryAt(from *os.File, source string, to *os.File, target string) error {
	return unix.RenameatxNp(int(from.Fd()), source, int(to.Fd()), target, unix.RENAME_EXCL)
}
