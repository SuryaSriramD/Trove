package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func authJSON(status int, v any) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(mustMCPJSON(v))))}
}
func awaitMCPAuthorization(t *testing.T, b *Broker, id, state string) AuthorizationAttempt {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a, err := b.Attempt(id)
		if err != nil {
			t.Fatal(err)
		}
		if a.State == state {
			return a
		}
		if a.State == "failed" || a.State == "expired" || a.State == "cancelled" {
			t.Fatalf("authorization %s: %s", a.State, a.Message)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("authorization did not finish")
	return AuthorizationAttempt{}
}
func TestMCPOAuthNegotiationNativeCallbackPersistenceAndRefresh(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered", true: "explicit_dynamic"}[dynamic], func(t *testing.T) {
			f := newMCPFixture(t)
			var authMu sync.Mutex
			challenge := ""
			redirect := ""
			var registrations, exchanges, refreshes atomic.Int32
			nativeClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Host == "mcp.example" && strings.Contains(r.URL.Path, ".well-known"):
					return authJSON(200, map[string]any{"resource": mcpEndpoint, "authorization_servers": []string{"https://auth.example"}, "scopes_supported": []string{"notes.read", "notes.write"}}), nil
				case r.URL.Host == "auth.example" && strings.Contains(r.URL.Path, ".well-known"):
					return authJSON(200, map[string]any{"issuer": "https://auth.example", "authorization_endpoint": "https://auth.example/authorize", "token_endpoint": "https://auth.example/token", "registration_endpoint": "https://auth.example/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"notes.read", "notes.write", "offline_access"}, "authorization_response_iss_parameter_supported": true}), nil
				case r.URL.String() == "https://auth.example/register":
					registrations.Add(1)
					var metadata map[string]any
					if json.NewDecoder(r.Body).Decode(&metadata) != nil {
						t.Error("invalid registration")
					}
					if !dynamic || metadata["token_endpoint_auth_method"] != "none" || metadata["application_type"] != "native" {
						t.Error("dynamic native registration not explicit")
					}
					return authJSON(201, map[string]any{"client_id": "dynamic-client", "token_endpoint_auth_method": "none", "redirect_uris": metadata["redirect_uris"]}), nil
				case r.URL.String() == "https://auth.example/token":
					body, _ := io.ReadAll(r.Body)
					values, _ := url.ParseQuery(string(body))
					if values.Get("resource") != mcpEndpoint {
						t.Error("token resource binding missing")
					}
					if values.Get("grant_type") == "refresh_token" {
						refreshes.Add(1)
						if values.Get("refresh_token") != "synthetic-refresh" {
							t.Error("wrong refresh token")
						}
						return authJSON(200, map[string]any{"access_token": "refreshed-token", "refresh_token": "rotated-refresh", "token_type": "Bearer", "expires_in": 3600}), nil
					}
					exchanges.Add(1)
					sum := sha256.Sum256([]byte(values.Get("code_verifier")))
					authMu.Lock()
					expectedChallenge, expectedRedirect := challenge, redirect
					authMu.Unlock()
					if base64.RawURLEncoding.EncodeToString(sum[:]) != expectedChallenge || values.Get("code") != "synthetic-code" || values.Get("redirect_uri") != expectedRedirect {
						t.Error("PKCE/code binding missing")
					}
					clientID := values.Get("client_id")
					if clientID == "" {
						var secret string
						clientID, secret, _ = r.BasicAuth()
						if secret != "" {
							t.Error("native fixture received a client secret")
						}
					}
					if dynamic && clientID != "dynamic-client" || !dynamic && clientID != "registered-client" {
						t.Error("wrong client registration")
					}
					return authJSON(200, map[string]any{"access_token": "synthetic-secret", "refresh_token": "synthetic-refresh", "token_type": "Bearer", "expires_in": 3600}), nil
				case r.URL.String() == mcpEndpoint:
					if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
						res := authJSON(401, map[string]any{})
						res.Header.Set("WWW-Authenticate", `Bearer resource_metadata="https://mcp.example/.well-known/oauth-protected-resource"`)
						return res, nil
					}
					return f.session.Client.Transport.RoundTrip(r)
				default:
					t.Error("unexpected authorization destination", r.URL)
					return authJSON(400, map[string]any{}), nil
				}
			})}
			store := &memorySecrets{values: map[string][]byte{}}
			broker := newBroker(store, nativeClient)
			defer broker.Close()
			registration := Registration{Kind: MCP, Endpoint: mcpEndpoint, DynamicClientRegistration: dynamic}
			if !dynamic {
				registration.ClientID = "registered-client"
			}
			attempt, err := broker.BeginMCP(context.Background(), "workspace", "/private/workspace", "account", registration, func(raw string) error {
				u, _ := url.Parse(raw)
				q := u.Query()
				if u.Scheme != "https" || u.Host != "auth.example" || q.Get("code_challenge_method") != "S256" || q.Get("resource") != mcpEndpoint {
					t.Error("bad browser authorization")
				}
				authMu.Lock()
				challenge = q.Get("code_challenge")
				redirect = q.Get("redirect_uri")
				authMu.Unlock()
				bad := q.Get("redirect_uri") + "?code=synthetic-code&state=wrong"
				res, err := http.Get(bad)
				if err != nil {
					return err
				}
				res.Body.Close()
				if res.StatusCode != 400 {
					t.Error("wrong state accepted")
				}
				callback := q.Get("redirect_uri") + "?" + url.Values{"code": {"synthetic-code"}, "state": {q.Get("state")}, "iss": {"https://auth.example"}}.Encode()
				res, err = http.Get(callback)
				if err != nil {
					return err
				}
				defer res.Body.Close()
				if res.StatusCode != 200 {
					t.Error("valid callback rejected")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			awaitMCPAuthorization(t, broker, attempt.ID, "authorized")
			account, err := broker.Complete(context.Background(), attempt.ID, NewMCPAdapter())
			if err != nil {
				t.Fatal(err)
			}
			if account.Identity.Provider != "mcp:"+mcpEndpoint || account.Identity.Subject == "" || strings.Contains(string(mustMCPJSON(account)), "synthetic") {
				t.Fatal("grant identity or secret boundary")
			}
			if exchanges.Load() != 1 || registrations.Load() != map[bool]int32{false: 0, true: 1}[dynamic] {
				t.Fatal("authorization repeated")
			}
			raw, _ := store.Get(context.Background(), account.RegistrationID)
			var credential protectedCredential
			if json.Unmarshal(raw, &credential) != nil {
				t.Fatal("invalid stored credential")
			}
			if credential.MCP == nil || credential.MCP.Resource != mcpEndpoint || credential.RefreshToken != "synthetic-refresh" {
				t.Fatal("protected refresh state missing")
			}
			credential.ExpiresAt = time.Now().Add(-time.Minute)
			raw, _ = json.Marshal(credential)
			store.Put(context.Background(), account.RegistrationID, raw)
			account.State = "active"
			session, err := broker.Session(context.Background(), "workspace", "/private/workspace", account)
			if err != nil || session.Token.AccessToken != "refreshed-token" || refreshes.Load() != 1 {
				t.Fatal("refresh", err)
			}
			saved, _ := store.Get(context.Background(), account.RegistrationID)
			json.Unmarshal(saved, &credential)
			if credential.RefreshToken != "rotated-refresh" || credential.Identity.Subject != account.Identity.Subject {
				t.Fatal("rotation identity/state lost")
			}
			if _, err = broker.Session(context.Background(), "workspace", "/restored/workspace", account); err == nil {
				t.Fatal("restore reused credentials")
			}
			if _, err = broker.Complete(context.Background(), attempt.ID, NewMCPAdapter()); err == nil {
				t.Fatal("completed attempt consumed again")
			}
		})
	}
}
func TestMCPOAuthCancellationAndRegistrationBounds(t *testing.T) {
	store := &memorySecrets{values: map[string][]byte{}}
	calls := atomic.Int32{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	broker := newBroker(store, client)
	defer broker.Close()
	for _, r := range []Registration{{Kind: MCP, Endpoint: mcpEndpoint}, {Kind: MCP, Endpoint: "https://127.0.0.1/mcp", ClientID: "client"}, {Kind: MCP, Endpoint: mcpEndpoint, ClientID: "client", DynamicClientRegistration: true}, {Kind: Notion, Endpoint: mcpEndpoint, DynamicClientRegistration: true}} {
		if _, err := broker.BeginMCP(context.Background(), "workspace", "/private/workspace", "account", r, func(string) error { return nil }); err == nil {
			t.Fatal("invalid registration accepted", r)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid registration sent a request")
	}
	a, err := broker.BeginMCP(context.Background(), "workspace", "/private/workspace", "account", Registration{Kind: MCP, Endpoint: mcpEndpoint, ClientID: "client"}, func(string) error { t.Error("browser opened after cancellation"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = broker.Cancel(a.ID); err != nil {
		t.Fatal(err)
	}
	state, err := broker.Attempt(a.ID)
	if err != nil || state.State != "cancelled" {
		t.Fatal(state, err)
	}
	if _, err = broker.Complete(context.Background(), a.ID, NewMCPAdapter()); err == nil {
		t.Fatal("cancelled grant activated")
	}
	if len(store.values) != 0 {
		t.Fatal("cancelled auth stored credentials")
	}
}
