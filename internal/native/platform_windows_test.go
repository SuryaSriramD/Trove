package native

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsWorkerRejectsInjectedHandleMapping(t *testing.T) {
	cmd := exec.Command("unstarted.exe")
	cmd.Env = []string{"TROVE_WORKER_HANDLES=123"}
	if p, err := prepareWorker(cmd, nil, 512<<20); err == nil {
		p.close()
		t.Fatal("caller handle mapping accepted")
	}
}
func TestWindowsJobAssignmentFailureKillsAndReaps(t *testing.T) {
	dir, err := privateWorkerDirectory("trove-job-failure-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestNativeWorkerProcessChild$")
	cmd.Dir = dir
	cmd.Env = append(workerEnvironment(dir), "TROVE_NATIVE_TEST_CHILD=forced")
	// Keep stdin open so the child cannot exit before failed job assignment.
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	p, err := prepareWorker(cmd, nil, 512<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	// Only same-package tests can remove assignment rights from the job.
	var readOnly windows.Handle
	if err = windows.DuplicateHandle(windows.CurrentProcess(), p.job, windows.CurrentProcess(), &readOnly, 0x0004 /* JOB_OBJECT_QUERY */, false, 0); err != nil {
		t.Fatal(err)
	}
	windows.CloseHandle(p.job)
	p.job = readOnly
	if err = p.start(); err == nil {
		t.Fatal("uncontained child was admitted")
	}
	if cmd.ProcessState == nil || len(p.inherited) != 0 {
		t.Fatal("partial launch did not reap child and close inherited handles")
	}
}
func TestWindowsNativeDirectoryHasProtectedDACL(t *testing.T) {
	dir, err := privateWorkerDirectory("trove-dacl-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("temporary worker DACL inherits permissions")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		t.Fatalf("missing private DACL: %v", err)
	}
	if acl.AceCount != 1 {
		t.Fatalf("unexpected worker directory grants: %d", acl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err = windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	granted := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !granted.Equals(user.User.Sid) {
		t.Fatal("directory grants access to a different principal")
	}
}
func TestWindowsUnsupportedNativeAdaptersFailBeforeDescriptors(t *testing.T) {
	s := &Supervisor{}
	if _, err := s.Capture(context.Background(), CaptureRequest{}, nil, nil); err == nil {
		t.Fatal("unsupported capture accepted")
	}
	if _, err := s.DecodeAudio(context.Background(), nil, nil); !errors.Is(err, ErrAudioUnsupported) {
		t.Fatalf("decoder availability: %v", err)
	}
	if OCRAvailable(t.TempDir()) {
		t.Fatal("OCR advertised on Windows")
	}
}

func TestWindowsPrivateStagingFileOnlyGrantsCurrentUser(t *testing.T) {
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	sd, err := windows.GetNamedSecurityInfo(file.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatalf("derived file inherited unexpected grants: %v", err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err = windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
		t.Fatal("derived file accessible by another principal")
	}
}
