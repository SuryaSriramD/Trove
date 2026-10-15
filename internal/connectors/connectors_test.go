package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type memorySecrets struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *memorySecrets) Get(_ context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.values[id]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte{}, b...), nil
}
func (s *memorySecrets) Put(_ context.Context, id string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[id] = append([]byte{}, b...)
	return nil
}
func (s *memorySecrets) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, id)
	return nil
}
func TestConnectorHTTPOriginBoundsAndUnknownWrites(t *testing.T) {
	ctx := context.Background()
	calls := 0
	s := Session{Account: Account{Kind: GitHub}, Token: Secret{AccessToken: "fake-secret"}, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection interrupted")
	})}}
	if _, err := request(ctx, s, "POST", "https://attacker.example/api", map[string]any{}, nil, nil); err == nil || calls != 0 {
		t.Fatal("token would leave provider origin")
	}
	_, err := request(ctx, s, "POST", "https://api.github.com/repos/a/b/issues", map[string]any{}, nil, nil)
	var f *Failure
	if !errors.As(err, &f) || !f.OutcomeUnknown {
		t.Fatal("transport write must be unknown", err)
	}
	_, err = request(ctx, s, "GET", "https://api.github.com/user", nil, nil, &map[string]any{})
	if !errors.As(err, &f) || f.OutcomeUnknown {
		t.Fatal("read must not masquerade as a write", err)
	}
	s.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 502, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("private provider diagnostic"))}, nil
	})
	_, err = request(ctx, s, "PATCH", "https://api.github.com/repos/a/b/issues/1", map[string]any{}, nil, nil)
	if !errors.As(err, &f) || !f.OutcomeUnknown || strings.Contains(err.Error(), "private") {
		t.Fatal("server failure became replayable or leaked body", err)
	}
	s.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	_, err = request(ctx, s, "GET", "https://api.github.com/user", nil, nil, &map[string]any{})
	if !errors.As(err, &f) || f.RetryAfter != 120*time.Second {
		t.Fatal("rate limit evidence missing", err)
	}
	for _, endpoint := range []string{"http://example.com", "https://user:pass@example.com", "https://127.0.0.1/mcp", "https://10.0.0.1/mcp", "https://100.64.0.1/mcp", "https://example.com:8080/mcp"} {
		if ValidateEndpoint(endpoint) == nil {
			t.Fatal("unsafe endpoint", endpoint)
		}
	}
	if ValidateEndpoint("https://example.com/mcp") != nil {
		t.Fatal("public endpoint denied")
	}
}
func TestConnectorCredentialsBoundToWorkspaceAccountAndRedacted(t *testing.T) {
	ctx := context.Background()
	store := &memorySecrets{values: map[string][]byte{}}
	broker := newBroker(store, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network"); return nil, nil })})
	defer broker.Close()
	a := Account{ID: uuid.NewString(), Kind: GitHub, Identity: Identity{Subject: "42", Provider: "github"}, State: "active", RegistrationID: uuid.NewString()}
	c := protectedCredential{Version: 1, WorkspaceID: "workspace", WorkspacePath: "/private/workspace", AccountID: a.ID, Registration: Registration{Kind: GitHub, ClientID: "client"}, Identity: a.Identity, AccessToken: "fake-token", ExpiresAt: time.Now().Add(time.Hour)}
	data, _ := json.Marshal(c)
	_ = store.Put(ctx, a.RegistrationID, data)
	s, err := broker.Session(ctx, "workspace", "/private/workspace", a)
	if err != nil || s.Token.AccessToken != "fake-token" {
		t.Fatal("valid local authorization failed", err)
	}
	for _, rendered := range []string{fmt.Sprintf("%v", s.Token), fmt.Sprintf("%+v", s.Token), fmt.Sprintf("%#v", s.Token), string(mustJSON(s.Token))} {
		if strings.Contains(rendered, "fake-token") {
			t.Fatal("secret serialized")
		}
	}
	if _, err = broker.Session(ctx, "workspace", "/restored/workspace", a); err == nil {
		t.Fatal("restored path activated credentials")
	}
	other := a
	other.Identity.Subject = "other"
	if _, err = broker.Session(ctx, "workspace", "/private/workspace", other); err == nil {
		t.Fatal("account switch used old token")
	}
	a.State = "restored_inactive"
	if _, err = broker.Session(ctx, "workspace", "/private/workspace", a); err == nil {
		t.Fatal("inactive restoration authenticated")
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func TestGoogleGrantScopesAndPKCEExchange(t *testing.T) {
	if googleScope(GoogleDrive) != "https://www.googleapis.com/auth/drive.file" {
		t.Fatal("picker grant broadened")
	}
	for _, kind := range []Kind{GoogleCalendar, Gmail} {
		if strings.Contains(googleScope(kind), "drive.file") {
			t.Fatal("picker combined with other grant")
		}
	}
	store := &memorySecrets{values: map[string][]byte{}}
	a := &authAttempt{registration: Registration{Kind: GoogleDrive, ClientID: "desktop.apps.googleusercontent.com"}, verifier: "synthetic-pkce-verifier", redirect: "http://127.0.0.1:1234/callback", workspaceID: "workspace", workspacePath: "/workspace", accountID: uuid.NewString()}
	broker := newBroker(store, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://oauth2.googleapis.com/token" || r.Method != "POST" {
			t.Fatal("wrong token endpoint")
		}
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("code_verifier") != a.verifier || values.Get("code") != "synthetic-code" || values.Get("redirect_uri") != a.redirect {
			t.Fatal("PKCE binding missing")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/drive.file"}`))}, nil
	})})
	defer broker.Close()
	if _, err := broker.exchangeGoogle(context.Background(), a, "synthetic-code"); err != nil {
		t.Fatal(err)
	}
	a.registration.Kind = Gmail
	if _, err := broker.exchangeGoogle(context.Background(), a, "synthetic-code"); err == nil {
		t.Fatal("missing granted scopes accepted")
	}
}
