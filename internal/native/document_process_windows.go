package native

import (
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"time"
	"unsafe"
)

func prepareDocumentProcess(cmd *exec.Cmd) (*workerProcess, error) {
	p, err := prepareWorker(cmd, nil, 2<<30)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 16
	limits.JobMemoryLimit = 2 << 30
	if _, err = windows.SetInformationJobObject(p.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}
func documentEnvironment(private string) []string        { return documentBaseEnvironment(private) }
func documentProcessRSS(p *workerProcess) (int64, error) { return p.rss() }
func documentMkdir(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.CreateDirectory(name, &attributes)
}

// Termination is asynchronous. Keep the job handle until all browser children
// have exited, so profile deletion cannot race live WebView processes.
func drainDocumentProcess(p *workerProcess) error {
	if err := p.stop(true); err != nil {
		return errors.New("document process tree termination failed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var info struct {
			TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
			TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
		}
		if err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return errors.New("cannot verify document process tree cleanup")
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("document process tree cleanup exceeded deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
