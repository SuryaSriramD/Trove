//go:build windows

package platformfs

import (
	"golang.org/x/sys/windows"
	"path/filepath"
)

func createRecoveryDirectory(path string) error {
	if err := validateStoragePath(path); err != nil {
		return err
	}
	handles, err := lockStorageDirectories(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer closeStorageDirectories(handles)
	if err = requireStorageVolume(filepath.Dir(path), handles[len(handles)-1]); err != nil {
		return err
	}
	attrs, err := privateStorageAttributes()
	if err != nil {
		return err
	}
	name, err := windowsPath(path)
	if err != nil {
		return err
	}
	// ERROR_ALREADY_EXISTS stays an error: no existing destination is adopted.
	if err = windows.CreateDirectory(name, attrs); err != nil {
		return err
	}
	return EnsurePrivateDirectory(path)
}

func relocateRecoveryDirectory(source, destination string) error { return ErrDirectoryDurability }

func (p *RecoveryPublication) publishRecoveryFile(source, target string) error {
	if err := p.checkRoot(); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(filepath.Join(p.directory, filepath.Dir(filepath.FromSlash(target)))); err != nil {
		return err
	}
	if err := PublishFile(filepath.Join(p.directory, filepath.FromSlash(source)), filepath.Join(p.directory, filepath.FromSlash(target)), false); err != nil {
		return err
	}
	return p.checkRoot()
}
