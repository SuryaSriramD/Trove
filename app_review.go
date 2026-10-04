package main

import (
	"context"
	"encoding/json"
	"time"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

// One local-model flight is shared by interactive AI and review analysis.
// This loop is separate from schedule/automation maintenance; a slow model
// cannot starve either scheduler and shutdown cancels the native process.
func (a *App) runPersonalReviewGenerations(ctx context.Context, k *workspace.Kernel) {
	e := k.OwnerContext()
	defer k.RevokeGrant(string(e.ScopeGrantId))
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		r, err := k.NextPersonalReviewGeneration(ctx, e)
		if err != nil || r.ID == "" {
			continue
		}
		s := a.aiState()
		s.mu.Lock()
		if len(s.flights) > 0 {
			s.mu.Unlock()
			continue
		}
		generation, cancel := context.WithTimeout(ctx, 5*time.Minute)
		flight := &aiFlight{cancel: cancel, done: make(chan struct{})}
		key := "review:" + r.ID
		s.flights[key] = flight
		s.mu.Unlock()
		func() {
			defer close(flight.done)
			defer cancel()
			defer func() { s.mu.Lock(); delete(s.flights, key); s.mu.Unlock() }()
			if a.native == nil || a.native.supervisor == nil {
				_ = k.FailPersonalReviewGeneration(ctx, e, r.ID, workspace.Fail("WORKER_FAILED", "Install and verify the selected local instruction model in Models"))
				return
			}
			supervisor := a.native.supervisor
			size, err := supervisor.GenerationContext(r.Generative.ModelPackID)
			if err != nil {
				_ = k.FailPersonalReviewGeneration(ctx, e, r.ID, err)
				return
			}
			_, _ = k.GeneratePersonalReview(generation, e, r.ID, workspace.AIBudget{ContextTokens: size, OutputTokens: min(2048, size/4), CountTokens: func(ctx context.Context, prompts []workspace.AIPrompt) ([]int, error) {
				texts := make([]string, len(prompts))
				for i, p := range prompts {
					texts[i] = native.ChatPrompt(p.System, p.User)
				}
				return supervisor.CountTokens(ctx, r.Generative.ModelPackID, texts)
			}}, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
				return supervisor.GenerateTextWithGrammar(ctx, r.Generative.ModelPackID, native.ChatPrompt(system, prompt), tokens, grammar, stream)
			})
		}()
	}
}
func (a *App) reviewCancel(raw json.RawMessage) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return errorEnvelope("WORKSPACE_LOCKED", "Workspace is not open")
	}
	result := a.kernel.Dispatch(a.ctx, a.execution, "review.cancel", raw)
	var envelope struct {
		OK     bool                        `json:"ok"`
		Result workspace.PersonalReviewRun `json:"result"`
	}
	if json.Unmarshal(result, &envelope) == nil && envelope.OK {
		state := a.aiState()
		state.mu.Lock()
		if flight := state.flights["review:"+envelope.Result.ID]; flight != nil {
			flight.cancel()
		}
		state.mu.Unlock()
	}
	return string(result)
}
