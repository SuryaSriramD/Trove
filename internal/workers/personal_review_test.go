package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"trove.local/phase0/internal/hosting"
	"trove.local/phase0/internal/workspace"
)

const reviewHostToken = "review-host-owner-0123456789abcdef0123456789"

type reviewHTTP struct {
	t                   *testing.T
	k                   *workspace.Kernel
	server              *httptest.Server
	grant               any
	page, source, block string
}

func newReviewHTTP(t *testing.T) *reviewHTTP {
	t.Helper()
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	host, err := hosting.New(k, reviewHostToken, fstest.MapFS{"index.html": {Data: []byte("<html><head></head><body>Trove</body></html>")}}, false)
	if err != nil {
		k.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(host)
	t.Cleanup(func() { server.Close(); host.Close(); k.Close() })
	h := &reviewHTTP{t: t, k: k, server: server, page: workspace.NewID(), source: workspace.NewID(), block: workspace.NewID()}
	h.grant = h.ok("workspace.snapshot", nil)["scopeGrantId"]
	fact := workspace.NewID()
	plan := workspace.PersonalReviewPlan{Type: "trove.personal-review/v1", Name: "Hosted review", Enabled: true, SourcePageIDs: []string{h.source}, Schedule: workspace.AutomationTrigger{Type: "schedule", Timezone: "UTC", Frequency: "daily", Hour: 9, MissedPolicy: "catch_up", CatchUpLimit: 1}, Generative: &workspace.PersonalReviewGenerationConfig{ModelPackID: workspace.NewID(), Categories: []string{"summary", "risks"}, AllowedActions: []string{"append_review"}, ActionPageIDs: []string{}}}
	raw, _ := json.Marshal(plan)
	h.batch([]any{
		map[string]any{"kind": "page.create", "pageId": h.page, "title": "Review inbox"},
		map[string]any{"kind": "page.create", "pageId": h.source, "title": "Current research evidence"},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": fact, "pageId": h.source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "The launch is blocked by the pending security review."}}}}},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": h.block, "pageId": h.page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(raw), "language": "trove-review"}}},
		map[string]any{"kind": "review.activate", "pageId": h.page, "planBlockId": h.block, "enabled": true},
	}, reviewGuard("page", h.page, "0"), reviewGuard("page", h.source, "0"), reviewGuard("block", h.block, "0"), reviewGuard("block", fact, "0"))
	return h
}
func reviewGuard(kind, id, revision string) any {
	return map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": revision}
}
func (h *reviewHTTP) call(method string, fields map[string]any) map[string]any {
	h.t.Helper()
	body := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": h.k.ID()}
	for key, value := range fields {
		body[key] = value
	}
	raw, _ := json.Marshal(map[string]any{"method": method, "request": body})
	req, err := http.NewRequest("POST", h.server.URL+"/api", bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+reviewHostToken)
	req.Header.Set("Origin", h.server.URL)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
		h.t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		h.t.Fatalf("HTTP %s: %#v", response.Status, out)
	}
	return out
}
func (h *reviewHTTP) ok(method string, fields map[string]any) map[string]any {
	h.t.Helper()
	out := h.call(method, fields)
	if out["ok"] != true {
		h.t.Fatalf("%s: %#v", method, out)
	}
	return out["result"].(map[string]any)
}
func (h *reviewHTTP) batch(commands []any, guards ...any) {
	h.t.Helper()
	h.ok("batch", map[string]any{"commands": commands, "mutation": map[string]any{"scopeGrantId": h.grant, "idempotencyKey": workspace.NewID(), "expectedRevisions": guards}})
}
func (h *reviewHTTP) queue() string {
	h.t.Helper()
	return h.ok("review.run", map[string]any{"planBlockId": h.block})["id"].(string)
}
func (h *reviewHTTP) run(id string) map[string]any {
	h.t.Helper()
	for _, raw := range h.ok("review.list", nil)["runs"].([]any) {
		run := raw.(map[string]any)
		if run["id"] == id {
			return run
		}
	}
	h.t.Fatal("missing run", id)
	return nil
}
func (h *reviewHTTP) wait(id string, statuses ...string) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run := h.run(id)
		for _, status := range statuses {
			if run["status"] == status {
				return run
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("review did not reach %v: %#v", statuses, run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func startReviewWorker(t *testing.T, h *reviewHTTP, g personalReviewGenerator) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runPersonalReviews(ctx, h.k, g, 10*time.Millisecond) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("worker shutdown: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("review worker did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

type reviewGenerator struct {
	entered, stopped chan struct{}
	invalid          bool
}

func (g *reviewGenerator) GenerationContext(string) (int, error) { return 8192, nil }
func (g *reviewGenerator) CountTokens(_ context.Context, _ string, texts []string) ([]int, error) {
	counts := make([]int, len(texts))
	for i, s := range texts {
		counts[i] = len(s)/4 + 1
	}
	return counts, nil
}
func (g *reviewGenerator) GenerateTextWithGrammar(ctx context.Context, _ string, prompt string, _ int, grammar string, _ func(string)) (string, error) {
	if g.entered != nil {
		close(g.entered)
		<-ctx.Done()
		close(g.stopped)
		return "", ctx.Err()
	}
	if !strings.Contains(prompt, "pending security review") || grammar == "" {
		return "", errors.New("missing evidence or constrained output grammar")
	}
	if g.invalid {
		return `{"claims":[{"category":"risks","text":"Invented","quote":"Nonexistent evidence"}],"actions":[]}`, nil
	}
	return `{"claims":[{"category":"risks","text":"The launch still awaits security review.","quote":"The launch is blocked by the pending security review."}],"actions":[]}`, nil
}
func TestHostedPersonalReviewFailureRetryAndReviewedApplication(t *testing.T) {
	h := newReviewHTTP(t)
	id := h.queue()
	stop := startReviewWorker(t, h, nil)
	failed := h.wait(id, "failed")
	if !strings.Contains(failed["error"].(string), "--models") {
		t.Fatal("missing host recovery instructions", failed)
	}
	stop()
	h.ok("review.retry", map[string]any{"runId": id})
	stop = startReviewWorker(t, h, &reviewGenerator{})
	ready := h.wait(id, "ready")
	if !strings.Contains(ready["text"].(string), "[1]") && !strings.Contains(ready["text"].(string), "[2]") {
		t.Fatal("missing citations", ready)
	}
	before := h.ok("readPage", map[string]any{"pageId": h.page})
	if out := h.call("review.apply", map[string]any{"runId": id}); out["ok"] != false {
		t.Fatal("generated analysis applied without proposal review", out)
	}
	h.ok("review.propose", map[string]any{"runId": id})
	h.ok("review.apply", map[string]any{"runId": id})
	h.wait(id, "accepted")
	after := h.ok("readPage", map[string]any{"pageId": h.page})
	if len(after["blocks"].([]any)) <= len(before["blocks"].([]any)) {
		t.Fatal("approval did not append reviewed analysis")
	}
	h.ok("review.apply", map[string]any{"runId": id})
	again := h.ok("readPage", map[string]any{"pageId": h.page})
	if len(again["blocks"].([]any)) != len(after["blocks"].([]any)) {
		t.Fatal("duplicate approval repeated canonical effects")
	}
	stop()
}
func TestHostedPersonalReviewStopsForCancellationStalenessAndShutdown(t *testing.T) {
	for _, mode := range []string{"cancel", "source", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			h := newReviewHTTP(t)
			id := h.queue()
			g := &reviewGenerator{entered: make(chan struct{}), stopped: make(chan struct{})}
			stop := startReviewWorker(t, h, g)
			select {
			case <-g.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start generation")
			}
			switch mode {
			case "cancel":
				h.ok("review.cancel", map[string]any{"runId": id})
			case "source":
				page := h.ok("readPage", map[string]any{"pageId": h.source})["page"].(map[string]any)
				h.batch([]any{map[string]any{"kind": "page.update", "pageId": h.source, "title": "Changed evidence"}}, reviewGuard("page", h.source, page["revision"].(string)))
			case "shutdown":
				stop()
			}
			select {
			case <-g.stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("native generation remained active after cancellation or changed evidence")
			}
			run := h.wait(id, "cancelled", "failed")
			if run["proposalId"] != "" && run["proposalId"] != nil {
				t.Fatal("cancelled output acquired proposal", run)
			}
			stop()
			if mode == "cancel" {
				h.ok("review.retry", map[string]any{"runId": id})
				startReviewWorker(t, h, &reviewGenerator{})
				h.wait(id, "ready")
			}
		})
	}
}
func TestHostedPersonalReviewMalformedOutputHasNoCanonicalEffects(t *testing.T) {
	h := newReviewHTTP(t)
	before := h.ok("readPage", map[string]any{"pageId": h.page})
	id := h.queue()
	startReviewWorker(t, h, &reviewGenerator{invalid: true})
	h.wait(id, "failed")
	after := h.ok("readPage", map[string]any{"pageId": h.page})
	if len(after["blocks"].([]any)) != len(before["blocks"].([]any)) {
		t.Fatal("unsupported generation changed content")
	}
	if h.call("review.propose", map[string]any{"runId": id})["ok"] != false {
		t.Fatal("failed generation was reviewable")
	}
}
