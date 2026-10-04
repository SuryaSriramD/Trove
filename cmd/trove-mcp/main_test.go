package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhase5RelayRequiresPairedLoopbackEndpoint(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1:8765/mcp", "http://localhost:8765/mcp", "http://external.example/mcp", "http://token@127.0.0.1:8765/mcp", ""} {
		t.Run(endpoint, func(t *testing.T) {
			t.Setenv("TROVE_MCP_URL", endpoint)
			t.Setenv("TROVE_MCP_TOKEN", "paired-secret")
			t.Setenv("TROVE_MCP_CONNECTION", "connection")
			if err := relay(); err == nil || err.Error() != "expected paired loopback endpoint" {
				t.Fatal("unpaired endpoint accepted", err)
			}
		})
	}
	for _, missing := range []string{"TROVE_MCP_TOKEN", "TROVE_MCP_CONNECTION"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("TROVE_MCP_URL", "http://127.0.0.1:8765/mcp")
			t.Setenv("TROVE_MCP_TOKEN", "paired-secret")
			t.Setenv("TROVE_MCP_CONNECTION", "connection")
			t.Setenv(missing, "")
			if err := relay(); err == nil || err.Error() != "pair this client in Trove first" {
				t.Fatal("unpaired relay accepted", err)
			}
		})
	}
}

func TestPhase5RelayTransportFailuresStayLocal(t *testing.T) {
	const request = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	var escaped atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1); w.Write([]byte(`{"unexpected":true}`)) }))
	defer destination.Close()
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"authentication", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "private server detail", http.StatusUnauthorized)
		}, "connection rejected (401)"},
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }, "connection rejected (302)"},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("private non-JSON data")) }, "invalid JSON-RPC response"},
		{"oversized", func(w http.ResponseWriter, r *http.Request) { w.Write(bytes.Repeat([]byte("x"), (4<<20)+1)) }, "response too large or incomplete"},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("{"))
		}, "response too large or incomplete"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer paired-secret" || r.Header.Get("Trove-Connection-Id") != "connection" {
					t.Error("pairing headers absent")
				}
				test.handler(w, r)
			}))
			defer server.Close()
			var output bytes.Buffer
			err := relayStreams(strings.NewReader(request), &output, server.URL, "paired-secret", "connection")
			if err == nil || err.Error() != test.want || output.Len() != 0 || calls.Load() != 1 {
				t.Fatalf("unexpected failure boundary: %v output=%q calls=%d", err, output.String(), calls.Load())
			}
		})
	}
	if escaped.Load() != 0 {
		t.Fatal("pairing credential followed a redirect")
	}
	t.Run("network", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		endpoint := server.URL
		server.Close()
		var output bytes.Buffer
		err := relayStreams(strings.NewReader(request), &output, endpoint, "paired-secret", "connection")
		if err == nil || err.Error() != "connection interrupted" || output.Len() != 0 {
			t.Fatal("network failure not closed", err, output.String())
		}
	})
}

func TestPhase5RelayFailureDoesNotWaitForStdinEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- relayStreams(input, io.Discard, server.URL, "paired-secret", "connection") }()
	if _, err := fmt.Fprintln(writer, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != "connection rejected (401)" {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection failure waited for client EOF")
	}
}
