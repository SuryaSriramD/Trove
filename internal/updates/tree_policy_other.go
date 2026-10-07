//go:build !windows

package updates

import (
	"errors"
	"io/fs"
	"os"
)

func inventoryMode(info fs.FileInfo) (uint32, error) {
	if info.Mode().Perm()&0022 != 0 || info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return 0, errors.New("application must not contain group/world-writable objects")
	}
	return uint32(info.Mode().Perm()), nil
}
func inventoryFilePolicy(*os.File) error { return nil }

func inventoryDirectoryPolicy(root *os.Root, name string, info fs.FileInfo) error { return nil }
