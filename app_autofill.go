package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

func (a *App) autofillDispatch(method string, raw json.RawMessage) string {
	if err := contracts.Validate("service", "AutofillControlRequest", raw); err != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid autofill request")
	}
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		BatchID     string `json:"batchId"`
	}
	_ = json.Unmarshal(raw, &req)
	a.mu.RLock()
	k, e := a.kernel, a.execution
	a.mu.RUnlock()
	if k == nil || req.WorkspaceID != k.ID() {
		return errorEnvelope("INVALID_REQUEST", "Autofill belongs to another workspace")
	}
	success := func(v any) string {
		out, _ := json.Marshal(map[string]any{"ok": true, "result": v})
		return string(out)
	}
	failure := func(err error) string {
		var f *workspace.ServiceFailure
		if errors.As(err, &f) {
			return errorEnvelope(f.Code, f.Message)
		}
		return errorEnvelope("INTERNAL", err.Error())
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := k.Autofill(ctx, e, req.BatchID)
	if err != nil {
		return failure(err)
	}
	s := a.aiState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if method == "autofill.cancel" {
		if f := s.flights[b.ID]; f != nil {
			f.cancel()
		}
		return success(map[string]any{"batchId": b.ID, "state": "cancellation_requested"})
	}
	if len(s.flights) > 0 {
		return errorEnvelope("RESOURCE_EXHAUSTED", "Another local generation is running; cancel it or wait")
	}
	if a.native == nil || a.native.supervisor == nil {
		return errorEnvelope("WORKER_FAILED", "Install and verify an instruction model in Models")
	}
	contextTokens, err := a.native.supervisor.GenerationContext(b.Input.ModelPackID)
	if err != nil {
		return failure(err)
	}
	b, err = k.StartAutofill(ctx, e, b.ID)
	if err != nil {
		return failure(err)
	}
	generation, cancel := context.WithTimeout(ctx, 10*time.Minute)
	flight := &aiFlight{cancel: cancel, done: make(chan struct{})}
	s.flights[b.ID] = flight
	go func() {
		defer close(flight.done)
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.flights, b.ID); s.mu.Unlock() }()
		_ = k.GenerateAutofill(generation, e, b, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
			text := native.ChatPrompt(system, prompt)
			counts, err := a.native.supervisor.CountTokens(ctx, b.Input.ModelPackID, []string{text})
			if err != nil {
				return "", err
			}
			if len(counts) != 1 || counts[0]+tokens+64 > contextTokens {
				return "", workspace.Fail("RESOURCE_EXHAUSTED", "Selected fields exceed the model context; narrow them in a new batch")
			}
			return a.native.supervisor.GenerateTextWithGrammar(ctx, b.Input.ModelPackID, text, tokens, grammar, stream)
		}, nil)
	}()
	return success(b)
}
