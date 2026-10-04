package workspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

// This uses the actual compiled relay over OS pipes and a loopback HTTP socket,
// against the real SQLite-backed broker. It is bounded paired-client protocol
// evidence, not live Claude/Codex or remote-OAuth qualification.
func TestPhase5V10PairedRelayProtocolConsentAndCancellation(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Approved context")
	secret := createPage(t, k, e, "Private context")
	b := NewProviderBridge(k, e)
	defer b.Close()
	for _, provider := range []string{"claude_code", "codex", "api_key"} {
		if _, err := b.Connect(ctx, provider, []string{page}, []string{"readPage"}); err == nil {
			t.Fatalf("unqualified provider enabled: %s", provider)
		}
	}
	if _, err := b.Connect(ctx, "mcp", nil, []string{"readPage"}); err == nil {
		t.Fatal("context consent not required")
	}
	c, err := b.Connect(ctx, "mcp", []string{page}, []string{"readPage", "proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(b.MCPHandler())
	defer server.Close()
	binary := os.Getenv("TROVE_MCP_QUALIFICATION_BINARY")
	if binary == "" {
		binary = filepath.Join(t.TempDir(), "trove-mcp")
		build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-mod=readonly", "-o", binary, "../../cmd/trove-mcp")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build paired relay: %v\n%s", err, output)
		}
	}
	process := exec.Command(binary)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TROVE_MCP_") {
			process.Env = append(process.Env, entry)
		}
	}
	process.Env = append(process.Env, "TROVE_MCP_URL="+server.URL, "TROVE_MCP_TOKEN="+c.Token, "TROVE_MCP_CONNECTION="+c.ID)
	input, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	process.Stderr = &stderr
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- process.Wait() }()
	defer func() {
		input.Close()
		process.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("relay did not stop")
		}
	}()
	replies := make(chan map[string]any, 16)
	go func() {
		defer close(replies)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var reply map[string]any
			decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
			decoder.UseNumber()
			if decoder.Decode(&reply) != nil {
				return
			}
			replies <- reply
		}
	}()
	send := func(value any) {
		t.Helper()
		if _, err := input.Write(append(marshal(value), '\n')); err != nil {
			t.Fatal(err)
		}
	}
	receive := func() map[string]any {
		t.Helper()
		select {
		case r, ok := <-replies:
			if !ok {
				t.Fatal("relay closed stdout")
			}
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("relay request timed out")
			return nil
		}
	}
	call := func(id any, method string, params any) map[string]any {
		t.Helper()
		send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		r := receive()
		if r["jsonrpc"] != "2.0" || string(marshal(r["id"])) != string(marshal(id)) {
			t.Fatalf("invalid response framing: %s", marshal(r))
		}
		return r
	}
	result := call("initialize", "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "independent-stdio-driver", "version": "1"}})["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatal(result)
	}
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	listed := call(1, "tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
	if len(listed) != 2 {
		t.Fatal("unapproved tools advertised", listed)
	}
	tool := func(id any, name string, args map[string]any) map[string]any {
		return call(id, "tools/call", map[string]any{"name": name, "arguments": args})["result"].(map[string]any)
	}
	if tool(2, "readPage", map[string]any{"pageId": secret})["isError"] != true {
		t.Fatal("private page escaped scope")
	}
	if tool(3, "batch", map[string]any{})["isError"] != true {
		t.Fatal("direct mutation tool was accessible")
	}
	propose := func(id any, key, revision, title string) string {
		t.Helper()
		r := tool(id, "proposeChanges", map[string]any{"idempotencyKey": key, "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": title}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, revision)}})
		if r["isError"] == true {
			t.Fatal(r)
		}
		var envelope struct {
			Result ProviderProposal `json:"result"`
		}
		if err := json.Unmarshal([]byte(r["content"].([]any)[0].(map[string]any)["text"].(string)), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Result.ID
	}
	stale := propose(4, "stale-wire", "1", "unreviewed")
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Approved context" {
		t.Fatal("proposal applied without review")
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Owner edit"}}, required("page", page, "1"))))
	raw, err := b.Approve(ctx, stale)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	testCode(t, envelope, "CONFLICT")
	approved := propose(5, "review-wire", "2", "Reviewed provider edit")
	raw, err = b.Approve(ctx, approved)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &envelope)
	testOK(t, envelope)
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Reviewed provider edit" {
		t.Fatal("approved mutation missing")
	}
	// Adjacent integer IDs above 2^53 must stay distinct. A float64 request-key
	// conversion used to merge them and cancel or reject the wrong operation.
	k.writer <- struct{}{}
	locked := true
	defer func() {
		if locked {
			<-k.writer
		}
	}()
	for index, id := range []json.Number{"9007199254740992", "9007199254740993"} {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "proposeChanges", "arguments": map[string]any{"idempotencyKey": "cancel-wire-" + id.String(), "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Cancelled"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "3")}}}})
		deadline := time.Now().Add(3 * time.Second)
		for len(k.pending) < index+1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if len(k.pending) != index+1 {
			t.Fatal("concurrent request did not reach broker; request IDs may have collided")
		}
	}
	for _, id := range []json.Number{"9007199254740992", "9007199254740993"} {
		send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
		r := receive()
		if r["id"] != id || r["result"].(map[string]any)["isError"] != true {
			t.Fatalf("wrong cancellation response: %s", marshal(r))
		}
	}
	<-k.writer
	locked = false
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Reviewed provider edit" {
		t.Fatal("cancelled proposal changed workspace")
	}
	revoked := propose(6, "revoke-wire", "3", "Revoked")
	if err = b.Disconnect(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Approve(ctx, revoked); err == nil {
		t.Fatal("revoked proposal accepted")
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/list"})
	select {
	case _, ok := <-replies:
		if ok {
			t.Fatal("revoked client received a response")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoked relay remained open")
	}
}

func TestPhase5V02APIAuthenticationNetworkFailuresAndOfflineRestart(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sourceNamespace": "phase5-api"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	original := capture.Pending[0]
	for _, failure := range []string{"network", "authentication", "redirect"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != "api.notion.com" || r.Header.Get("Authorization") != "Bearer phase5-secret-token" {
					t.Fatal("unexpected credential destination", r.URL)
				}
				switch failure {
				case "network":
					return nil, errors.New("sensitive underlying network message")
				case "authentication":
					return notionResponse(401, `{"code":"unauthorized"}`), nil
				default:
					response := notionResponse(302, "")
					response.Header.Set("Location", "https://untrusted.example/steal")
					return response, nil
				}
			})}
			capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "phase5-secret-token", client)
			if err != nil || capture.State != "blocked" || len(capture.Pending) != 1 || capture.Pending[0] != original || capture.Completed != 0 || calls != 1 {
				t.Fatalf("failed request lost cursor or retried elsewhere: %+v %v calls=%d", capture, err, calls)
			}
			if strings.Contains(capture.Error, "sensitive underlying") {
				t.Fatal("transport details leaked")
			}
			var stored string
			if err = k.db.QueryRow("SELECT state_json FROM notion_captures WHERE id=?", capture.ID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stored, "phase5-secret-token") {
				t.Fatal("credential persisted")
			}
		})
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := loadNotionCapture(ctx, reopened.db, capture.ID)
	if err != nil || restored.Revision != capture.Revision || len(restored.Pending) != 1 || restored.Pending[0] != original {
		t.Fatal("restart lost blocked cursor", restored, err)
	}
	// Failure of the optional network integration never prevents local writes.
	local := createPage(t, reopened, reopened.OwnerContext(), "Offline after API failure")
	if readPage(t, reopened, reopened.OwnerContext(), local)["page"].(map[string]any)["title"] != "Offline after API failure" {
		t.Fatal("offline core unavailable")
	}
}

