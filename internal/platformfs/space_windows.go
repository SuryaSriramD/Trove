//go:build windows

package platformfs

import "golang.org/x/sys/windows"

func AvailableBytes(path string) (uint64, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(wide, &free, nil, nil)
	return free, err
}
