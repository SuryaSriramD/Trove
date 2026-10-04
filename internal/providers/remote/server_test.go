package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

func certificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic TLS fixture"}, DNSNames: []string{"trove.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

type fixture struct {
	s      *Server
	c      Connection
	client *http.Client
	live   atomic.Bool
	clock  atomic.Int64
	calls  atomic.Int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.live.Store(true)
	f.clock.Store(time.Now().UnixNano())
	sdk := mcp.NewServer(&mcp.Implementation{Name: "remote-test", Version: "1"}, nil)
	sdk.AddTool(&mcp.Tool{Name: "readPage", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"pageId": map[string]any{"type": "string"}}, "required": []string{"pageId"}}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Selected fixture page"}}}, nil
	})
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdk }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	cert := certificate(t)
	callbacks := Callbacks{RecordDecision: func(context.Context, string, string, string, bool) error { return nil }, Forward: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal-only" || r.Header.Get("Trove-Connection-Id") != "connection" {
			t.Error("missing internal bridge substitution")
		}
		sdkHandler.ServeHTTP(w, r)
	}), Validate: func(context.Context, string, string) error {
		if !f.live.Load() {
			return errInvalid
		}
		return nil
	}, Disconnect: func(context.Context, string) error { f.live.Store(false); return nil }}
	var err error
	f.s, err = newServer(Config{PublicOrigin: "https://trove.example.test", BindAddress: "127.0.0.1:0", Certificate: cert, AllowedOrigins: []string{"https://allowed.example.test"}}, callbacks, func() time.Time { return time.Unix(0, f.clock.Load()) }, func(ctx context.Context, id string) (clientMetadata, error) {
		if id != "https://client.example.test/oauth.json" {
			return clientMetadata{}, errMetadata
		}
		return clientMetadata{ID: id, Name: "Untrusted client label", RedirectURIs: []string{"https://client.example.test/callback"}, AuthMethods: []string{"private_key_jwt", "none"}, GrantTypes: []string{"authorization_code", "refresh_token"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.s.Close() })
	f.s.limiter = rate.NewLimiter(rate.Inf, 1)
	f.s.registrationLimiter = rate.NewLimiter(rate.Inf, 1)
	f.c, err = f.s.AddConnection(context.Background(), Binding{ID: "connection", Token: "internal-only", PageIDs: []string{"selected-page"}, Tools: []string{"readPage"}})
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	pool.AddCert(leaf)
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "trove.example.test:443" {
			return nil, errors.New("fixture forbids external networking")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", f.s.listener.Addr().String())
	}}
	t.Cleanup(tr.CloseIdleConnections)
	f.client = &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}
