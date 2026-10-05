package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

var errDocumentCleanupUnverified = errors.New("document process cleanup is unverified")

// RunDocumentProcess starts only an already verified private executable. The
// fixed single-byte gate precedes all document/profile reads and child launch.
func runDocumentProcess(ctx context.Context, executable, mode, root, private string, stdout io.Writer) error {
	return runDocumentProcessWithMonitor(ctx, executable, mode, root, private, stdout, documentProcessRSS)
}
func runDocumentProcessWithMonitor(ctx context.Context, executable, mode, root, private string, stdout io.Writer, memory func(*workerProcess) (int64, error)) (resultErr error) {
	cmd := exec.Command(executable, mode, root)
	cmd.Dir = private
	cmd.Env = documentEnvironment(private)
	cmd.Stdout = stdout
	var diagnostic ringLog
	cmd.Stderr = &diagnostic
	gate, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer gate.Close()
	process, err := prepareDocumentProcess(cmd)
	if err != nil {
		return err
	}
	defer process.close()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = process.start(); err != nil {
		return errors.New("cannot start document helper")
	}
	defer func() {
		if err := drainDocumentProcess(process); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: %v", errDocumentCleanupUnverified, err))
		}
	}()
	if _, err = gate.Write([]byte{1}); err != nil {
		process.stop(true)
		cmd.Wait()
		return errors.New("cannot release document helper start gate")
	}
	gate.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err = <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return errors.New("local document processing failed or required WebView capability is unavailable")
			}
			return nil
		case <-ctx.Done():
			process.stop(true)
			<-done
			return ctx.Err()
		case <-tick.C:
			if rss, e := memory(process); e != nil || rss > 2<<30 {
				process.stop(true)
				<-done
				return errors.New("document process exceeded the aggregate 2 GiB memory bound or its usage could not be verified")
			}
		}
	}
}
func newDocumentDirectory() (string, error)           { return privateWorkerDirectory("trove-document-") }
func documentBaseEnvironment(private string) []string { return workerEnvironment(private) }
