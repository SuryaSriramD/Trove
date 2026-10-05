package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStore struct {
	mu         sync.Mutex
	data       map[string][]byte
	failPut    bool
	failDelete bool
}

func (s *memoryStore) Get(_ context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[id]
	if !ok {
		return nil, ErrSecretMissing
	}
	return append([]byte(nil), b...), nil
}
func (s *memoryStore) Put(_ context.Context, id string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failPut {
		return errors.New("synthetic store failure")
	}
	s.data[id] = append([]byte(nil), b...)
	return nil
}
func (s *memoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return errors.New("synthetic delete failure")
	}
	delete(s.data, id)
	return nil
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t                             *testing.T
	key                           *rsa.PrivateKey
	store                         *memoryStore
	manager                       *Manager
	clock                         atomic.Int64
	mu                            sync.Mutex
	auth                          url.Values
	root                          string
	scopes                        string
	mode                          string
	refreshError                  string
	refreshes, exchanges, revokes int
	revokeFailures                int
	blocked                       chan struct{}
	release                       chan struct{}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := os.MkdirTemp(base, "trove-auth-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(temporary) })
	root := filepath.Join(temporary, "accounts")
	f := &fixture{t: t, key: key, root: root, store: &memoryStore{data: map[string][]byte{}}, scopes: scope}
	f.clock.Store(time.Now().Unix())
	m, err := newManager(root, f.store, roundTrip(f.transport), func() time.Time { return time.Unix(f.clock.Load(), 0) })
	if err != nil {
		t.Fatal(err)
	}
	f.manager = m
	t.Cleanup(func() { m.Close() })
	return f
}
func response(r *http.Request, status int, value any) *http.Response {
	b, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}
}
func (f *fixture) jwt(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "fixture"})
	b, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
func (f *fixture) transport(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Host != "auth.openai.com" {
		f.t.Errorf("unexpected host %s", r.URL.Host)
	}
	if r.Header.Get("Authorization") != "" {
		f.t.Error("unexpected Authorization header")
	}
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		return response(r, 200, map[string]any{"issuer": issuer, "authorization_endpoint": authorizeEndpoint, "token_endpoint": tokenEndpoint, "jwks_uri": issuer + "/.well-known/jwks.json", "revocation_endpoint": issuer + "/api/accounts/oauth/revoke", "id_token_signing_alg_values_supported": []string{"RS256"}}), nil
	case "/.well-known/jwks.json":
		return response(r, 200, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}}), nil
	case "/api/accounts/oauth/token":
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Error("wrong token protocol")
		}
		r.ParseForm()
		if r.Form.Get("resource") != resource || r.Form.Get("client_secret") != "" || r.Form.Get("client_id") == dynamicClient {
			f.t.Error("wrong client/resource")
		}
		refresh := r.Form.Get("grant_type") == "refresh_token"
		if refresh {
			f.refreshes++
			if r.Form.Has("scope") {
				f.t.Error("refresh changed scope")
			}
			if f.refreshError != "" {
				status := 400
				if f.refreshError == "temporary" {
					status = 503
				}
				return response(r, status, map[string]any{"error": f.refreshError}), nil
			}
		} else {
			f.exchanges++
			if r.Form.Get("redirect_uri") != f.auth.Get("redirect_uri") {
				f.t.Error("callback URI changed")
			}
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != f.auth.Get("code_challenge") {
				f.t.Error("PKCE mismatch")
			}
			if f.mode == "invalid_grant" {
				return response(r, 400, map[string]any{"error": "invalid_grant"}), nil
			}
			if f.blocked != nil {
				close(f.blocked)
				release := f.release
				f.mu.Unlock()
				<-release
				f.mu.Lock()
			}
		}
		claims := map[string]any{"iss": issuer, "aud": r.Form.Get("client_id"), "sub": "subject-one", "nonce": f.auth.Get("nonce"), "exp": f.clock.Load() + 3600, "iat": f.clock.Load(), "email": "same@example.test", "name": "Fixture Person"}
		switch f.mode {
		case "issuer":
			claims["iss"] = "https://other.invalid"
		case "audience":
			claims["aud"] = "other"
		case "nonce":
			claims["nonce"] = "other"
		case "expired":
			claims["exp"] = f.clock.Load() - 1
		case "subject":
			claims["sub"] = "different-subject"
		case "azp":
			claims["aud"] = []string{r.Form.Get("client_id"), "other"}
			claims["azp"] = "other"
		}
		id := f.jwt(claims)
		if f.mode == "signature" {
			parts := strings.Split(id, ".")
			parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
			id = strings.Join(parts, ".")
		}
		result := map[string]any{"access_token": "SYNTHETIC_ACCESS_" + fmt.Sprint(f.refreshes), "refresh_token": "SYNTHETIC_REFRESH_" + fmt.Sprint(f.refreshes), "token_type": "Bearer", "expires_in": 3600, "id_token": id}
		if !refresh {
			result["scope"] = f.scopes
		}
		return response(r, 200, result), nil
	case "/api/accounts/oauth/revoke":
		f.revokes++
		r.ParseForm()
		if r.Form.Get("token_type_hint") != "refresh_token" || r.Form.Get("token") == "" || r.Form.Get("client_id") == dynamicClient {
			f.t.Error("invalid revoke form")
		}
		if f.revokes <= f.revokeFailures {
			return response(r, 503, map[string]any{}), nil
		}
		return response(r, 200, map[string]any{}), nil
	}
	f.t.Errorf("unexpected path %s", r.URL.Path)
	return response(r, 404, nil), nil
}
func (f *fixture) open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Host != "auth.openai.com" || u.Path != "/api/accounts/authorize" {
		f.t.Fatal("untrusted browser URL")
	}
	f.mu.Lock()
	f.auth = u.Query()
	f.mu.Unlock()
	return nil
}
func (f *fixture) begin(id string) Attempt {
	f.t.Helper()
	a, err := f.manager.Begin(context.Background(), id, f.open)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}
