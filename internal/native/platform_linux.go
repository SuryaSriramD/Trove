package native

import "golang.org/x/sys/unix"

func platformMemoryBytes() (int64, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, err
	}
	return int64(info.Totalram) * int64(info.Unit), nil
}
