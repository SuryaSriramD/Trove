package native

import (
	"os"
	"path/filepath"
	"sync"
)

// NewPrivateStagingFile creates derived bytes beneath an owner-only directory.
// On Windows the protected DACL is installed before any file is created. Callers
// must invoke cleanup on every outcome; canonical assets never use this helper.
func NewPrivateStagingFile() (*os.File, func(), error) {
	dir, err := privateWorkerDirectory("trove-staging-")
	if err != nil {
		return nil, func() {}, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "derived"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, func() {}, err
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { _ = file.Close(); _ = os.RemoveAll(dir) }) }
	return file, cleanup, nil
}
