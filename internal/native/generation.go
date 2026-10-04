package native

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"trove.local/phase0/contracts"
)

// ChatPrompt formats the pinned Qwen instruction pack. Untrusted source text
// cannot introduce special-token message boundaries into its user message.
func ChatPrompt(system, user string) string {
	clean := func(s string) string { return strings.ReplaceAll(s, "<|", "‹|") }
	return "<|im_start|>system\n" + clean(system) + "<|im_end|>\n<|im_start|>user\n" + clean(user) + "<|im_end|>\n<|im_start|>assistant\n"
}

// GenerateText runs a single cancellable local inference. The caller owns
// retrieval, evidence validation and approval; generated text is never a write.
func (s *Supervisor) GenerateText(ctx context.Context, packID, prompt string, maxTokens int, onToken func(string)) (string, error) {
	return s.GenerateTextWithGrammar(ctx, packID, prompt, maxTokens, "", onToken)
}

// GenerateTextWithGrammar constrains structured output with a trusted GBNF
// grammar while preserving the same local model, budget, cancellation, and
// streaming boundaries. Callers still validate evidence and reviewed writes.
func (s *Supervisor) GenerateTextWithGrammar(ctx context.Context, packID, prompt string, maxTokens int, grammar string, onToken func(string)) (string, error) {
	if len(grammar) > 64<<10 {
		return "", errors.New("INVALID_INPUT: generation grammar exceeds 64 KiB")
	}

	if s == nil || s.manager == nil {
		return "", errors.New("MODEL_NOT_INSTALLED: local runtime is unavailable")
	}
	packs, err := s.manager.List()
	if err != nil {
		return "", err
	}
	var pack *Pack
	for _, status := range packs {
		if status.Pack.ID == packID && status.State == "ready" {
			p := status.Pack
			pack = &p
			break
		}
	}
	if pack == nil {
		return "", errors.New("MODEL_NOT_INSTALLED: install a verified instruction pack in Settings")
	}
	if pack.Kind != "chat" || pack.QualificationOnly {
		return "", errors.New("MODEL_INCOMPATIBLE: choose an instruction pack, not a runtime qualification pack")
	}
	if maxTokens < 1 || maxTokens > 2048 {
		return "", errors.New("INVALID_INPUT: answer token limit must be 1–2048")
	}
	contextTokens := min(pack.ContextTokens, 8192)
	// UTF-8 byte count is a conservative token upper bound. Refuse rather than
	// silently truncating user requests or evidence. Callers can select less context.
	if prompt == "" || len(prompt)+maxTokens+16 > contextTokens {
		return "", errors.New("RESOURCE_EXHAUSTED: prompt exceeds the local context budget; reduce the selection or sources")
	}
	f, err := os.CreateTemp("", "trove-generation-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(InputMetadata{SchemaVersion: 1, Text: prompt, Grammar: grammar, ContextTokens: contextTokens, GPULayers: -1}); err != nil {
		return "", err
	}
	input, err := OpenInput(f, "text", NewID())
	if err != nil {
		return "", err
	}
	wire := contracts.JobsWorkerRequest{
		ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()),
		LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()),
		Operation: "generate", ModelPackId: contracts.DomainId(packID),
		Limits: contracts.JobsResourceLimits{Threads: 4, MemoryBytes: s.MemoryBudget(), TimeoutMs: 180000, MaxOutputBytes: 4 << 20, MaxTokens: int64(maxTokens)},
	}
	raw, err := s.Run(ctx, Request{Wire: wire, Inputs: []Input{input}, Priority: PriorityChat}, func(event Event) {
		if event.Kind == "token" && onToken != nil {
			onToken(event.Text)
		}
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Kind != "generation" || strings.TrimSpace(result.Text) == "" {
		return "", errors.New("WORKER_FAILED: model returned no answer")
	}
	return result.Text, nil
}
