package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type pendingAttempt struct {
	public                                     Attempt
	ctx                                        context.Context
	cancel                                     context.CancelFunc
	server                                     *http.Server
	state, nonce, verifier, redirect, clientID string
	selected                                   Identity
	retryClientID                              string
}
type Manager struct {
	mu       sync.Mutex
	root     string
	lock     *os.File
	secrets  SecretStore
	client   *http.Client
	now      func() time.Time
	meta     metadata
	attempts map[string]*pendingAttempt
	closed   bool
}

// NewManager requires an absolute, private host storage root outside any
// workspace. The desktop adapter chooses this path; the renderer cannot.
func NewManager(storageRoot string) (*Manager, error) {
	secret, err := nativeSecretStore(storageRoot)
	if err != nil {
		return nil, err
	}
	return newManager(storageRoot, secret, http.DefaultTransport, time.Now)
}
func newManager(root string, secret SecretStore, transport http.RoundTripper, now func() time.Time) (*Manager, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || secret == nil || transport == nil || now == nil {
		return nil, fail("STORAGE_FAILED", "Account storage configuration is invalid")
	}
	lock, err := lockRoot(root)
	if err != nil {
		return nil, err
	}
	m := &Manager{root: root, lock: lock, secrets: secret, now: now, attempts: map[string]*pendingAttempt{}, client: &http.Client{Transport: safeTransport{transport}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fail("AUTH_ENDPOINT_INVALID", "Authentication redirects are not allowed")
	}}}
	if err = m.load(); err != nil {
		lock.Close()
		return nil, err
	}
	return m, nil
}
func (m *Manager) List() ([]Registration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fail("AUTH_CLOSED", "Account manager is closed")
	}
	return append([]Registration{}, m.meta.Registrations...), nil
}
func randomSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func equalSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func validClientID(s string) bool {
	if s == "" || s == dynamicClient || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (m *Manager) find(id string) *Registration {
	for i := range m.meta.Registrations {
		if m.meta.Registrations[i].ID == id {
			return &m.meta.Registrations[i]
		}
	}
	return nil
}
func (m *Manager) readCredentials(ctx context.Context, r Registration) (credentials, error) {
	var c credentials
	b, err := m.secrets.Get(ctx, r.ID)
	if err != nil {
		return c, err
	}
	if len(b) > 131072 || json.Unmarshal(b, &c) != nil || c.Identity.RegistrationID != r.ID || c.Identity.Subject != r.Identity.Subject || c.Identity.ClientID != r.Identity.ClientID || c.HostID != m.meta.HostID {
		return c, fail("CREDENTIAL_STORE_FAILED", "Stored credentials do not match this account")
	}
	return c, nil
}
func (m *Manager) Begin(ctx context.Context, id string, openURL func(string) error) (Attempt, error) {
	return m.begin(ctx, id, "", openURL)
}

// Retry keeps an issued client ID after an authorization-code invalid_grant. It
// remains only an unverified pending registration until a fresh token validates.
func (m *Manager) Retry(ctx context.Context, id string, openURL func(string) error) (Attempt, error) {
	m.mu.Lock()
	a := m.attempts[id]
	if a == nil || a.public.State != "retry_required" || a.retryClientID == "" {
		m.mu.Unlock()
		return Attempt{}, fail("AUTH_NOT_FOUND", "No sign-in retry is available")
	}
	reg, client := a.public.RegistrationID, a.retryClientID
	m.mu.Unlock()
	return m.begin(ctx, reg, client, openURL)
}
func (m *Manager) begin(ctx context.Context, id, retryClient string, openURL func(string) error) (Attempt, error) {
	m.mu.Lock()
	if m.closed || ctx.Err() != nil || openURL == nil {
		m.mu.Unlock()
		return Attempt{}, fail("AUTH_CLOSED", "Sign-in is unavailable")
	}
	for _, a := range m.attempts {
		if a.public.State == "waiting" || a.public.State == "exchanging" {
			m.mu.Unlock()
			return Attempt{}, fail("AUTH_BUSY", "Finish or cancel the current sign-in first")
		}
	}
	if len(m.meta.Registrations) >= 32 && id == "" {
		m.mu.Unlock()
		return Attempt{}, fail("AUTH_LIMIT", "This host has reached its account registration limit")
	}
	clientID := dynamicClient
	selected := Identity{}
	hint := ""
	reconsent := false
	if id != "" {
		r := m.find(id)
		if r == nil {
			m.mu.Unlock()
			return Attempt{}, fail("AUTH_NOT_FOUND", "Account registration was not found")
		}
		if r.State == "sign_out_incomplete" {
			m.mu.Unlock()
			return Attempt{}, fail("CREDENTIAL_STORE_FAILED", "Finish clearing this account’s credentials before signing in again")
		}
		selected = r.Identity
		clientID = selected.ClientID
		reconsent = !r.PlanAuthorized && r.State == "signed_in"
		if r.State == "signed_in" {
			c, err := m.readCredentials(ctx, *r)
			if err == nil {
				hint = c.IDToken
			} else if !errors.Is(err, ErrSecretMissing) {
				m.mu.Unlock()
				return Attempt{}, fail("CREDENTIAL_STORE_FAILED", "Account credentials could not be read")
			}
		}
	}
	if retryClient != "" {
		clientID = retryClient
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		m.mu.Unlock()
		return Attempt{}, fail("AUTH_CALLBACK_FAILED", "The local sign-in listener could not start")
	}
	attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	a := &pendingAttempt{public: Attempt{ID: uuid.NewString(), RegistrationID: id, State: "waiting", Message: "Complete sign-in in your browser", ExpiresAt: m.now().Add(10 * time.Minute)}, ctx: attemptCtx, cancel: cancel, state: randomSecret(), nonce: randomSecret(), verifier: randomSecret(), redirect: "http://" + listener.Addr().String() + "/auth/callback", clientID: clientID, selected: selected}
	sum := sha256.Sum256([]byte(a.verifier))
	query := url.Values{"client_id": {clientID}, "ext_agent_host_id": {m.meta.HostID}, "response_type": {"code"}, "redirect_uri": {a.redirect}, "scope": {scope}, "resource": {resource}, "state": {a.state}, "nonce": {a.nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
	if clientID == dynamicClient {
		query.Set("agent_name_hint", "Trove")
	}
	if hint != "" {
		query.Set("id_token_hint", hint)
	}
	if selected.Email != "" {
		query.Set("login_hint", selected.Email)
	}
	if reconsent {
		query.Set("prompt", "consent")
	}
	for key, p := range m.attempts {
		if len(m.attempts) >= 16 && p.public.State != "waiting" && p.public.State != "exchanging" {
			delete(m.attempts, key)
		}
	}
	m.attempts[a.public.ID] = a
	a.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.callback(a, w, r) }), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	go a.server.Serve(listener)
	go func() {
		<-attemptCtx.Done()
		m.mu.Lock()
		if a.public.State == "waiting" || a.public.State == "exchanging" {
			a.public.State = "cancelled"
			a.public.Message = "Sign-in expired or was cancelled"
		}
		m.mu.Unlock()
		a.server.Close()
	}()
	result := a.public
	m.mu.Unlock()
	if openURL(authorizeEndpoint+"?"+query.Encode()) != nil {
		_ = m.Cancel(result.ID)
		return Attempt{}, fail("AUTH_BROWSER_FAILED", "The system browser could not open sign-in")
	}
	return result, nil
}
func (m *Manager) Attempt(id string) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[id]
	if a == nil {
		return Attempt{}, fail("AUTH_NOT_FOUND", "Sign-in attempt was not found")
	}
	return a.public, nil
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[id]
	if a == nil {
		return fail("AUTH_NOT_FOUND", "Sign-in attempt was not found")
	}
	if a.public.State == "waiting" || a.public.State == "exchanging" {
		a.public.State = "cancelled"
		a.public.Message = "Sign-in cancelled"
		a.cancel()
	}
	return nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	for _, a := range m.attempts {
		a.cancel()
		_ = a.server.Close()
	}
	return m.lock.Close()
}

