//go:build darwin && cgo

package native

/*
#cgo LDFLAGS: -lproc
#include <libproc.h>
#include <sys/proc_info.h>
#include <errno.h>
static int trove_process_rss(int pid, unsigned long long *rss) {
 struct proc_taskinfo info = {0};
 int n = proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &info, sizeof(info));
 if (n != sizeof(info)) return errno ? errno : EIO;
 *rss = info.pti_resident_size;
 return 0;
}
*/
import "C"

import (
	"errors"
	"syscall"
)

// libproc measures only the requested owned worker. Unlike setuid /bin/ps it
// remains available inside the offline sandbox and does not spawn a process.
func processRSS(pid int) (int64, error) {
	if pid <= 0 {
		return 0, errors.New("invalid worker process")
	}
	var rss C.ulonglong
	if code := C.trove_process_rss(C.int(pid), &rss); code != 0 {
		return 0, syscall.Errno(code)
	}
	if uint64(rss) == 0 || uint64(rss) > 1<<63-1 {
		return 0, errors.New("invalid worker RSS")
	}
	return int64(rss), nil
}
