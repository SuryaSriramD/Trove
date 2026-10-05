//go:build darwin

package updates

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// RollbackChild owns inherited, private handshake descriptors. It never exposes
// the nonce or descriptor numbers to the renderer, logs, or the workspace.
type RollbackChild struct {
	request       rollbackMessage
	input, output *os.File
	ctx           context.Context
	cancel        context.CancelFunc
	failed        chan struct{}
	commit        chan rollbackMessage
	mu            sync.Mutex
	once          sync.Once
	ready         atomic.Bool
	accepted      atomic.Bool
}

func ReceiveRollbackChild() (*RollbackChild, error) {
	value := os.Getenv(rollbackEnvironmentKey)
	if value == "" {
		return nil, nil
	}
	_ = os.Unsetenv(rollbackEnvironmentKey)
	if value != strconv.Itoa(RollbackHandshakeVersion) {
		return nil, errors.New("unsupported rollback startup channel")
	}
	// Do this before Wails, model, provider or WebView subprocess creation.
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	if syscall.SetNonblock(3, true) != nil || syscall.SetNonblock(4, true) != nil {
		return nil, errors.New("rollback descriptors are not pollable")
	}
	input, output := os.NewFile(3, "rollback-request"), os.NewFile(4, "rollback-reply")
	fail := func() (*RollbackChild, error) {
		if input != nil {
			input.Close()
		}
		if output != nil {
			output.Close()
		}
		return nil, errors.New("invalid rollback startup channel")
	}
	if input == nil || output == nil {
		return fail()
	}
	for _, f := range []*os.File{input, output} {
		st, e := f.Stat()
		if e != nil || st.Mode()&os.ModeNamedPipe == 0 {
			return fail()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), rollbackReadyTimeout+rollbackCommitTimeout+5*time.Second)
	deadline, _ := ctx.Deadline()
	if input.SetReadDeadline(deadline) != nil || output.SetWriteDeadline(deadline) != nil {
		cancel()
		return fail()
	}
	child := &RollbackChild{input: input, output: output, ctx: ctx, cancel: cancel, failed: make(chan struct{}), commit: make(chan rollbackMessage, 1)}
	received := make(chan struct {
		m rollbackMessage
		e error
	}, 1)
	go func() {
		m, e := readRollbackMessage(input)
		received <- struct {
			m rollbackMessage
			e error
		}{m, e}
	}()
	select {
	case r := <-received:
		if r.e != nil || r.m.Kind != "request" || r.m.PID != os.Getppid() {
			cancel()
			return fail()
		}
		child.request = r.m
	case <-ctx.Done():
		cancel()
		return fail()
	}
	go func() {
		select {
		case <-ctx.Done():
			if !child.accepted.Load() {
				child.Abort()
			}
		case <-child.failed:
		}
	}()
	go func() {
		m, e := readRollbackMessage(input)
		if e != nil || !child.ready.Load() || !matchesRollbackMessage(m, "commit", child.request.Nonce, child.request.PID, child.request.RollbackTarget) {
			child.Abort()
			return
		}
		child.commit <- m
	}()
	return child, nil
}
func (c *RollbackChild) Target() RollbackTarget  { return c.request.RollbackTarget }
func (c *RollbackChild) Failed() <-chan struct{} { return c.failed }
func (c *RollbackChild) Abort() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accepted.Load() {
		return
	}
	c.once.Do(func() { c.input.Close(); c.output.Close(); c.cancel(); close(c.failed) })
}
func (c *RollbackChild) ConfirmReady(actual RollbackTarget, initialize func() error) error {
	if actual != c.request.RollbackTarget || !validRollbackTarget(actual) {
		c.Abort()
		return errors.New("opened rollback workspace differs from reviewed destination")
	}
	if !c.ready.CompareAndSwap(false, true) {
		return errors.New("rollback readiness already sent")
	}
	reply := c.request
	reply.PID = os.Getpid()
	reply.Kind = "ready"
	if err := writeRollbackMessage(c.output, reply); err != nil {
		c.Abort()
		return errors.New("rollback readiness channel failed")
	}
	select {
	case <-c.commit:
	case <-c.failed:
		return errors.New("rollback parent disconnected")
	case <-c.ctx.Done():
		c.Abort()
		return errors.New("rollback parent did not commit")
	}
	if initialize == nil {
		c.Abort()
		return errors.New("rollback runtime initialization is unavailable")
	}
	if err := initialize(); err != nil {
		c.Abort()
		return errors.New("rollback runtime initialization failed; restore a fresh copy before retrying")
	}
	c.mu.Lock()
	if c.ctx.Err() != nil {
		c.mu.Unlock()
		c.Abort()
		return errors.New("rollback startup deadline exceeded")
	}
	reply.Kind = "committed"
	if err := writeRollbackMessage(c.output, reply); err != nil {
		c.mu.Unlock()
		c.Abort()
		return errors.New("rollback acceptance channel failed")
	}
	c.accepted.Store(true)
	c.cancel()
	c.input.Close()
	c.output.Close()
	c.mu.Unlock()
	return nil
}
