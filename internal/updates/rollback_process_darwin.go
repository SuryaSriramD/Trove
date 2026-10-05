//go:build darwin

package updates

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const RollbackHandshakeVersion = 1
const rollbackEnvironmentKey = "TROVE_ROLLBACK_HANDSHAKE"
const rollbackReadyTimeout = 45 * time.Second
const rollbackCommitTimeout = 15 * time.Second

// ErrRollbackCleanupUnverified prohibits restoring the startup preference while
// a launched application might still own the reviewed workspace.
var ErrRollbackCleanupUnverified = errors.New("rollback child termination could not be verified")

func launchRollbackProcess(ctx context.Context, cmd *exec.Cmd, target RollbackTarget) (resultErr error) {
	if !validRollbackTarget(target) {
		return errors.New("invalid reviewed rollback destination")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	nonce, err := newRollbackNonce()
	if err != nil {
		return err
	}
	inputR, inputW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer inputR.Close()
	defer inputW.Close()
	outputR, outputW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer outputR.Close()
	defer outputW.Close()
	for _, pair := range cmd.Env {
		if strings.HasPrefix(pair, rollbackEnvironmentKey+"=") {
			return errors.New("rollback channel override refused")
		}
	}
	cmd.Env = append(cmd.Env, rollbackEnvironmentKey+"="+strconv.Itoa(RollbackHandshakeVersion))
	cmd.ExtraFiles = []*os.File{inputR, outputW}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return errors.New("previous application could not be started; current app remains open")
	}
	inputR.Close()
	outputW.Close()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	accepted := false
	defer func() {
		if accepted {
			return
		}
		// Before accepted readiness/commit, the child has no jobs or mutation access.
		// Kill by retained process object, and wait for its actual Wait completion.
		select {
		case <-exited:
			return
		default:
		}
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			resultErr = errors.Join(resultErr, ErrRollbackCleanupUnverified)
		}
	}()
	readyCtx, cancel := context.WithTimeout(ctx, rollbackReadyTimeout)
	defer cancel()
	deadline, _ := readyCtx.Deadline()
	if err = inputW.SetWriteDeadline(deadline); err != nil {
		return err
	}
	request := rollbackMessage{Version: rollbackProtocolVersion, Kind: "request", Nonce: nonce, PID: os.Getpid(), RollbackTarget: target}
	if err = writeRollbackMessage(inputW, request); err != nil {
		return errors.New("rollback startup channel failed")
	}
	receive := func(wait context.Context) (rollbackMessage, error) {
		type result struct {
			message rollbackMessage
			err     error
		}
		received := make(chan result, 1)
		go func() { m, e := readRollbackMessage(outputR); received <- result{m, e} }()
		select {
		case r := <-received:
			return r.message, r.err
		case <-exited:
			return rollbackMessage{}, errors.New("previous application exited during startup")
		case <-wait.Done():
			outputR.Close()
			return rollbackMessage{}, wait.Err()
		}
	}
	ready, err := receive(readyCtx)
	if err != nil {
		return errors.New("previous application did not confirm workspace readiness before cancellation or deadline")
	}
	if !matchesRollbackMessage(ready, "ready", nonce, cmd.Process.Pid, target) {
		return errors.New("previous application returned mismatched readiness")
	}
	if err = readyCtx.Err(); err != nil {
		return err
	}
	select {
	case <-exited:
		return errors.New("previous application exited before handoff")
	default:
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return errors.New("previous application exited before handoff")
	}
	request.Kind = "commit"
	if err = writeRollbackMessage(inputW, request); err != nil {
		return errors.New("rollback commit channel failed")
	}
	// Once commit is sent, late cancellation cannot abandon an accepted child.
	// Require its authenticated acceptance within a separate short deadline.
	commitCtx, commitCancel := context.WithTimeout(context.Background(), rollbackCommitTimeout)
	defer commitCancel()
	committed, err := receive(commitCtx)
	if err != nil || !matchesRollbackMessage(committed, "committed", nonce, cmd.Process.Pid, target) {
		return errors.New("previous application did not acknowledge initialized handoff; restore a fresh copy before retrying")
	}
	select {
	case <-exited:
		return errors.New("previous application exited at handoff")
	default:
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return errors.New("previous application exited at handoff")
	}
	accepted = true
	return nil
}
