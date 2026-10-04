package native

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealTokenizerLongInputAndIncompleteOutput(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires verified native helpers and supported instruction packs")
	}
	m, err := NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	pack := "01990000-0000-7000-8000-000000000105"
	prompt := ChatPrompt("Use only the supplied text. Reply with the access code, nothing else.", strings.Repeat("An irrelevant sample sentence.\n", 600)+"\nThe access code is COBALT-83. What is the access code?")
	started := time.Now()
	counts, err := s.CountTokens(context.Background(), pack, []string{prompt, ChatPrompt("Summarize.", "日本語の文章です。これは正確なトークン数の検査です。")})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) <= 8192 || len(counts) != 2 || counts[0]+64 > 8192 || counts[0] >= len(prompt)/2 || counts[1] < 1 {
		t.Fatal("model vocabulary was not used", len(prompt), counts)
	}
	t.Logf("prompt bytes=%d tokens=%d tokenizer duration=%s", len(prompt), counts[0], time.Since(started))
	answer, err := s.GenerateText(context.Background(), pack, prompt, 64, nil)
	if err != nil || !strings.Contains(strings.ToUpper(answer), "COBALT-83") {
		t.Fatalf("long-input answer: %q; %v", answer, err)
	}
	var partial strings.Builder
	_, err = s.GenerateText(context.Background(), pack, ChatPrompt("Follow the request.", "List every integer from 1 to 1000, with one number on each line."), 4, func(token string) { partial.WriteString(token) })
	if err == nil || !strings.Contains(err.Error(), "OUTPUT_LIMIT") || partial.Len() == 0 {
		t.Fatal("incomplete generation was accepted", err, partial.String())
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.CountTokens(cancelled, pack, []string{"Cancel before tokenization"}); !errors.Is(err, context.Canceled) {
		t.Fatal("tokenization ignored cancellation", err)
	}
	t.Logf("long input fact retained; token exhaustion rejected with partial stream; cancellation honored; total=%s", time.Since(started))
}