func TestPhase5V02V03HTMLSourceEvidenceSurvivesRestart(t *testing.T) {
	k, e := testKernel(t)
	source := `<html><body><h4>Local heading</h4><p>नमस्ते 世界 <a href="Child.html">child</a></p><script>sourceOnly()</script><iframe src="https://remote.example"></iframe></body></html>`
	plan := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Parent.html": source, "Child.html": "<p>Offline child</p>"}))
	var parent ImportItem
	for _, item := range plan.Items {
		if item.Locator == "Parent.html" {
			parent = item
		}
	}
	if parent.RawFileID == "" || parent.Outcome != "partial" {
		t.Fatal("source reconciliation absent", parent)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner := reopened.OwnerContext()
	file, _, err := reopened.ReadFile(context.Background(), owner, parent.RawFileID)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(evidence) != source {
		t.Fatal("source bytes changed")
	}
	page := string(marshal(readPage(t, reopened, owner, parent.PageID)))
	if !strings.Contains(page, "नमस्ते 世界") || !strings.Contains(page, "trove://page/") || strings.Contains(page, "sourceOnly()") || strings.Contains(page, "https://remote.example") {
		t.Fatal("inert offline blocks not preserved", page)
	}
	restored, err := loadImportPlan(context.Background(), reopened.db, plan.ID)
	if err != nil || restored.State != "complete" || restored.Hash != plan.Hash {
		t.Fatal("reconciliation lost on restart", err)
	}
}
