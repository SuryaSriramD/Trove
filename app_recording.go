package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

type activeRecording struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type appRecordingState struct {
	sync.Mutex
	active map[string]*activeRecording
}

var appRecordings sync.Map

func (a *App) recordingState() *appRecordingState {
	v, _ := appRecordings.LoadOrStore(a, &appRecordingState{active: map[string]*activeRecording{}})
	return v.(*appRecordingState)
}

// StartMeetingRecording is a trusted explicit microphone gesture. It creates a
// durable workspace destination before launching native capture asynchronously.
func (a *App) StartMeetingRecording(title string) (workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return workspace.RecordingDraft{}, err
	}

	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return workspace.RecordingDraft{}, fmt.Errorf("workspace is not open")
	}
	if a.native == nil || a.native.supervisor == nil {
		return workspace.RecordingDraft{}, fmt.Errorf("bundled microphone runtime is unavailable")
	}
	state := a.recordingState()
	state.Lock()
	defer state.Unlock()
	if len(state.active) > 0 {
		return workspace.RecordingDraft{}, fmt.Errorf("stop the current recording before starting another")
	}
	draft, file, err := a.kernel.BeginRecording(a.ctx, a.execution, title)
	if err != nil {
		return draft, err
	}
	k, e, s := a.kernel, a.execution, a.native.supervisor
	ctx, cancel := context.WithCancel(a.ctx)
	active := &activeRecording{cancel: cancel, done: make(chan struct{})}
	state.active[draft.ID] = active
	go func() {
		defer close(active.done)
		defer cancel()
		defer file.Close()
		checkpoint := func(terminal bool, message string) {
			checkpointCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			d, err := k.CheckpointRecording(checkpointCtx, e, draft.ID, terminal, message)
			if err != nil {
				runtime.EventsEmit(a.ctx, "recording:error", map[string]any{"id": draft.ID, "message": err.Error()})
				return
			}
			runtime.EventsEmit(a.ctx, "recording:progress", d)
		}
		_, err := s.Capture(ctx, native.CaptureRequest{RequestID: draft.ID, DurationMS: 7200000, MaxBytes: 16000 * 4 * 7200, RequestPermission: true}, file, func(raw json.RawMessage) { checkpoint(false, "") })
		_ = file.Sync()
		message := ""
		if err != nil {
			message = err.Error()
		}
		checkpoint(true, message)
		state.Lock()
		delete(state.active, draft.ID)
		state.Unlock()
	}()
	return draft, nil
}
func (a *App) StopMeetingRecording(id string) (workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return workspace.RecordingDraft{}, err
	}

	state := a.recordingState()
	state.Lock()
	active := state.active[id]
	state.Unlock()
	if active != nil {
		active.cancel()
		select {
		case <-active.done:
		case <-time.After(10 * time.Second):
			return workspace.RecordingDraft{}, fmt.Errorf("capture is stopping; flushed audio remains recoverable")
		}
	}
	return a.SaveMeetingRecording(id)
}
func (a *App) SaveMeetingRecording(id string) (workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return workspace.RecordingDraft{}, err
	}

	state := a.recordingState()
	state.Lock()
	active := state.active[id]
	state.Unlock()
	if active != nil {
		return workspace.RecordingDraft{}, fmt.Errorf("stop recording before saving the meeting")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return workspace.RecordingDraft{}, fmt.Errorf("workspace is not open")
	}
	return a.kernel.FinalizeRecording(a.ctx, a.execution, id)
}
func (a *App) MeetingRecordings() ([]workspace.RecordingDraft, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return nil, fmt.Errorf("workspace is not open")
	}
	drafts, err := a.kernel.ListRecordings(a.ctx, a.execution)
	if err != nil {
		return nil, err
	}
	state := a.recordingState()
	state.Lock()
	defer state.Unlock()
	for i := range drafts {
		if drafts[i].State == "recording" && state.active[drafts[i].ID] == nil {
			drafts[i].State = "interrupted"
		}
	}
	return drafts, nil
}
func (a *App) PickMeetingAudio(title string) (workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return workspace.RecordingDraft{}, err
	}

	// Wails maps filters through UTType; wildcard/unknown raw PCM extensions
	// are not portable UTTypes. Validate the selected bytes in the audio service.
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a local recording (WAV, MP3, AAC or M4A)"})
	if err != nil {
		return workspace.RecordingDraft{}, err
	}
	if path == "" {
		return workspace.RecordingDraft{}, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return workspace.RecordingDraft{}, fmt.Errorf("workspace is not open")
	}
	handle, err := a.kernel.RegisterSelection(path)
	if err != nil {
		return workspace.RecordingDraft{}, err
	}
	return a.kernel.CreateMeetingFromSelection(a.ctx, a.execution, handle, title)
}

// Called before closing/switching a workspace. A stopped app keeps its flushed
// capture and journal for recovery instead of deleting an unfinished recording.
func (a *App) stopRecordings() {
	state := a.recordingState()
	state.Lock()
	active := make([]*activeRecording, 0, len(state.active))
	for _, r := range state.active {
		r.cancel()
		active = append(active, r)
	}
	state.Unlock()
	for _, r := range active {
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
		}
	}
}
