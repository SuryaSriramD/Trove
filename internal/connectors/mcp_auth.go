package connectors

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// MCPOAuthState is private credential-store data, never an application contract.
// Config can contain a dynamically registered client secret. The registration
// response and refresh token remain outside workspace SQLite and backups.
type MCPOAuthState struct {
	Config   oauth2.Config
	Resource string
	GrantID  string
}

func (MCPOAuthState) String() string   { return "MCP authorization state (redacted)" }
func (MCPOAuthState) GoString() string { return "MCP authorization state (redacted)" }

type mcpOAuthTransport struct {
	next               http.RoundTripper
	tokenURL, resource string
}

func (t mcpOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := ValidateEndpoint(r.URL.String()); err != nil {
		return nil, err
	}
	if t.tokenURL != "" {
		if r.URL.String() != t.tokenURL || r.Method != "POST" {
			return nil, fail("INVALID_ENDPOINT", "MCP refresh endpoint changed")
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		r.Body.Close()
		if err != nil {
			return nil, fail("AUTH_REJECTED", "Invalid MCP token request")
		}
		values, err := url.ParseQuery(string(b))
		if err != nil {
			return nil, fail("AUTH_REJECTED", "Invalid MCP token request")
		}
		values.Set("resource", t.resource)
		r = r.Clone(r.Context())
		body := values.Encode()
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = nil
	}
	resp, err := t.next.RoundTrip(r)
	if resp != nil && resp.Body != nil {
		resp.Body = &mcpBoundedBody{ReadCloser: resp.Body, remaining: 128 << 10}
	}
	return resp, err
}
func mcpOAuthClient(base *http.Client, tokenURL, resource string) *http.Client {
	if base == nil {
		base = NewHTTPClient()
	}
	c := *base
	next := base.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	c.Transport = mcpOAuthTransport{next: next, tokenURL: tokenURL, resource: resource}
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fail("REDIRECT_DENIED", "Authorization endpoint redirects are not permitted")
	}
	return &c
}

