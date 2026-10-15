package native

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type AudioInput struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// AudioInputs enumerates device identifiers without opening capture or requesting
// microphone permission. Selection is rechecked by the helper at capture start.
func (s *Supervisor) AudioInputs(ctx context.Context) ([]AudioInput, error) {
	if s == nil {
		return nil, errors.New("microphone helper unavailable")
	}
	h, ok := s.helpers["microphone-worker"]
	if !ok || h.Architecture != runtime.GOARCH || (h.RuntimeCommit != MicrophoneCommit && h.RuntimeCommit != MicrophoneCommit+"-fixture") {
		return nil, errors.New("input selection requires the current microphone helper")
	}
	if err := s.verifyHelper(h); err != nil {
		return nil, err
	}
	run, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	run, release, err := s.Scheduler.Enter(run, PriorityRecording)
	if err != nil {
		return nil, err
	}
	defer release()
	dir, err := privateWorkerDirectory("trove-inputs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	controlR, controlW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer controlR.Close()
	defer controlW.Close()
	unused, err := os.CreateTemp(dir, "unused-")
	if err != nil {
		return nil, err
	}
	defer unused.Close()
	cmd := exec.Command(filepath.Join(s.bundle, helperFilename(h.Name)))
	cmd.Dir = dir
	cmd.Env = workerEnvironment(dir)
	cmd.Stderr = &ringLog{}
	process, err := prepareWorker(cmd, []*os.File{controlR, unused}, 512<<20)
	if err != nil {
		return nil, err
	}
	defer process.close()
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer out.Close()
	if err = process.start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-run.Done():
			_ = process.stop(true)
		}
	}()
	defer func() { _ = process.stop(true); _ = cmd.Wait() }()
	raw, err := ReadFrame(out)
	if err != nil {
		return nil, err
	}
	var hello Event
	if json.Unmarshal(raw, &hello) != nil || hello.Kind != "hello" || hello.RuntimeCommit != h.RuntimeCommit || hello.Runtime != "microphone" || hello.Architecture != runtime.GOARCH || hello.ProtocolVersion != 1 {
		return nil, errors.New("microphone hello mismatch")
	}
	capable := false
	for _, capability := range hello.Capabilities {
		if capability == "input_devices_v1" {
			capable = true
		}
	}
	if !capable {
		return nil, errors.New("input enumeration unavailable")
	}
	id := NewID()
	if err = WriteFrame(in, map[string]any{"protocolVersion": 1, "kind": "inputs", "requestId": id}); err != nil {
		return nil, err
	}
	in.Close()
	raw, err = ReadFrame(out)
	if err != nil {
		return nil, err
	}
	var result struct {
		Kind      string       `json:"kind"`
		RequestID string       `json:"requestId"`
		Sequence  int          `json:"sequence"`
		Inputs    []AudioInput `json:"inputs"`
		Code      string       `json:"code"`
	}
	if json.Unmarshal(raw, &result) != nil || result.RequestID != id || result.Sequence != 0 {
		return nil, errors.New("invalid audio input response")
	}
	if result.Kind == "error" {
		return nil, errors.New("microphone: " + result.Code)
	}
	if result.Kind != "inputs" || len(result.Inputs) > 256 {
		return nil, errors.New("invalid audio input list")
	}
	seen := map[string]bool{}
	for _, input := range result.Inputs {
		if input.ID == "" || len(input.ID) > 1024 || input.Name == "" || len(input.Name) > 1024 || seen[input.ID] {
			return nil, errors.New("invalid audio input")
		}
		seen[input.ID] = true
	}
	if err = cmd.Wait(); err != nil {
		return nil, err
	}
	return result.Inputs, nil
}