func (f *fixture) callback(a Attempt, client string, modify func(url.Values)) *http.Response {
	f.t.Helper()
	f.mu.Lock()
	target := f.auth.Get("redirect_uri")
	state := f.auth.Get("state")
	f.mu.Unlock()
	q := url.Values{"state": {state}, "code": {"SYNTHETIC_CODE"}}
	if client != "" {
		q.Set("client_id", client)
	}
	if modify != nil {
		modify(q)
	}
	resp, err := http.Get(target + "?" + q.Encode())
	if err != nil {
		f.t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}
func (f *fixture) signIn(client string) Registration {
	f.t.Helper()
	a := f.begin("")
	f.callback(a, client, nil)
	got, err := f.manager.Attempt(a.ID)
	if err != nil || got.State != "completed" {
		f.t.Fatalf("sign-in: %+v %v", got, err)
	}
	list, _ := f.manager.List()
	for _, r := range list {
		if r.ID == got.RegistrationID {
			return r
		}
	}
	f.t.Fatal("registration absent")
	return Registration{}
}
func errorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSIWCRegistrationPKCEStatePersistenceAndRedaction(t *testing.T) {
	f := newFixture(t)
	a := f.begin("")
	if f.auth.Get("client_id") != dynamicClient || f.auth.Get("agent_name_hint") != "Trove" || f.auth.Get("scope") != scope || f.auth.Get("response_type") != "code" || f.auth.Get("code_challenge_method") != "S256" || !strings.HasPrefix(f.auth.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(f.auth.Get("redirect_uri"), "/auth/callback") {
		t.Fatal("wrong authorization contract")
	}
	host := f.auth.Get("ext_agent_host_id")
	if !strings.HasPrefix(host, "urn:uuid:") {
		t.Fatal("host identity not persistent UUID URI")
	}
	f.callback(a, "oaiapp_one", func(q url.Values) { q.Set("state", "wrong") })
	if f.exchanges != 0 {
		t.Fatal("wrong state exchanged code")
	}
	f.callback(a, "oaiapp_one", nil)
	got, _ := f.manager.Attempt(a.ID)
	if got.State != "completed" {
		t.Fatal(got)
	}
	regs, _ := f.manager.List()
	r := regs[0]
	if r.Identity.ClientID != "oaiapp_one" || r.Identity.Subject != "subject-one" || !r.PlanAuthorized {
		t.Fatal(r)
	}
	token, err := f.manager.AccessToken(context.Background(), r.ID)
	if err != nil || token.Value != "SYNTHETIC_ACCESS_0" {
		t.Fatal(token, err)
	}
	for _, v := range []any{regs, got, token} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "SYNTHETIC_") || strings.Contains(string(b), "code_challenge") {
			t.Fatal("secret exposed")
		}
	}
	files, _ := os.ReadDir(f.root)
	for _, entry := range files {
		data, _ := os.ReadFile(filepath.Join(f.root, entry.Name()))
		if strings.Contains(string(data), "SYNTHETIC_") || strings.Contains(string(data), "idToken") {
			t.Fatal("tokens persisted outside secret store")
		}
	}
	if _, err := newManager(f.root, f.store, roundTrip(f.transport), time.Now); errorCode(err) != "AUTH_BUSY" {
		t.Fatalf("second manager: %v", err)
	}
	f.manager.Close()
	m, err := newManager(f.root, f.store, roundTrip(f.transport), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.meta.HostID != host {
		t.Fatal("host changed after reopen")
	}
	token, err = m.AccessToken(context.Background(), r.ID)
	if err != nil || token.Value == "" {
		t.Fatal(err)
	}
}
func TestSIWCRejectsUnverifiedIDTokens(t *testing.T) {
	for _, mode := range []string{"issuer", "audience", "nonce", "expired", "signature", "azp"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.mode = mode
			a := f.begin("")
			f.callback(a, "oaiapp_one", nil)
			got, _ := f.manager.Attempt(a.ID)
			list, _ := f.manager.List()
			if got.State != "failed" || len(list) != 0 || len(f.store.data) != 0 {
				t.Fatal("invalid identity became usable", got, list)
			}
		})
	}
}
func TestSIWCAccountsStaySeparateAndReturningIdentityCannotChange(t *testing.T) {
	f := newFixture(t)
	one := f.signIn("oaiapp_one")
	two := f.signIn("oaiapp_two")
	if one.ID == two.ID || one.Identity.Email != two.Identity.Email {
		t.Fatal("bad fixture identity")
	}
	before, _ := f.store.Get(context.Background(), one.ID)
	f.mode = "subject"
	a := f.begin(one.ID)
	if f.auth.Get("client_id") != "oaiapp_one" || f.auth.Has("agent_name_hint") || f.auth.Get("id_token_hint") == "" {
		t.Fatal("returning registration contract violated")
	}
	f.callback(a, "", nil)
	after, _ := f.store.Get(context.Background(), one.ID)
	if string(before) != string(after) {
		t.Fatal("different identity replaced account")
	}
	f.mode = ""
	a = f.begin(one.ID)
	f.callback(a, "oaiapp_two", nil)
	after, _ = f.store.Get(context.Background(), one.ID)
	if string(before) != string(after) {
		t.Fatal("different callback client replaced account")
	}
	result, err := f.manager.SignOut(context.Background(), one.ID)
	if err != nil || !result.LocalCleared || !result.RemoteRevoked {
		t.Fatal(result, err)
	}
	if _, err = f.store.Get(context.Background(), two.ID); err != nil {
		t.Fatal("signout removed other account")
	}
	a = f.begin(one.ID)
	if f.auth.Get("client_id") != "oaiapp_one" || f.auth.Has("id_token_hint") {
		t.Fatal("signout lost registration or retained hint")
	}
	f.manager.Cancel(a.ID)
}
func TestSIWCRefreshSerializesAndRetainsScope(t *testing.T) {
	f := newFixture(t)
	r := f.signIn("oaiapp_one")
	f.clock.Add(3570)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := f.manager.AccessToken(context.Background(), r.ID)
			if err != nil || token.Value != "SYNTHETIC_ACCESS_1" {
				t.Errorf("refresh result: %v %s", err, token.Value)
			}
		}()
	}
	wg.Wait()
	if f.refreshes != 1 {
		t.Fatalf("refresh raced: %d", f.refreshes)
	}
	data, _ := f.store.Get(context.Background(), r.ID)
	var c credentials
	json.Unmarshal(data, &c)
	if c.RefreshToken != "SYNTHETIC_REFRESH_1" || c.Scope != scope {
		t.Fatal("replacement not stored atomically")
	}
}
func TestSIWCRefreshFailureRecovery(t *testing.T) {
	for _, mode := range []string{"temporary", "invalid_grant", "invalid_client"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			r := f.signIn("oaiapp_one")
			f.clock.Add(3601)
			f.refreshError = mode
			_, err := f.manager.AccessToken(context.Background(), r.ID)
			if err == nil {
				t.Fatal("failed refresh allowed token")
			}
			_, stored := f.store.Get(context.Background(), r.ID)
			if mode == "invalid_grant" {
				if !errors.Is(stored, ErrSecretMissing) || errorCode(err) != "AUTH_REAUTHORIZE" {
					t.Fatal(err, stored)
				}
			} else if stored != nil {
				t.Fatal("temporary/configuration failure erased tokens")
			}
		})
	}
}
func TestSIWCPlanPermissionAndSecureStoreFailures(t *testing.T) {
	f := newFixture(t)
	f.scopes = "openid email offline_access"
	r := f.signIn("oaiapp_one")
	if r.PlanAuthorized {
		t.Fatal("identity implies plan permission")
	}
	_, err := f.manager.AccessToken(context.Background(), r.ID)
	if errorCode(err) != "AUTH_PLAN_DISABLED" {
		t.Fatal(err)
	}
	a := f.begin(r.ID)
	if f.auth.Get("prompt") != "consent" {
		t.Fatal("explicit enable plan did not request consent")
	}
	f.manager.Cancel(a.ID)
	f.store.failPut = true
	f.scopes = scope
	a = f.begin("")
	f.callback(a, "oaiapp_two", nil)
	got, _ := f.manager.Attempt(a.ID)
	if got.State != "failed" {
		t.Fatal("unprotected credentials accepted")
	}
}
func TestSIWCCancelDuringExchangeCannotSaveTokens(t *testing.T) {
	f := newFixture(t)
	f.blocked = make(chan struct{})
	f.release = make(chan struct{})
	a := f.begin("")
	done := make(chan struct{})
	go func() {
		f.mu.Lock()
		target := f.auth.Get("redirect_uri")
		state := f.auth.Get("state")
		f.mu.Unlock()
		resp, err := http.Get(target + "?" + url.Values{"state": {state}, "code": {"code"}, "client_id": {"oaiapp_one"}}.Encode())
		if err == nil {
			resp.Body.Close()
		}
		close(done)
	}()
	<-f.blocked
	if err := f.manager.Cancel(a.ID); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	<-done
	got, _ := f.manager.Attempt(a.ID)
	list, _ := f.manager.List()
	if got.State != "cancelled" || len(list) != 0 {
		t.Fatal("cancelled exchange saved account", got, list)
	}
}
func TestSIWCInvalidGrantRetryReusesIssuedClient(t *testing.T) {
	f := newFixture(t)
	f.mode = "invalid_grant"
	a := f.begin("")
	f.callback(a, "oaiapp_pending", nil)
	got, _ := f.manager.Attempt(a.ID)
	if got.State != "retry_required" {
		t.Fatal(got)
	}
	f.mode = ""
	next, err := f.manager.Retry(context.Background(), a.ID, f.open)
	if err != nil {
		t.Fatal(err)
	}
	if f.auth.Get("client_id") != "oaiapp_pending" || f.auth.Has("agent_name_hint") {
		t.Fatal("retry lost issued client")
	}
	f.callback(next, "", nil)
	got, _ = f.manager.Attempt(next.ID)
	if got.State != "completed" {
		t.Fatal(got)
	}
}
func TestSIWCRevocationRetriesAndReportsUnconfirmed(t *testing.T) {
	for _, failures := range []int{1, 3} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) {
			f := newFixture(t)
			r := f.signIn("oaiapp_one")
			f.revokeFailures = failures
			out, err := f.manager.SignOut(context.Background(), r.ID)
			if err != nil || !out.LocalCleared || out.RemoteRevoked != (failures == 1) || f.revokes != 2 {
				t.Fatal(out, err, f.revokes)
			}
			if _, err = f.manager.AccessToken(context.Background(), r.ID); errorCode(err) != "AUTH_REAUTHORIZE" {
				t.Fatal(err)
			}
		})
	}
}
func TestSIWCPrivateStorageAndEndpointBoundaries(t *testing.T) {
	f := newFixture(t)
	outside := filepath.Join(f.root, "public")
	os.Mkdir(outside, 0755)
	if runtime.GOOS != "windows" {
		if m, err := newManager(outside, f.store, roundTrip(f.transport), time.Now); err == nil {
			m.Close()
			t.Fatal("public storage accepted")
		}
	}
	link := filepath.Join(f.root, "link")
	if err := os.Symlink(f.root, link); err == nil {
		if m, err := newManager(link, f.store, roundTrip(f.transport), time.Now); err == nil {
			m.Close()
			t.Fatal("symlink storage accepted")
		}
	} else {
		t.Log("Symlink creation unavailable; native reparse-point qualification remains required:", err)
	}
	req, _ := http.NewRequest("GET", "https://other.invalid/.well-known/jwks.json", nil)
	if _, err := f.manager.client.Do(req); err == nil {
		t.Fatal("untrusted authentication endpoint allowed")
	}
}

