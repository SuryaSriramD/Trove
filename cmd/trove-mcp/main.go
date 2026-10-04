// trove-mcp relays MCP stdio to an explicitly paired running Trove app. It never
// opens a workspace database, discovers files, or reads vendor credentials.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
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
				response, err := relayRequest(ctx, client, endpoint, token, id, body)
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

func relayRequest(ctx context.Context, client *http.Client, endpoint, token, id string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Trove-Connection-Id", id)
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
	return response, nil
}
