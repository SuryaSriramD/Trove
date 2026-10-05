//go:build darwin

package providers

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var macOSICUName = regexp.MustCompile(`^icudt[0-9]{2,3}[bl]\.dat$`)

// JavaScriptCore's Intl.Collator loads Apple's public ICU data from this fixed
// system directory. The runtime library alone is insufficient. Grant only data
// reads of validated root-owned assets, never directory contents or execution.
func macOSICUReadPolicy() (string, error) {
	const root = "/usr/share/icu"
	for _, path := range []string{"/usr", "/usr/share", root} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if !trustedICUObject(info, true) {
			return "", errors.New("operating-system locale directory is not trusted")
		}
	}
	dir, err := os.Open(root)
	if err != nil {
		return "", err
	}
	entries, err := dir.ReadDir(65)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if len(entries) > 64 {
		return "", errors.New("operating-system locale inventory exceeds limit")
	}
	paths := []string{}
	for _, entry := range entries {
		if !macOSICUName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !trustedICUObject(info, false) || info.Size() < 1 || info.Size() > 128<<20 {
			return "", errors.New("operating-system locale data is not trusted")
		}
		paths = append(paths, "(literal "+strconv.Quote(path)+")")
		if len(paths) > 8 {
			return "", errors.New("operating-system locale data inventory exceeds limit")
		}
	}
	if len(paths) == 0 {
		return "", nil
	}
	sort.Strings(paths)
	return "; Public, root-owned operating-system ICU data only.\n(allow file-read-data " + strings.Join(paths, " ") + ")\n", nil
}
func trustedICUObject(info os.FileInfo, directory bool) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0 && info.Mode().Perm()&0022 == 0 && info.Mode()&os.ModeSymlink == 0 && ((directory && info.IsDir()) || (!directory && info.Mode().IsRegular()))
}
