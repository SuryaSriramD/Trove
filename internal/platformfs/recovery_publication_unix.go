//go:build darwin || linux

package platformfs

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func createRecoveryDirectory(directory string) error { return os.Mkdir(directory, 0700) }

// Pin each existing parent and create children relative to that descriptor. No
// absolute path mutation may precede validation: a hostile nested symlink must
// not create a directory or publish bytes outside the reserved workspace.
func recoveryParent(root *os.Root, relative string, create bool) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return current, nil
	}
	for _, component := range strings.Split(relative, "/") {
		if create {
			if err = current.Mkdir(component, 0700); err != nil && !os.IsExist(err) {
				current.Close()
				return nil, err
			}
		}
		named, err := current.Lstat(component)
		if err != nil {
			current.Close()
			return nil, err
		}
		if !named.IsDir() || named.Mode()&os.ModeSymlink != 0 {
			current.Close()
			return nil, ErrPrivateStorage
		}
		child, err := current.OpenRoot(component)
		if err != nil {
			current.Close()
			return nil, err
		}
		descriptor, err := child.Open(".")
		if err == nil {
			err = AuditPrivateHandle(descriptor, true)
			if err == nil {
				var actual os.FileInfo
				actual, err = descriptor.Stat()
				if err == nil && !os.SameFile(named, actual) {
					err = ErrPrivateStorage
				}
			}
			descriptor.Close()
		}
		current.Close()
		if err != nil {
			child.Close()
			return nil, err
		}
		current = child
	}
	return current, nil
}
func (p *RecoveryPublication) publishRecoveryFile(source, target string) error {
	if err := p.checkRoot(); err != nil {
		return err
	}
	sourceParent, err := recoveryParent(p.root, path.Dir(source), false)
	if err != nil {
		return err
	}
	defer sourceParent.Close()
	targetParent, err := recoveryParent(p.root, path.Dir(target), true)
	if err != nil {
		return err
	}
	defer targetParent.Close()
	from, err := sourceParent.Open(".")
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := targetParent.Open(".")
	if err != nil {
		return err
	}
	defer to.Close()
	if err = renameRecoveryAt(from, path.Base(source), to, path.Base(target)); err != nil {
		return err
	}
	// Flush the pinned directories, not potentially replaced pathnames.
	return errors.Join(from.Sync(), to.Sync(), p.checkRoot())
}
func relocateRecoveryDirectory(source, destination string) error {
	if err := renamePublication(source, destination, false); err != nil {
		return err
	}
	if err := SyncDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	return SyncDirectory(filepath.Dir(source))
}
