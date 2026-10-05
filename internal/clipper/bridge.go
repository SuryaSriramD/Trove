// Package clipper exposes only an explicitly paired, session-scoped capture
// draft channel. It has no filesystem, workspace command or arbitrary URL API.
package clipper

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaximumRequestBytes = 4 << 20

var extensionID = regexp.MustCompile(`^[a-p]{32}$`)

type Destination struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Revision string `json:"revision"`
}
type Image struct {
	URL       string `json:"url"`
	MediaType string `json:"mediaType"`
	Base64    string `json:"base64"`
}
type Capture struct {
	Version             int     `json:"version"`
	RequestID           string  `json:"requestId"`
	Title               string  `json:"title"`
	URL                 string  `json:"url"`
	HTML                string  `json:"html"`
	Text                string  `json:"text"`
	Mode                string  `json:"mode"`
	DestinationID       string  `json:"destinationId"`
	DestinationRevision string  `json:"destinationRevision"`
	CapturedAt          string  `json:"capturedAt"`
	ResourceConsent     bool    `json:"resourceConsent"`
	Images              []Image `json:"images"`
}
type Receipt struct {
	PlanID string `json:"planId"`
	State  string `json:"state"`
	Title  string `json:"title"`
}
type Config struct {
	ExtensionID  string
	Destinations func(context.Context) ([]Destination, error)
	Preview      func(context.Context, Capture) (Receipt, error)
}
type Pairing struct {
	Endpoint    string `json:"endpoint"`
	Code        string `json:"code"`
	ExtensionID string `json:"extensionId"`
	ExpiresAt   string `json:"expiresAt"`
	SessionOnly bool   `json:"sessionOnly"`
}
type Bridge struct {
	mu                  sync.Mutex
	config              Config
	server              *http.Server
	listener            net.Listener
	ctx                 context.Context
	cancel              context.CancelFunc
	origin, host        string
	codeHash, tokenHash [32]byte
	expires             time.Time
	paired, closed      bool
	attempts            int
	gate                chan struct{}
	receipts            map[string]cached
}
type cached struct {
	hash    [32]byte
	receipt Receipt
}

