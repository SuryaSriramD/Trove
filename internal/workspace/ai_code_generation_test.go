package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAICodeTransportKeepsLiteralSourceAndHidesProtocol(t *testing.T) {
	for _, source := range []string{"```go\r\n// retain me\r\nfunc F() int { return 7 }\r\n```\r\n", "  const marker = `\\n`;\n// résumé 🦉\n", "\\ud800 is literal source text", strings.Repeat("x", 16000)} {
		raw, _ := json.Marshal(map[string]string{"source": source})
		var stream strings.Builder
		p := AIPrepared{Run: AIRun{Request: AIRequest{Mode: "code_comment"}}}
		got, err := GeneratePreparedAI(context.Background(), p, func(_ context.Context, _, _ string, _ int, grammar string, token func(string)) (string, error) {
			if grammar == "" || token != nil {
				t.Fatal("protocol was exposed")
			}
			return string(raw), nil
		}, func(v string) { stream.WriteString(v) })
		if err != nil || got.Text != source || stream.String() != source {
			t.Fatalf("literal source changed: %q %v", got.Text, err)
		}
	}
	for _, raw := range []string{`{"source":"\ud83e\udd89"}`, `{"source":"\u0061"}`} {
		got, err := decodeAICodeDraft(raw)
		if err != nil || (got != "🦉" && got != "a") {
			t.Fatal(got, err)
		}
	}
}

func TestAICodeTransportRejectsAmbiguousIncompleteAndChangedUnicode(t *testing.T) {
	cases := []string{`{}`, `{"source":null}`, `{"source":""}`, `{"source":7}`, `{"source":"a","source":"b"}`, `{"Source":"a"}`, `{"source":"a","extra":true}`, `{"source":"a"} {}`, `{"source":"unfinished`, "```json\n{\"source\":\"a\"}\n```", `{"source":"\ud800"}`, `{"source":"\udc00"}`, `{"source":"\ud800\u0061"}`, `{"source":"\u0000"}`, "{\"source\":\"\xff\"}"}
	large, _ := json.Marshal(map[string]string{"source": strings.Repeat("x", 16001)})
	cases = append(cases, string(large), strings.Repeat(" ", 384<<10)+`{"source":"a"}`)
	for _, raw := range cases {
		emitted := false
		got, err := GeneratePreparedAI(context.Background(), AIPrepared{Run: AIRun{Request: AIRequest{Mode: "code_refactor"}}}, func(context.Context, string, string, int, string, func(string)) (string, error) { return raw, nil }, func(string) { emitted = true })
		if err == nil || got.Text != "" || emitted {
			t.Fatalf("malformed source published: %q %v", got.Text, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := GeneratePreparedAI(ctx, AIPrepared{Run: AIRun{Request: AIRequest{Mode: "code_comment"}}}, func(context.Context, string, string, int, string, func(string)) (string, error) {
		return `{"source":"safe"}`, nil
	}, func(string) { t.Fatal("cancelled source published") })
	if err != context.Canceled || got.Text != "" {
		t.Fatal(got, err)
	}
}
