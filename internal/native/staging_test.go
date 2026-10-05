package native

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateStagingFileLifecycle(t *testing.T) {
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	directory := filepath.Dir(file.Name())
	if !strings.HasPrefix(filepath.Base(directory), "trove-staging-") || directory == os.TempDir() {
		t.Fatal("derived file exposed directly in ambient temp")
	}
	stat, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions %v", stat.Mode())
	}
	stat, err = file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0600 {
		t.Fatalf("file permissions %v", stat.Mode())
	}
	if _, err = file.WriteString("derived private bytes"); err != nil {
		t.Fatal(err)
	}
	cleanup()
	cleanup()
	if _, err = os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("staging directory remains after cleanup")
	}
	if _, err = file.WriteString("closed"); err == nil {
		t.Fatal("staging descriptor remains open")
	}
}