func secret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func Start(parent context.Context, config Config) (*Bridge, Pairing, error) {
	if !extensionID.MatchString(config.ExtensionID) || config.Destinations == nil || config.Preview == nil {
		return nil, Pairing{}, errors.New("invalid clipper configuration")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, Pairing{}, err
	}
	ctx, cancel := context.WithCancel(parent)
	code := secret()
	b := &Bridge{config: config, listener: listener, ctx: ctx, cancel: cancel, origin: "chrome-extension://" + config.ExtensionID, host: listener.Addr().String(), codeHash: sha256.Sum256([]byte(code)), expires: time.Now().Add(5 * time.Minute), gate: make(chan struct{}, 1), receipts: map[string]cached{}}
	b.server = &http.Server{Handler: b, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	go b.server.Serve(listener)
	go func() { <-ctx.Done(); b.Close() }()
	return b, Pairing{"http://" + b.host, code, config.ExtensionID, b.expires.UTC().Format(time.RFC3339), true}, nil
}
func (b *Bridge) Close() {
	b.mu.Lock()
	b.closed = true
	b.codeHash = [32]byte{}
	b.tokenHash = [32]byte{}
	b.mu.Unlock()
	b.cancel()
	// Serve starts asynchronously and may not have registered this listener
	// with http.Server yet. Revocation closes the owned socket synchronously.
	_ = b.listener.Close()
	_ = b.server.Close()
}
func (b *Bridge) Paired() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.paired && !b.closed }
func answer(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func deny(w http.ResponseWriter, status int, message string) {
	answer(w, status, map[string]any{"ok": false, "error": message})
}
func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() || r.Host != b.host || r.Header.Get("Origin") != b.origin || r.URL.RawQuery != "" {
		deny(w, 403, "Unpaired browser origin")
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", b.origin)
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		if r.Header.Get("Access-Control-Request-Method") != "POST" {
			deny(w, 405, "POST required")
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
		deny(w, 415, "JSON POST required")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaximumRequestBytes))
	if err != nil {
		deny(w, 413, "Capture exceeds 4 MiB")
		return
	}
	b.mu.Lock()
	closed := b.closed
	authorized := b.paired && subtle.ConstantTimeCompare(b.tokenHash[:], hashBearer(r.Header.Get("Authorization"))) == 1
	b.mu.Unlock()
	if closed {
		deny(w, 401, "Pairing revoked")
		return
	}
	if r.URL.Path == "/pair" {
		var req struct {
			Version int    `json:"version"`
			Code    string `json:"code"`
		}
		if err = decode(body, &req); err != nil || req.Version != 1 {
			deny(w, 400, "Invalid pairing request")
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		b.attempts++
		hash := sha256.Sum256([]byte(req.Code))
		if b.paired || b.closed || b.attempts > 10 || time.Now().After(b.expires) || subtle.ConstantTimeCompare(hash[:], b.codeHash[:]) != 1 {
			deny(w, 401, "Pairing code is invalid, expired or already used")
			return
		}
		token := secret()
		b.tokenHash = sha256.Sum256([]byte(token))
		b.codeHash = [32]byte{}
		b.paired = true
		answer(w, 200, map[string]any{"ok": true, "version": 1, "token": token, "sessionOnly": true})
		return
	}
	if !authorized {
		deny(w, 401, "Pairing expired or revoked; pair again in Trove")
		return
	}
	if r.URL.Path == "/destinations" {
		destinations, err := b.config.Destinations(r.Context())
		if err != nil {
			deny(w, 409, "Approved destinations changed; pair again")
			return
		}
		answer(w, 200, map[string]any{"ok": true, "destinations": destinations})
		return
	}
	if r.URL.Path != "/capture" {
		deny(w, 404, "Unknown clipper operation")
		return
	}
	var capture Capture
	if err = decode(body, &capture); err != nil || capture.Version != 1 || len(capture.RequestID) < 16 || len(capture.RequestID) > 100 {
		deny(w, 400, "Invalid capture request")
		return
	}
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	default:
		deny(w, 429, "Another capture is being reviewed; retry shortly")
		return
	}
	digest := sha256.Sum256(body)
	b.mu.Lock()
	prior, found := b.receipts[capture.RequestID]
	count := len(b.receipts)
	b.mu.Unlock()
	if found {
		if prior.hash != digest {
			deny(w, 409, "Capture request identity was reused with different content")
			return
		}
		answer(w, 200, map[string]any{"ok": true, "receipt": prior.receipt})
		return
	}
	if count >= 100 {
		deny(w, 429, "Session capture limit reached; pair again")
		return
	}
	destinations, err := b.config.Destinations(r.Context())
	if err != nil {
		deny(w, 409, "Destination unavailable")
		return
	}
	allowed := false
	for _, d := range destinations {
		if d.ID == capture.DestinationID && d.Revision == capture.DestinationRevision {
			allowed = true
		}
	}
	if !allowed {
		deny(w, 403, "Destination was not approved or its revision changed")
		return
	}
	receipt, err := b.config.Preview(r.Context(), capture)
	if err != nil {
		deny(w, 400, fmt.Sprintf("Capture review failed: %s", err))
		return
	}
	b.mu.Lock()
	b.receipts[capture.RequestID] = cached{digest, receipt}
	b.mu.Unlock()
	answer(w, 200, map[string]any{"ok": true, "receipt": receipt})
}
func hashBearer(value string) []byte {
	if !strings.HasPrefix(value, "Bearer ") {
		return nil
	}
	sum := sha256.Sum256([]byte(strings.TrimPrefix(value, "Bearer ")))
	return sum[:]
}
func decode(raw []byte, target any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("extra JSON data")
	}
	return nil
}
