//go:build darwin || linux

package native

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type workerProcess struct{ cmd *exec.Cmd }

func prepareWorker(cmd *exec.Cmd, files []*os.File, memory int64) (*workerProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = files
	return &workerProcess{cmd: cmd}, nil
}
func (p *workerProcess) start() error { return p.cmd.Start() }
func (p *workerProcess) close()       {}
func (p *workerProcess) stop(force bool) error {
	if p.cmd.Process == nil {
		return os.ErrProcessDone
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-p.cmd.Process.Pid, signal)
}
func (p *workerProcess) rss() (int64, error) {
	if p.cmd.Process == nil {
		return 0, os.ErrProcessDone
	}
	return processRSS(p.cmd.Process.Pid)
}
func privateWorkerDirectory(prefix string) (string, error) { return os.MkdirTemp("", prefix) }
func workerEnvironment(dir string) []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "LANG=en_US.UTF-8", "TROVE_GPU_LAYERS=0"}
}
func availableModelBytes(path string) (uint64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, err
	}
	return uint64(s.Bavail) * uint64(s.Bsize), nil
}
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
func syncModelDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
