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
	cancel     context.CancelFunc
	done       chan struct{}
	controller *native.CaptureController
	controls   sync.Mutex
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

	return workspace.RecordingDraft{}, fmt.Errorf("Acknowledge participant disclosure before recording")
}
func (a *App) StartMeetingRecordingWithOptions(title, inputDeviceID string, disclosureAcknowledged bool) (workspace.RecordingDraft, error) {
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
	draft, file, err := a.kernel.BeginDisclosedRecording(a.ctx, a.execution, title, inputDeviceID, disclosureAcknowledged)
	if err != nil {
		return draft, err
	}
	k, e, s := a.kernel, a.execution, a.native.supervisor
	ctx, cancel := context.WithCancel(a.ctx)
	active := &activeRecording{cancel: cancel, done: make(chan struct{}), controller: native.NewCaptureController()}
	state.active[draft.ID] = active
	go func() {
		defer close(active.done)
		defer cancel()
		defer file.Close()
		var journalError error
		var lastCheckpoint time.Time
		checkpoint := func(terminal bool, message string) {
			checkpointCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			d, err := k.CheckpointRecording(checkpointCtx, e, draft.ID, terminal, message)
			if err != nil {
				journalError = err
				cancel()
				runtime.EventsEmit(a.ctx, "recording:error", map[string]any{"id": draft.ID, "message": err.Error()})
				return
			}
			lastCheckpoint = time.Now()
			runtime.EventsEmit(a.ctx, "recording:progress", d)
		}
		_, err := s.Capture(ctx, native.CaptureRequest{RequestID: draft.ID, DurationMS: 7200000, MaxBytes: 16000 * 4 * 7200, RequestPermission: true, InputDeviceID: inputDeviceID, Controller: active.controller}, file, func(raw json.RawMessage) {
			var event struct {
				Kind string `json:"kind"`
				native.CaptureControlAck
			}
			_ = json.Unmarshal(raw, &event)
			// Keep the two-hour journal bounded. Native writes are already fsynced;
			// recovery retains the last unjournaled tail after a process failure.
			if event.Kind != "progress" || time.Since(lastCheckpoint) >= time.Second {
				checkpoint(false, "")
			}
			if event.Kind == "control_ack" && journalError == nil {
				d, err := k.AcknowledgeRecordingControl(context.Background(), e, draft.ID, event.ControlID, event.Action, event.CapturedSamples)
				if err != nil {
					journalError = err
					cancel()
				} else {
					runtime.EventsEmit(a.ctx, "recording:progress", d)
				}
			}
		})
		_ = file.Sync()
		message := ""
		if err != nil {
			message = err.Error()
		}
		if journalError != nil {
			message = "Recording journal could not be saved; audio remains recoverable: " + journalError.Error()
		}
		if ctx.Err() != nil && message == "" {
			message = "Capture interrupted; flushed audio remains recoverable"
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
		active.controls.Lock()
		controlCtx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		_, controlErr := active.controller.Command(controlCtx, "stop")
		cancel()
		active.controls.Unlock()
		if controlErr != nil {
			active.cancel()
		}
		select {
		case <-active.done:
		case <-time.After(10 * time.Second):
			return workspace.RecordingDraft{}, fmt.Errorf("capture is stopping; flushed audio remains recoverable")
		}
		if controlErr != nil {
			return workspace.RecordingDraft{}, fmt.Errorf("stop was not acknowledged; flushed audio is retained for recovery: %w", controlErr)
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
		if (drafts[i].State == "recording" || drafts[i].State == "paused") && state.active[drafts[i].ID] == nil {
			d, recoveryErr := a.kernel.CheckpointRecording(a.ctx, a.execution, drafts[i].ID, true, "Capture process exited; gap duration is unknown")
			if recoveryErr != nil {
				return nil, recoveryErr
			}
			drafts[i] = d
		}
	}
	return drafts, nil
}

func (a *App) MeetingRecordingDetails(meetingID string) (*workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return nil, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return nil, fmt.Errorf("workspace is not open")
	}
	return a.kernel.MeetingRecordingDetails(a.ctx, a.execution, meetingID)
}

// Native device enumeration does not request microphone consent or open capture.
func (a *App) MeetingAudioInputs() ([]native.AudioInput, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return nil, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil || a.native == nil || a.native.supervisor == nil {
		return nil, fmt.Errorf("bundled microphone runtime is unavailable")
	}
	return a.native.supervisor.AudioInputs(a.ctx)
}
func (a *App) ControlMeetingRecording(id, action string) (workspace.RecordingDraft, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return workspace.RecordingDraft{}, err
	}
	if action != "pause" && action != "resume" {
		return workspace.RecordingDraft{}, fmt.Errorf("invalid recording control")
	}
	state := a.recordingState()
	state.Lock()
	active := state.active[id]
	state.Unlock()
	if active == nil {
		return workspace.RecordingDraft{}, fmt.Errorf("recording is not active")
	}
	active.controls.Lock()
	defer active.controls.Unlock()
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	ack, err := active.controller.Command(ctx, action)
	if err != nil {
		return workspace.RecordingDraft{}, err
	}
	drafts, err := a.MeetingRecordings()
	if err != nil {
		return workspace.RecordingDraft{}, err
	}
	for _, draft := range drafts {
		if draft.ID == id && draft.LastControlID == ack.ControlID {
			return draft, nil
		}
	}
	return workspace.RecordingDraft{}, fmt.Errorf("capture acknowledged but journal is unavailable; audio remains recoverable")
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