func (b *Broker) BeginMCP(ctx context.Context, workspaceID, workspacePath, accountID string, r Registration, openURL func(string) error) (AuthorizationAttempt, error) {
	if (r.Kind != MCP && r.Kind != Notion) || !filepath.IsAbs(workspacePath) || filepath.Clean(workspacePath) != workspacePath || workspaceID == "" || accountID == "" || openURL == nil || len(r.ClientID) > 512 || strings.ContainsAny(r.ClientID, "\r\n\x00") {
		return AuthorizationAttempt{}, fail("INVALID_REQUEST", "Invalid MCP authorization registration")
	}
	if r.Kind == Notion && r.Endpoint != "https://mcp.notion.com/mcp" {
		return AuthorizationAttempt{}, fail("INVALID_ENDPOINT", "Notion authorization requires its hosted MCP endpoint")
	}
	if err := validateMCPEndpoint(r.Endpoint); err != nil {
		return AuthorizationAttempt{}, err
	}
	if r.DynamicClientRegistration && r.ClientID != "" {
		return AuthorizationAttempt{}, fail("INVALID_REQUEST", "Choose either a registered client ID or explicit dynamic registration")
	}
	if !r.DynamicClientRegistration && strings.TrimSpace(r.ClientID) == "" {
		return AuthorizationAttempt{}, fail("REGISTRATION_REQUIRED", "Configure a native client ID or explicitly enable dynamic client registration")
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return AuthorizationAttempt{}, fail("CLOSED", "Connector broker is closed")
	}
	pending := 0
	for _, a := range b.attempts {
		if a.public.State == "waiting" {
			pending++
		}
	}
	if pending >= 4 {
		b.mu.Unlock()
		return AuthorizationAttempt{}, fail("RESOURCE_EXHAUSTED", "Complete or cancel an existing authorization attempt")
	}
	flow, cancel := context.WithTimeout(ctx, 10*time.Minute)
	id := uuid.NewString()
	a := &authAttempt{public: AuthorizationAttempt{ID: id, Kind: r.Kind, State: "waiting", Message: "Authorize the exact MCP server in your browser; resources and tools will be selected separately", ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)}, registration: r, workspaceID: workspaceID, workspacePath: workspacePath, accountID: accountID, cancel: cancel}
	b.attempts[id] = a
	b.mu.Unlock()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		b.finish(id, "failed", "Local authorization callback is unavailable", nil, nil)
		return AuthorizationAttempt{}, fail("CALLBACK_UNAVAILABLE", "Local authorization callback is unavailable")
	}
	a.redirect = "http://" + listener.Addr().String() + "/callback"
	result := make(chan *mcpauth.AuthorizationResult, 1)
	var callbackMu sync.Mutex
	expectedState := ""
	claimed := false
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	b.mu.Lock()
	if b.closed || a.public.State != "waiting" {
		b.mu.Unlock()
		listener.Close()
		cancel()
		return AuthorizationAttempt{}, fail("CANCELLED", "Authorization was cancelled")
	}
	a.server = server
	b.mu.Unlock()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		callbackMu.Lock()
		defer callbackMu.Unlock()
		if req.Method != "GET" || req.Host != listener.Addr().String() || len(req.URL.RawQuery) > 32768 || expectedState == "" || subtle.ConstantTimeCompare([]byte(req.URL.Query().Get("state")), []byte(expectedState)) != 1 {
			http.Error(w, "Invalid authorization response", 400)
			return
		}
		if claimed || flow.Err() != nil {
			http.Error(w, "Authorization response already handled or expired", 409)
			return
		}
		claimed = true
		q := req.URL.Query()
		if q.Get("error") != "" || q.Get("code") == "" {
			b.finish(id, "failed", "Provider did not authorize the connection", nil, nil)
			cancel()
			io.WriteString(w, "Authorization was not completed. Return to Trove.")
			return
		}
		result <- &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
		io.WriteString(w, "Return to Trove to finish connecting the selected server.")
	})
	go func() { _ = server.Serve(listener) }()
	go func() { <-flow.Done(); _ = server.Close() }()
	var credentialMu sync.Mutex
	var credential *protectedCredential
	resource := ""
	config := &mcpauth.AuthorizationCodeHandlerConfig{RedirectURL: a.redirect, RequestRefreshToken: true, Client: mcpOAuthClient(b.client, "", ""), AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		if err := ValidateEndpoint(args.URL); err != nil {
			return nil, err
		}
		u, err := url.Parse(args.URL)
		if err != nil {
			return nil, fail("AUTH_REJECTED", "Authorization URL is invalid")
		}
		q := u.Query()
		if q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != a.redirect {
			return nil, fail("AUTH_REJECTED", "MCP native authorization binding is invalid")
		}
		resource = q.Get("resource")
		if err = ValidateEndpoint(resource); err != nil {
			return nil, err
		}
		callbackMu.Lock()
		expectedState = q.Get("state")
		callbackMu.Unlock()
		if err = openURL(args.URL); err != nil {
			return nil, fail("BROWSER_UNAVAILABLE", "Could not open the system browser")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-result:
			return res, nil
		}
	}, NewTokenSource: func(_ context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
		if token.AccessToken == "" || !strings.EqualFold(token.Type(), "Bearer") || !token.Valid() {
			return nil, fail("AUTH_REJECTED", "MCP provider did not issue a usable bearer token")
		}
		if err := ValidateEndpoint(cfg.Endpoint.TokenURL); err != nil {
			return nil, err
		}
		u, _ := url.Parse(r.Endpoint)
		grant := uuid.NewString()
		copyConfig := *cfg
		copyConfig.Scopes = append([]string{}, cfg.Scopes...)
		c := &protectedCredential{Version: 1, WorkspaceID: workspaceID, WorkspacePath: workspacePath, AccountID: accountID, Registration: r, Identity: Identity{Subject: grant, Label: "MCP authorization at " + u.Host, Provider: "mcp:" + r.Endpoint}, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: token.Expiry, Scopes: strings.Join(cfg.Scopes, " "), MCP: &MCPOAuthState{Config: copyConfig, Resource: resource, GrantID: grant}}
		credentialMu.Lock()
		credential = c
		credentialMu.Unlock()
		// The broker owns refresh and persistence after this short-lived negotiation.
		return oauth2.StaticTokenSource(token), nil
	}}
	if r.DynamicClientRegistration {
		config.DynamicClientRegistrationConfig = &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{RedirectURIs: []string{a.redirect}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, ClientName: "Trove", ApplicationType: "native"}}
	} else {
		config.PreregisteredClient = &oauthex.ClientCredentials{ClientID: r.ClientID}
	}
	handler, err := mcpauth.NewAuthorizationCodeHandler(config)
	if err != nil {
		cancel()
		b.finish(id, "failed", "MCP authorization registration is invalid", nil, nil)
		return AuthorizationAttempt{}, fail("REGISTRATION_REQUIRED", "MCP authorization registration is invalid")
	}
	go func() {
		defer cancel()
		defer server.Close()
		cs, err := connectMCP(flow, Session{Account: Account{Kind: r.Kind, Endpoint: r.Endpoint}, Client: b.client}, handler)
		if cs != nil {
			defer cs.Close()
		}
		if err != nil {
			state := "failed"
			if flow.Err() != nil {
				state = "expired"
			}
			b.finish(id, state, "MCP authorization did not complete; reconnect explicitly", nil, nil)
			return
		}
		credentialMu.Lock()
		c := credential
		credentialMu.Unlock()
		if c == nil {
			b.finish(id, "failed", "Server did not negotiate an authorization grant", nil, nil)
			return
		}
		b.finish(id, "authorized", "Authorization received; select resources and tools explicitly", c, nil)
	}()
	return b.Attempt(id)
}

