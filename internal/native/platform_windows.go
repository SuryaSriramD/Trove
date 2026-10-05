package native

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows does not implement exec.Cmd.ExtraFiles. Duplicate just the approved
// handles as inheritable; never inherit the parent's environment or handle table.
// Logical descriptor slots 3..N are mapped inside the verified C++ bootstrap.
type workerProcess struct {
	cmd       *exec.Cmd
	job       windows.Handle
	process   windows.Handle
	inherited []windows.Handle
}

func prepareWorker(cmd *exec.Cmd, files []*os.File, memory int64) (_ *workerProcess, err error) {
	p := &workerProcess{cmd: cmd}
	defer func() {
		if err != nil {
			p.close()
		}
	}()
	p.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 1
	limits.ProcessMemoryLimit = uintptr(memory)
	if _, err = windows.SetInformationJobObject(p.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	values := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil {
			return nil, errors.New("nil worker input handle")
		}
		var duplicate windows.Handle
		if err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &duplicate, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return nil, err
		}
		p.inherited = append(p.inherited, duplicate)
		values = append(values, strconv.FormatUint(uint64(duplicate), 10))
	}
	handles := make([]syscall.Handle, len(p.inherited))
	for i, h := range p.inherited {
		handles[i] = syscall.Handle(h)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, AdditionalInheritedHandles: handles}
	// The supervisor supplies Env explicitly. Do not accept a caller-provided map.
	for _, value := range cmd.Env {
		if strings.HasPrefix(strings.ToUpper(value), "TROVE_WORKER_HANDLES=") {
			return nil, errors.New("duplicate worker handle mapping")
		}
	}
	cmd.Env = append(cmd.Env, "TROVE_WORKER_HANDLES="+strings.Join(values, ","))
	return p, nil
}
func (p *workerProcess) closeInherited() {
	for _, h := range p.inherited {
		_ = windows.CloseHandle(h)
	}
	p.inherited = nil
}
func (p *workerProcess) start() error {
	defer p.closeInherited()
	if err := p.cmd.Start(); err != nil {
		return err
	}
	// The verified helper emits hello then waits for a request. No request or model
	// input is processed until assignment succeeds. Assignment failure is fatal.
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(p.cmd.Process.Pid))
	if err == nil {
		p.process = handle
		err = windows.AssignProcessToJobObject(p.job, handle)
	}
	if err != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		return fmt.Errorf("worker job assignment: %w", err)
	}
	return nil
}
func (p *workerProcess) close() {
	p.closeInherited()
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
	if p.process != 0 {
		_ = windows.CloseHandle(p.process)
		p.process = 0
	}
}
func (p *workerProcess) stop(force bool) error {
	// Cooperative cancellation already travelled over the independent pipe.
	// Windows has no equivalent SIGTERM; both escalation steps terminate the job.
	return windows.TerminateJobObject(p.job, 1)
}

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func (p *workerProcess) rss() (int64, error) {
	var counters struct {
		Size, PageFaults                                                                                                 uint32
		PeakWorkingSet, WorkingSet, QuotaPeakPaged, QuotaPaged, QuotaPeakNonPaged, QuotaNonPaged, Pagefile, PeakPagefile uintptr
	}
	counters.Size = uint32(unsafe.Sizeof(counters))
	ok, _, err := getProcessMemoryInfo.Call(uintptr(p.process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		return 0, err
	}
	return int64(counters.WorkingSet), nil
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func platformMemoryBytes() (int64, error) {
	var status struct {
		Length, Load                                                                          uint32
		TotalPhys, AvailPhys, TotalPage, AvailPage, TotalVirtual, AvailVirtual, AvailExtended uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return 0, err
	}
	return int64(status.TotalPhys), nil
}
func privateWorkerDirectory(prefix string) (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	// Create with an owner-only, protected DACL in the initial syscall. Children
	// inherit it; a permissive parent temp directory cannot expose staged bytes.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	for i := 0; i < 10; i++ {
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return "", err
		}
		path := filepath.Join(os.TempDir(), prefix+hex.EncodeToString(nonce[:]))
		wide, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return "", err
		}
		err = windows.CreateDirectory(wide, &sa)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return "", errors.New("cannot allocate private worker directory")
}
func workerEnvironment(dir string) []string {
	// SystemRoot is the only inherited setting; exec also requires it for Windows.
	return []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32"), "HOME=" + dir, "USERPROFILE=" + dir, "TEMP=" + dir, "TMP=" + dir, "TROVE_GPU_LAYERS=0"}
}
func availableModelBytes(path string) (uint64, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(wide, &free, nil, nil)
	return free, err
}
func isDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL) || errors.Is(err, windows.ERROR_DISK_QUOTA_EXCEEDED)
}

// The file itself is FlushFileBuffers'd before the atomic replace. Windows does
// not expose POSIX directory fsync through os.File; power-loss qualification is open.
func syncModelDirectory(string) error { return nil }