func (m *Manager) callback(a *pendingAttempt, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	target, _ := url.Parse(a.redirect)
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if r.Method != "GET" || r.URL.Path != "/auth/callback" || r.Host != target.Host || host != "127.0.0.1" || r.Header.Get("Origin") != "" || len(r.URL.RawQuery) > 8192 {
		http.Error(w, "Invalid sign-in callback", 400)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "Invalid sign-in callback", 400)
		return
	}
	for _, key := range []string{"state", "code", "client_id", "error"} {
		if len(q[key]) > 1 {
			http.Error(w, "Invalid sign-in callback", 400)
			return
		}
	}
	m.mu.Lock()
	if m.closed || a.ctx.Err() != nil || a.public.State != "waiting" || !equalSecret(q.Get("state"), a.state) {
		m.mu.Unlock()
		http.Error(w, "Invalid or expired sign-in callback", 400)
		return
	}
	a.public.State = "exchanging"
	a.public.Message = "Verifying your account"
	client := a.clientID
	if client == dynamicClient {
		client = q.Get("client_id")
	} else if q.Get("client_id") != "" && q.Get("client_id") != client {
		client = ""
	}
	m.mu.Unlock()
	defer func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			a.server.Shutdown(ctx)
			a.cancel()
		}()
	}()
	if q.Get("error") != "" {
		m.finishAttempt(a, "failed", "Sign-in was declined or could not complete")
		io.WriteString(w, "Sign-in did not complete. Return to Trove.")
		return
	}
	if !validClientID(client) || q.Get("code") == "" {
		m.finishAttempt(a, "failed", "Registration did not return a valid issued client ID and code")
		http.Error(w, "Sign-in could not be verified. Return to Trove.", 400)
		return
	}
	provider, err := m.provider(a.ctx)
	var token tokenResponse
	if err == nil {
		token, err = m.tokenRequest(a.ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {q.Get("code")}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirect}, "resource": {resource}})
	}
	if err != nil {
		var oauth *oauthFailure
		if errors.As(err, &oauth) && oauth.code == "invalid_grant" {
			m.mu.Lock()
			a.retryClientID = client
			m.mu.Unlock()
			m.finishAttempt(a, "retry_required", "The sign-in code expired. Retry sign-in to continue this registration")
		} else {
			m.finishAttempt(a, "failed", "Sign-in could not be verified; try again")
		}
		http.Error(w, "Sign-in did not complete. Return to Trove.", 400)
		return
	}
	identity, err := m.validateID(a.ctx, provider, token.IDToken, client, a.nonce)
	if err == nil && a.selected.Subject != "" && (identity.Subject != a.selected.Subject || identity.ClientID != a.selected.ClientID) {
		err = fail("AUTH_ACCOUNT_MISMATCH", "Sign-in returned a different account")
	}
	if err != nil {
		m.finishAttempt(a, "failed", "Sign-in returned an invalid or different account; saved accounts are unchanged")
		http.Error(w, "Sign-in could not be verified. Return to Trove.", 400)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || a.ctx.Err() != nil || a.public.State != "exchanging" {
		http.Error(w, "Sign-in cancelled. Return to Trove.", 400)
		return
	}
	id := a.public.RegistrationID
	if id == "" {
		for _, reg := range m.meta.Registrations {
			if reg.Identity.Subject == identity.Subject && reg.Identity.ClientID == identity.ClientID {
				id = reg.ID
			}
		}
	}
	isNew := id == ""
	if isNew {
		id = uuid.NewString()
	}
	identity.RegistrationID = id
	c, err := makeCredentials(token, identity, m.meta.HostID, nil, m.now())
	if err == nil {
		// Persist the validated identity before tokens. A failed Keychain write leaves
		// a reauthorizable signed-out mapping, never a half-authenticated account.
		reg := Registration{ID: id, Identity: identity, State: "signed_out"}
		if isNew {
			m.meta.Registrations = append(m.meta.Registrations, reg)
		} else {
			*m.find(id) = reg
		}
		err = m.save()
		if err == nil {
			var data []byte
			data, err = json.Marshal(c)
			if err == nil {
				err = m.secrets.Put(a.ctx, id, data)
			}
		}
		if err == nil {
			reg.State = "signed_in"
			reg.PlanAuthorized = planScope(c)
			reg.ExpiresAt = c.ExpiresAt
			*m.find(id) = reg
			err = m.save()
		}
	}
	if err != nil {
		a.public.State = "failed"
		a.public.Message = "Verified credentials could not be saved securely"
		if reg := m.find(id); reg != nil {
			reg.State = "reauthorization_required"
			reg.PlanAuthorized = false
			reg.ExpiresAt = time.Time{}
			_ = m.save()
		}
		http.Error(w, "Sign-in could not be saved. Return to Trove.", 500)
		return
	}
	a.public.RegistrationID = id
	a.public.State = "completed"
	a.public.Message = "Signed in. Select this account in Trove"
	io.WriteString(w, "Sign-in complete. You may close this window and return to Trove.")
}
func (m *Manager) finishAttempt(a *pendingAttempt, state, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.public.State == "exchanging" {
		a.public.State = state
		a.public.Message = message
	}
}
