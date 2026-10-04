// trove-mcp relays MCP stdio to an explicitly paired running Trove app. It never
// opens a workspace database, discovers files, or reads vendor credentials.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	if err := relay(); err != nil {
		fmt.Fprintln(os.Stderr, "Trove bridge:", err)
		os.Exit(1)
	}
}
func relay() error {
	endpoint := os.Getenv("TROVE_MCP_URL")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil {
		return fmt.Errorf("expected paired loopback endpoint")
	}
	token, id := os.Getenv("TROVE_MCP_TOKEN"), os.Getenv("TROVE_MCP_CONNECTION")
	if token == "" || id == "" {
		return fmt.Errorf("pair this client in Trove first")
	}
	return relayStreams(os.Stdin, os.Stdout, endpoint, token, id)
}

// Keep reading stdio while requests are running so cancellation notifications
// can reach the server. Only this goroutine writes stdout, preserving framing
// when independent requests finish out of order. Cancellation has reserved
// capacity and cannot be blocked behind the requests it needs to interrupt.
func relayStreams(input io.Reader, output io.Writer, endpoint, token, id string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type line struct {
		body []byte
		err  error
	}
	lines := make(chan line)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if len(strings.TrimSpace(scanner.Text())) == 0 {
				continue
			}
			body := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line{body: body}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- line{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	type completion struct {
		body         []byte
		err          error
		cancellation bool
	}
	done := make(chan completion, 40)
	requests, cancellations := 0, 0
	transport := &relayTransport{}
	for lines != nil || requests+cancellations > 0 {
		select {
		case next, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			if next.err != nil {
				return next.err
			}
			var header struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(next.body, &header) != nil {
				return fmt.Errorf("invalid JSON-RPC input")
			}
			isCancellation := header.Method == "notifications/cancelled"
			if isCancellation {
				if cancellations >= 8 {
					return fmt.Errorf("too many concurrent cancellations")
				}
				cancellations++
			} else {
				if requests >= 32 {
					return fmt.Errorf("too many concurrent requests")
				}
				requests++
			}
			go func(body []byte, cancellation bool) {
				response, err := transport.request(ctx, client, endpoint, token, id, body)
				done <- completion{body: response, err: err, cancellation: cancellation}
			}(next.body, isCancellation)
		case result := <-done:
			if result.cancellation {
				cancellations--
			} else {
				requests--
			}
			if result.err != nil {
				return result.err
			}
			if len(result.body) > 0 {
				if _, err := output.Write(append(bytes.TrimSpace(result.body), '\n')); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type relayTransport struct {
	mu               sync.Mutex
	session, version string
}

func (t *relayTransport) request(ctx context.Context, client *http.Client, endpoint, token, id string, body []byte) ([]byte, error) {
	// The pinned SDK decodes numeric JSON-RPC IDs through float64. Encode only
	// transport identities as strings so adjacent large integers and cancellation
	// IDs remain distinct; content arguments are never rewritten.
	encoded, original, err := relayIDs(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Trove-Connection-Id", id)
	t.mu.Lock()
	session, version := t.session, t.version
	t.mu.Unlock()
	var rpc struct {
		Method string `json:"method"`
		Params struct {
			Meta map[string]any `json:"_meta"`
			Name string         `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &rpc) != nil {
		return nil, fmt.Errorf("invalid JSON-RPC input")
	}
	if explicit, ok := rpc.Params.Meta["io.modelcontextprotocol/protocolVersion"].(string); ok {
		version = explicit
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	if version >= "2026-07-28" {
		req.Header.Set("Mcp-Method", rpc.Method)
		if rpc.Params.Name != "" {
			req.Header.Set("Mcp-Name", rpc.Params.Name)
		}
	} else if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection interrupted")
	}
	defer res.Body.Close()
	response, err := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024+1))
	if err != nil || len(response) > 4*1024*1024 {
		return nil, fmt.Errorf("response too large or incomplete")
	}
	if res.StatusCode == 202 || res.StatusCode == 204 {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("connection rejected (%d)", res.StatusCode)
	}
	if !json.Valid(response) {
		return nil, fmt.Errorf("invalid JSON-RPC response")
	}
	if len(original) > 0 {
		var message map[string]json.RawMessage
		if json.Unmarshal(response, &message) == nil && message["id"] != nil {
			message["id"] = original
			response, _ = json.Marshal(message)
		}
	}
	if rpc.Method == "initialize" {
		var initialized struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(response, &initialized) == nil && initialized.Result.ProtocolVersion != "" {
			t.mu.Lock()
			t.session = res.Header.Get("Mcp-Session-Id")
			t.version = initialized.Result.ProtocolVersion
			t.mu.Unlock()
		}
	}
	return response, nil
}

func relayIDs(body []byte) ([]byte, json.RawMessage, error) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON-RPC input")
	}
	identity := func(raw json.RawMessage) (json.RawMessage, error) {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		switch value.(type) {
		case string, json.Number:
		default:
			return nil, fmt.Errorf("invalid JSON-RPC request identity")
		}
		canonical, _ := json.Marshal(value)
		return json.Marshal("trove-relay:" + base64.RawURLEncoding.EncodeToString(canonical))
	}
	original := message["id"]
	if len(original) > 0 {
		encoded, err := identity(original)
		if err != nil {
			return nil, nil, err
		}
		message["id"] = encoded
	}
	var method string
	_ = json.Unmarshal(message["method"], &method)
	if method == "notifications/cancelled" {
		var params map[string]json.RawMessage
		if err := json.Unmarshal(message["params"], &params); err != nil {
			return nil, nil, err
		}
		encoded, err := identity(params["requestId"])
		if err != nil {
			return nil, nil, err
		}
		params["requestId"] = encoded
		message["params"], _ = json.Marshal(params)
	}
	encoded, err := json.Marshal(message)
	return encoded, original, err
}
