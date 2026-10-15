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
	once   sync.Once
}
type aiAdapter struct {
	mu        sync.Mutex
	flights   map[string]*aiFlight
	cancelled map[string]time.Time
}

var aiAdapters sync.Map

func (a *App) aiState() *aiAdapter {
	v, _ := aiAdapters.LoadOrStore(a, &aiAdapter{flights: map[string]*aiFlight{}})
	return v.(*aiAdapter)
}

// Admission is short: preparation and generation share a cancellable flight,
// without holding the flight mutex across network or model work.
func (s *aiAdapter) begin(ctx context.Context, id string) (context.Context, *aiFlight, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneCancellations()
	if _, cancelled := s.cancelled[id]; cancelled {
		delete(s.cancelled, id)
		return nil, nil, context.Canceled
	}
	if len(s.flights) > 0 {
		return nil, nil, workspace.Fail("RESOURCE_EXHAUSTED", "A local answer is already running; cancel it or wait")
	}
	flow, cancel := context.WithCancel(ctx)
	f := &aiFlight{cancel: cancel, done: make(chan struct{})}
	s.flights[id] = f
	return flow, f, nil
}
func (s *aiAdapter) pruneCancellations() {
	for id, expires := range s.cancelled {
		if !time.Now().Before(expires) {
			delete(s.cancelled, id)
		}
	}
}
func (s *aiAdapter) cancelRequest(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.flights[id]; f != nil {
		f.cancel()
		return nil
	}
	// A renderer can cancel while its start call is still being admitted.
	// Keep only short-lived bounded request identities, never prompt content.
	s.pruneCancellations()
	if len(s.cancelled) >= 128 {
		return workspace.Fail("RESOURCE_EXHAUSTED", "Too many pending cancellations; retry after current requests settle")
	}
	if s.cancelled == nil {
		s.cancelled = map[string]time.Time{}
	}
	s.cancelled[id] = time.Now().Add(time.Minute)
	return nil
}
func (s *aiAdapter) finish(f *aiFlight) {
	f.once.Do(func() {
		f.cancel()
		s.mu.Lock()
		for id, current := range s.flights {
			if current == f {
				delete(s.flights, id)
			}
		}
		s.mu.Unlock()
		close(f.done)
	})
}
func (a *App) stopAI() {
	v, ok := aiAdapters.Load(a)
	if !ok {
		return
	}
	s := v.(*aiAdapter)
	s.mu.Lock()
	s.cancelled = nil
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
		RequestID       string `json:"requestId"`
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
		if req.RunID == "" || len(req.RunID) > 128 {
			return errorEnvelope("INVALID_REQUEST", "Choose the exact preparation or generation request")
		}
		if err := s.cancelRequest(req.RunID); err != nil {
			return errorEnvelope("RESOURCE_EXHAUSTED", err.Error())
		}
		return success(map[string]any{"runId": req.RunID, "state": "cancellation_requested"})
	}
	if a.native == nil || a.native.supervisor == nil {
		return errorEnvelope("WORKER_FAILED", "Install and verify an instruction model pack in Models")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if req.RequestID == "" {
		req.RequestID = workspace.NewID()
	}
	flow, f, err := s.begin(ctx, req.RequestID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errorEnvelope("CANCELLED", "Source preparation was cancelled before admission")
		}
		return errorEnvelope("RESOURCE_EXHAUSTED", err.Error())
	}
	handedOff := false
	defer func() {
		if !handedOff {
			s.finish(f)
		}
	}()
	contextTokens, err := a.native.supervisor.GenerationContext(req.ModelPackID)
	if err != nil {
		return errorEnvelope("WORKER_FAILED", err.Error())
	}
	prepareCtx, prepareCancel := context.WithTimeout(flow, 30*time.Second)
	defer prepareCancel()
	prepared, err := k.PrepareAIWithBudget(prepareCtx, e, req.AIRequest, workspace.AIBudget{
		ContextTokens: contextTokens, OutputTokens: min(2048, contextTokens/4),
		CountTokens: func(ctx context.Context, prompts []workspace.AIPrompt) ([]int, error) {
			texts := make([]string, len(prompts))
			for i, prompt := range prompts {
				texts[i] = native.ChatPrompt(prompt.System, prompt.User)
			}
			return a.native.supervisor.CountTokens(ctx, req.ModelPackID, texts)
		},
	})
	if err != nil {
		if prepareCtx.Err() != nil {
			return errorEnvelope("CANCELLED", "Source preparation was cancelled or exceeded its time limit")
		}
		var failure *workspace.ServiceFailure
		if errors.As(err, &failure) {
			return errorEnvelope(failure.Code, failure.Message)
		}
		return errorEnvelope("INTERNAL", err.Error())
	}
	generation, cancel := context.WithTimeout(flow, 5*time.Minute)
	s.mu.Lock()
	s.flights[prepared.Run.ID] = f
	s.mu.Unlock()
	handedOff = true
	go func() {
		defer s.finish(f)
		defer cancel()
		emit := func(event map[string]any) {
			event["runId"] = prepared.Run.ID
			event["workspaceId"] = k.ID()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "ai:progress", event)
			}
		}
		text := ""
		last := time.Now()
		completion, err := workspace.GeneratePreparedAI(generation, prepared, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
			return a.native.supervisor.GenerateTextWithGrammar(ctx, req.ModelPackID, native.ChatPrompt(system, prompt), tokens, grammar, stream)
		}, func(token string) {
			text += token
			emit(map[string]any{"state": "running", "token": token})
			if time.Since(last) > time.Second {
				_ = k.CheckpointAI(generation, e, prepared.Run.ID, text)
				last = time.Now()
			}
		})
		answer := completion.Text
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
