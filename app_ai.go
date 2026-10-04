package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

type aiFlight struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type aiAdapter struct {
	mu      sync.Mutex
	flights map[string]*aiFlight
}

var aiAdapters sync.Map

func (a *App) aiState() *aiAdapter {
	v, _ := aiAdapters.LoadOrStore(a, &aiAdapter{flights: map[string]*aiFlight{}})
	return v.(*aiAdapter)
}
func (a *App) stopAI() {
	v, ok := aiAdapters.Load(a)
	if !ok {
		return
	}
	s := v.(*aiAdapter)
	s.mu.Lock()
	waiting := []chan struct{}{}
	for _, f := range s.flights {
		f.cancel()
		waiting = append(waiting, f.done)
	}
	s.mu.Unlock()
	for _, done := range waiting {
		<-done
	}
}
func (a *App) aiDispatch(method string, raw json.RawMessage) string {
	var req struct {
		workspace.AIRequest
		WorkspaceID     string `json:"workspaceId"`
		ContractVersion int    `json:"contractVersion"`
		RunID           string `json:"runId"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid AI request")
	}
	a.mu.RLock()
	k, e := a.kernel, a.execution
	a.mu.RUnlock()
	if k == nil || req.WorkspaceID != k.ID() || req.ContractVersion != 1 {
		return errorEnvelope("INVALID_REQUEST", "AI request belongs to another workspace")
	}
	success := func(v any) string { b, _ := json.Marshal(map[string]any{"ok": true, "result": v}); return string(b) }
	s := a.aiState()
	if method == "ai.cancel" {
		s.mu.Lock()
		f := s.flights[req.RunID]
		s.mu.Unlock()
		if f != nil {
			f.cancel()
		}
		return success(map[string]any{"runId": req.RunID, "state": "cancellation_requested"})
	}
	if a.native == nil || a.native.supervisor == nil {
		return errorEnvelope("WORKER_FAILED", "Install and verify an instruction model pack in Models")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.flights) > 0 {
		return errorEnvelope("RESOURCE_EXHAUSTED", "A local answer is already running; cancel it or wait")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	prepared, err := k.PrepareAI(ctx, e, req.AIRequest)
	if err != nil {
		var failure *workspace.ServiceFailure
		if errors.As(err, &failure) {
			return errorEnvelope(failure.Code, failure.Message)
		}
		return errorEnvelope("INTERNAL", err.Error())
	}
	generation, cancel := context.WithTimeout(ctx, 5*time.Minute)
	f := &aiFlight{cancel: cancel, done: make(chan struct{})}
	s.flights[prepared.Run.ID] = f
	go func() {
		defer close(f.done)
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.flights, prepared.Run.ID); s.mu.Unlock() }()
		emit := func(event map[string]any) {
			event["runId"] = prepared.Run.ID
			event["workspaceId"] = k.ID()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "ai:progress", event)
			}
		}
		text := ""
		last := time.Now()
		answer, err := a.native.supervisor.GenerateText(generation, req.ModelPackID, native.ChatPrompt(prepared.System, prepared.Prompt), 512, func(token string) {
			text += token
			emit(map[string]any{"state": "running", "token": token})
			if time.Since(last) > time.Second {
				_ = k.CheckpointAI(generation, e, prepared.Run.ID, text)
				last = time.Now()
			}
		})
		state, failure := "completed", ""
		if err != nil {
			state = "failed"
			failure = err.Error()
			if generation.Err() != nil {
				state = "cancelled"
				failure = "Generation cancelled; partial text is retained."
			}
			answer = text
		}
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer saveCancel()
		run, saveErr := k.FinishAI(saveCtx, e, prepared.Run.ID, answer, state, failure)
		if saveErr != nil {
			emit(map[string]any{"state": "failed", "error": saveErr.Error()})
			return
		}
		emit(map[string]any{"state": run.State, "run": run})
	}()
	return success(prepared.Run)
}