func request(t *testing.T, f *fixture, method, path, body string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	r, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, string(b)
}
func (f *fixture) consent(t *testing.T, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	browser := *f.client
	browser.Jar = jar
	resp, err := browser.Get(args.URL)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("authorize: %d", resp.StatusCode)
	}
	reviewURL := f.s.config.PublicOrigin + resp.Header.Get("Location")
	resp, err = browser.Get(reviewURL)
	if err != nil {
		return nil, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	status := f.s.Status()
	if len(status.Authorizations) != 1 || status.Connections[0].State != "awaiting_client" {
		t.Fatal("authorization auto-granted context", status)
	}
	pending := status.Authorizations[0]
	if !strings.Contains(string(data), pending.ComparisonCode) || pending.RedirectURI != "https://client.example.test/callback" || pending.ConnectionID != f.c.ID {
		t.Fatal("review context mismatch", string(data), pending)
	}
	if err = f.s.Decide(context.Background(), pending.ID, strings.Repeat("0", 64), true); err == nil {
		t.Fatal("stale review hash accepted")
	}
	if err = f.s.Decide(context.Background(), pending.ID, pending.ReviewHash, true); err != nil {
		return nil, err
	}
	resp, err = browser.Get(reviewURL)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	destination, _ := url.Parse(resp.Header.Get("Location"))
	q := destination.Query()
	if resp.StatusCode != 303 || destination.Host != "client.example.test" || q.Get("iss") != f.s.config.PublicOrigin {
		t.Fatal("invalid authorization redirect", destination)
	}
	return &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
}
func TestTLSOfficialSDKDiscoveryPKCEOwnerConsent(t *testing.T) {
	for _, registration := range []string{"cimd", "dcr"} {
		t.Run(registration, func(t *testing.T) {
			f := newFixture(t)
			cfg := &mcpauth.AuthorizationCodeHandlerConfig{Client: f.client, RedirectURL: "https://client.example.test/callback", RequestRefreshToken: true, AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
				return f.consent(t, args)
			}}
			if registration == "cimd" {
				cfg.ClientIDMetadataDocumentConfig = &mcpauth.ClientIDMetadataDocumentConfig{URL: "https://client.example.test/oauth.json"}
			} else {
				cfg.DynamicClientRegistrationConfig = &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "DCR fixture", RedirectURIs: []string{cfg.RedirectURL}, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none"}}
			}
			initialRefresh := ""
			cfg.NewTokenSource = func(ctx context.Context, config *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
				initialRefresh = token.RefreshToken
				expired := *token
				expired.Expiry = time.Now().Add(-time.Hour)
				return config.TokenSource(ctx, &expired), nil
			}
			auth, err := mcpauth.NewAuthorizationCodeHandler(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			sdk := mcp.NewClient(&mcp.Implementation{Name: "official-remote-client", Version: "1.8.0"}, nil)
			session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.c.URL, HTTPClient: f.client, OAuthHandler: auth}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			tools, err := session.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 1 {
				t.Fatal(tools, err)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "readPage", Arguments: map[string]any{"pageId": "selected-page"}})
			if err != nil || result.IsError || result.Content[0].(*mcp.TextContent).Text != "Selected fixture page" {
				t.Fatal(result, err)
			}
			f.s.mu.Lock()
			initial := f.s.refresh[sha256.Sum256([]byte(initialRefresh))]
			automaticallyRefreshed := initial != nil && initial.used
			f.s.mu.Unlock()
			if !automaticallyRefreshed {
				t.Fatal("official SDK token source did not exercise automatic refresh")
			}
			ts, err := auth.TokenSource(ctx)
			if err != nil {
				t.Fatal(err)
			}
			token, err := ts.Token()
			if err != nil {
				t.Fatal(err)
			}
			status, _ := json.Marshal(f.s.Status())
			if strings.Contains(string(status), token.AccessToken) || strings.Contains(string(status), token.RefreshToken) || strings.Contains(string(status), "internal-only") {
				t.Fatal("status leaked a secret")
			}
			clientID := f.s.Status().Connections[0].ClientID
			fields := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "resource": {f.c.URL}, "refresh_token": {token.RefreshToken}}
			fields.Set("resource", f.s.config.PublicOrigin+"/mcp/wrong")
			wrong, _, _ := request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
			if wrong != 400 {
				t.Fatal("refresh audience widened")
			}
			fields.Set("resource", f.c.URL)
			code, _, body := request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
			if code != 200 {
				t.Fatal(code, body)
			}
			if registration == "cimd" {
				// Rotation replay revokes the underlying canonical connection.
				code, _, _ = request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
				if code != 400 || f.live.Load() {
					t.Fatal("refresh reuse retained canonical grant", code)
				}
			} else {
				var rotated map[string]any
				json.Unmarshal([]byte(body), &rotated)
				fields = url.Values{"client_id": {clientID}, "token": {rotated["refresh_token"].(string)}}
				code, _, _ = request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/revoke", fields.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
				if code != 200 || f.live.Load() {
					t.Fatal("explicit revocation retained grant", code)
				}
			}

			if _, err = session.ListTools(ctx, nil); err == nil {
				t.Fatal("revoked client retained access")
			}
		})
	}
}
func TestRemoteBoundaryPKCEReplayIsolationAndDenial(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	clientID := "https://client.example.test/oauth.json"
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {"https://client.example.test/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {"expected-state"}, "resource": {f.c.URL}}
	q.Set("redirect_uri", "https://attacker.example.test")
	code, _, _ := request(t, f, "GET", f.s.config.PublicOrigin+"/oauth/authorize?"+q.Encode(), "", nil)
	if code != 400 || len(f.s.Status().Authorizations) != 0 {
		t.Fatal("unregistered redirect accepted")
	}
	q.Set("redirect_uri", "https://client.example.test/callback")
	authorized, err := f.consent(t, &mcpauth.AuthorizationArgs{URL: f.s.config.PublicOrigin + "/oauth/authorize?" + q.Encode()})
	if err != nil {
		t.Fatal(err)
	}
	fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "resource": {f.c.URL}, "code": {authorized.Code}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {verifier}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	code, _, body := request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), headers)
	if code != 200 {
		t.Fatal(code, body)
	}
	var tok map[string]any
	json.Unmarshal([]byte(body), &tok)
	other, err := f.s.AddConnection(ctx, Binding{ID: "other", Token: "internal-only", PageIDs: []string{"other-page"}, Tools: []string{"readPage"}})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ = request(t, f, "POST", other.URL, `{}`, map[string]string{"Authorization": "Bearer " + tok["access_token"].(string)})
	if code != 401 {
		t.Fatal("cross-resource token accepted", code)
	}
	code, _, _ = request(t, f, "GET", f.c.URL+"?access_token="+tok["access_token"].(string), "", nil)
	if code != 400 {
		t.Fatal("query token accepted")
	}
	code, _, _ = request(t, f, "GET", f.c.URL, "", map[string]string{"Origin": "https://attacker.example.test"})
	if code != 403 {
		t.Fatal("unapproved Origin accepted")
	}
	code, _, _ = request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), headers)
	if code != 400 {
		t.Fatal("authorization code replay accepted")
	}
	if f.live.Load() {
		t.Fatal("code replay retained canonical grant")
	}
	fields = url.Values{"client_id": {clientID}, "token": {tok["refresh_token"].(string)}}
	code, _, _ = request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/revoke", fields.Encode(), headers)
	if code != 200 || f.live.Load() {
		t.Fatal("revocation failed")
	}
}
func TestRemoteMetadataAndResourceBounds(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.5", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("SSRF address allowed %s", raw)
		}
	}
	for _, raw := range []string{"https://user:password@client.example/path", "https://client.example/", "http://client.example/doc", "https://client.example/doc?x=1", "https://client.example/doc#x", "https://client.example./doc"} {
		if _, err := httpsURL(raw, true); err == nil {
			t.Errorf("invalid metadata URL allowed %s", raw)
		}
	}
	f := newFixture(t)
	ctx := context.Background()
	for n := 1; n < 32; n++ {
		_, err := f.s.AddConnection(ctx, Binding{ID: string(rune(n)), Token: "token", PageIDs: []string{"page"}, Tools: []string{"readPage"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.AddConnection(ctx, Binding{ID: "overflow", Token: "token", PageIDs: []string{"page"}, Tools: []string{"readPage"}}); err == nil {
		t.Fatal("connection bound absent")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if f.live.Load() || f.s.Status().Enabled {
		t.Fatal("disable left grant active")
	}
}

func TestRemoteWrongPKCEConsumesCodeAndExpiredConsentCannotApprove(t *testing.T) {
	f := newFixture(t)
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {"https://client.example.test/oauth.json"}, "redirect_uri": {"https://client.example.test/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {"state"}, "resource": {f.c.URL}}
	result, err := f.consent(t, &mcpauth.AuthorizationArgs{URL: f.s.config.PublicOrigin + "/oauth/authorize?" + q.Encode()})
	if err != nil {
		t.Fatal(err)
	}
	fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {q.Get("client_id")}, "resource": {f.c.URL}, "code": {result.Code}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {strings.Repeat("x", 43)}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	code, _, _ := request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), headers)
	if code != 400 {
		t.Fatal("wrong PKCE accepted")
	}
	fields.Set("code_verifier", verifier)
	code, _, _ = request(t, f, "POST", f.s.config.PublicOrigin+"/oauth/token", fields.Encode(), headers)
	if code != 400 {
		t.Fatal("failed PKCE code reused")
	}
	f.s.mu.Lock()
	if len(f.s.access) != 0 || len(f.s.refresh) != 0 {
		t.Error("failed PKCE issued tokens")
	}
	f.s.mu.Unlock()
	f = newFixture(t)
	q.Set("resource", f.c.URL)
	code, _, _ = request(t, f, "GET", f.s.config.PublicOrigin+"/oauth/authorize?"+q.Encode(), "", nil)
	if code != 303 {
		t.Fatal(code)
	}
	var pending Authorization
	for _, a := range f.s.Status().Authorizations {
		if a.State == "pending" {
			pending = a
		}
	}
	f.clock.Add(int64(6 * time.Minute))
	if err = f.s.Decide(context.Background(), pending.ID, pending.ReviewHash, true); err == nil {
		t.Fatal("expired consent accepted")
	}
}
func TestRemoteDenialIssuerCookieAndTransportBoundary(t *testing.T) {
	f := newFixture(t)
	sum := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	q := url.Values{"client_id": {"https://client.example.test/oauth.json"}, "redirect_uri": {"https://client.example.test/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {"opaque-state"}, "resource": {f.c.URL}}
	jar, _ := cookiejar.New(nil)
	browser := *f.client
	browser.Jar = jar
	resp, err := browser.Get(f.s.config.PublicOrigin + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	reviewURL := f.s.config.PublicOrigin + resp.Header.Get("Location")
	cookies := resp.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("review cookie is not protected", cookies)
	}
	code, _, _ := request(t, f, "GET", reviewURL, "", nil)
	if code != 400 {
		t.Fatal("review without browser cookie succeeded")
	}
	a := f.s.Status().Authorizations[0]
	if err = f.s.Decide(context.Background(), a.ID, a.ReviewHash, false); err != nil {
		t.Fatal(err)
	}
	resp, err = browser.Get(reviewURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	destination, _ := url.Parse(resp.Header.Get("Location"))
	out := destination.Query()
	if resp.StatusCode != 303 || out.Get("error") != "access_denied" || out.Get("iss") != f.s.config.PublicOrigin || out.Get("state") != "opaque-state" || out.Has("code") {
		t.Fatal("denial redirect mismatch", destination)
	}
	if f.s.Status().Connections[0].State != "awaiting_client" {
		t.Fatal("denial granted a client")
	}
	req, _ := http.NewRequest("GET", f.c.URL, nil)
	req.Host = "attacker.example.test"
	resp, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("mismatching host accepted")
	}
	req, _ = http.NewRequest("GET", f.c.URL, nil)
	rec := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("non-TLS request accepted")
	}
	for _, origin := range []string{"https://allowed.example.test", "https://trove.example.test"} {
		code, headers, _ := request(t, f, "OPTIONS", f.c.URL, "", map[string]string{"Origin": origin})
		if code != 204 || headers.Get("Access-Control-Allow-Origin") != origin {
			t.Fatal("approved CORS origin unavailable", origin)
		}
	}
}

func TestRemoteConfigurationAndFailedAuditStayClosed(t *testing.T) {
	f := newFixture(t)
	for _, mutate := range []func(*Config){func(c *Config) { c.PublicOrigin = "http://trove.example.test" }, func(c *Config) { c.PublicOrigin = "https://other.example.test" }, func(c *Config) { c.PublicOrigin += "/path" }, func(c *Config) { c.AllowedOrigins = []string{"https://client.example.test/path"} }, func(c *Config) { c.BindAddress = "localhost:8443" }, func(c *Config) { c.BindAddress = "127.0.0.1:99999" }, func(c *Config) { c.Certificate = tls.Certificate{} }} {
		config := f.s.config
		mutate(&config)
		if _, err := New(config, f.s.callbacks); err == nil {
			t.Fatal("invalid HTTPS configuration accepted", config.PublicOrigin, config.BindAddress)
		}
	}
	if _, err := newServer(f.s.config, f.s.callbacks, func() time.Time { return time.Now().Add(2 * time.Hour) }, f.s.fetch); err == nil {
		t.Fatal("expired certificate accepted")
	}
	sum := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	q := url.Values{"client_id": {"https://client.example.test/oauth.json"}, "redirect_uri": {"https://client.example.test/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "resource": {f.c.URL}}
	code, _, _ := request(t, f, "GET", f.s.config.PublicOrigin+"/oauth/authorize?"+q.Encode(), "", nil)
	if code != 303 {
		t.Fatal(code)
	}
	a := f.s.Status().Authorizations[0]
	f.s.callbacks.RecordDecision = func(context.Context, string, string, string, bool) error {
		return errors.New("synthetic audit persistence failure")
	}
	if err := f.s.Decide(context.Background(), a.ID, a.ReviewHash, true); err == nil {
		t.Fatal("failed audit silently approved")
	}
	status := f.s.Status()
	if status.Connections[0].State != "awaiting_client" || status.Authorizations[0].State != "pending" {
		t.Fatal("failed audit changed consent", status)
	}
}
