package main

import (
	"context"
	"encoding/json"
	"os"
	"time"
	"trove.local/phase0/internal/workspace"
)

func (a *App) imageDispatch(method string, raw json.RawMessage) string {
	a.mu.RLock()
	k, e, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil {
		return errorEnvelope("WORKSPACE_LOCKED", "Workspace is not open")
	}
	s := a.aiState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if method == "image.start" {
		if a.native == nil || !a.native.supervisor.VisionAvailable() {
			return errorEnvelope("WORKER_FAILED", "Install the native image helper and a verified vision pair in Models")
		}
		if len(s.flights) > 0 {
			return errorEnvelope("RESOURCE_EXHAUSTED", "A local model is already running; cancel it or wait")
		}
	}
	reply := k.Dispatch(ctx, e, method, raw)
	var envelope struct {
		OK     bool                       `json:"ok"`
		Result workspace.ImageDescription `json:"result"`
	}
	if json.Unmarshal(reply, &envelope) != nil || !envelope.OK {
		return string(reply)
	}
	r := envelope.Result
	key := "image:" + r.ID
	if method == "image.cancel" {
		if f := s.flights[key]; f != nil {
			f.cancel()
		}
		return string(reply)
	}
	generation, cancel := context.WithTimeout(ctx, 5*time.Minute)
	f := &aiFlight{cancel: cancel, done: make(chan struct{})}
	s.flights[key] = f
	go func() {
		defer close(f.done)
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.flights, key); s.mu.Unlock() }()
		_, _ = k.GenerateImageDescription(generation, e, r.ID, func(ctx context.Context, pack, question string, image *os.File) (workspace.ImageDescriptionResult, error) {
			v, err := a.native.supervisor.GenerateImage(ctx, pack, question, image, nil)
			return workspace.ImageDescriptionResult{Text: v.Text, PeakRSSBytes: v.PeakRSSBytes, AnalysisMethod: v.AnalysisMethod, ModelHash: v.ModelHash, ProjectorHash: v.ProjectorHash, HelperHash: v.HelperHash, FinishReason: v.FinishReason, InputTokens: v.InputTokens, OutputTokens: v.OutputTokens}, err
		})
	}()
	return string(reply)
}