// Called by Broker.Session while tokenMu is held, after workspace/account
// binding checks. A rejected refresh never starts an interactive OAuth flow.
func (b *Broker) mcpSession(ctx context.Context, c *protectedCredential, a Account) (Session, error) {
	state := c.MCP
	if state == nil || state.GrantID == "" || state.GrantID != c.Identity.Subject || c.Identity.Provider != "mcp:"+a.Endpoint || ValidateEndpoint(state.Resource) != nil || ValidateEndpoint(state.Config.Endpoint.TokenURL) != nil {
		return Session{}, fail("AUTH_REQUIRED", "Stored MCP authorization is invalid; reconnect explicitly")
	}
	if !c.ExpiresAt.IsZero() && time.Until(c.ExpiresAt) < time.Minute {
		if c.RefreshToken == "" {
			return Session{}, fail("AUTH_REQUIRED", "MCP authorization expired; reconnect explicitly")
		}
		token := &oauth2.Token{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, TokenType: "Bearer", Expiry: time.Now().Add(-time.Minute)}
		client := mcpOAuthClient(b.client, state.Config.Endpoint.TokenURL, state.Resource)
		tokenCtx := context.WithValue(ctx, oauth2.HTTPClient, client)
		fresh, err := state.Config.TokenSource(tokenCtx, token).Token()
		if err != nil || fresh == nil || !fresh.Valid() || !strings.EqualFold(fresh.Type(), "Bearer") {
			return Session{}, fail("AUTH_REQUIRED", "MCP refresh was rejected or interrupted; reconnect explicitly")
		}
		c.AccessToken = fresh.AccessToken
		c.RefreshToken = fresh.RefreshToken
		c.ExpiresAt = fresh.Expiry
		data, _ := json.Marshal(c)
		if err = b.secrets.Put(ctx, a.RegistrationID, data); err != nil {
			return Session{}, fail("CREDENTIAL_STORE_FAILED", "Refreshed MCP credentials could not be stored")
		}
	}
	return Session{Account: a, Token: Secret{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, ExpiresAt: c.ExpiresAt}, Client: b.client}, nil
}