func TestSIWCFailedCredentialCleanupIsRetryableAndCannotAuthorize(t *testing.T) {
	f := newFixture(t)
	r := f.signIn("oaiapp_one")
	f.store.failDelete = true
	out, err := f.manager.SignOut(context.Background(), r.ID)
	if err == nil || out.LocalCleared {
		t.Fatal("failed cleanup reported success")
	}
	list, _ := f.manager.List()
	if list[0].State != "sign_out_incomplete" {
		t.Fatal(list)
	}
	if _, err := f.manager.AccessToken(context.Background(), r.ID); errorCode(err) != "AUTH_REAUTHORIZE" {
		t.Fatal(err)
	}
	if _, err := f.manager.Begin(context.Background(), r.ID, f.open); errorCode(err) != "CREDENTIAL_STORE_FAILED" {
		t.Fatal(err)
	}
	f.store.failDelete = false
	out, err = f.manager.SignOut(context.Background(), r.ID)
	if err != nil || !out.LocalCleared {
		t.Fatal(out, err)
	}
}
func TestSIWCCallbackRejectsDuplicateFieldsAndHandlesDeniedConsent(t *testing.T) {
	f := newFixture(t)
	a := f.begin("")
	f.callback(a, "oaiapp_one", func(q url.Values) { q.Add("state", q.Get("state")) })
	if f.exchanges != 0 {
		t.Fatal("duplicate callback exchanged code")
	}
	f.callback(a, "oaiapp_one", func(q url.Values) { q.Set("error", "access_denied") })
	got, _ := f.manager.Attempt(a.ID)
	if got.State != "failed" || f.exchanges != 0 {
		t.Fatal(got)
	}
}
