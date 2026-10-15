package connectors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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
	"trove.local/phase0/internal/platformfs"
	"trove.local/phase0/internal/providers/auth"
)

type Registration struct {
	Kind     Kind   `json:"kind"`
	ClientID string `json:"clientId"`
	Endpoint string `json:"endpoint,omitempty"`
}
type AuthorizationAttempt struct {
	ID              string `json:"id"`
	Kind            Kind   `json:"kind"`
	State           string `json:"state"`
	Message         string `json:"message"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	ExpiresAt       string `json:"expiresAt"`
}

// Only credential storage serializes this private type. It never crosses an
// application bridge, ordinary service dispatcher or diagnostics boundary.
type protectedCredential struct {
	Version                               int
	WorkspaceID, WorkspacePath, AccountID string
	Registration                          Registration
	Identity                              Identity
	AccessToken, RefreshToken             string
	ExpiresAt                             time.Time
	Scopes                                string
}
type authAttempt struct {
	public                                                           AuthorizationAttempt
	registration                                                     Registration
	workspaceID, workspacePath, accountID, state, verifier, redirect string
	cancel                                                           context.CancelFunc
	server                                                           *http.Server
	credential                                                       *protectedCredential
	selected                                                         []string
	claimed                                                          bool
}
type Broker struct {
	mu       sync.Mutex
	tokenMu  sync.Mutex
	secrets  auth.SecretStore
	client   *http.Client
	attempts map[string]*authAttempt
	closed   bool
}

func NewBroker(root string) (*Broker, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fail("STORAGE_FAILED", "Invalid connector account storage")
	}
	if err := platformfs.EnsurePrivateDirectory(root); err != nil {
		return nil, fail("STORAGE_FAILED", "Connector account storage is unavailable")
	}
	store, err := auth.NewNativeSecretStore(root)
	if err != nil {
		return nil, err
	}
	return newBroker(store, NewHTTPClient()), nil
}
func newBroker(store auth.SecretStore, client *http.Client) *Broker {
	return &Broker{secrets: store, client: client, attempts: map[string]*authAttempt{}}
}
func secretString() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, a := range b.attempts {
		a.cancel()
		if a.server != nil {
			_ = a.server.Close()
		}
		a.credential = nil
	}
	b.attempts = map[string]*authAttempt{}
}
func (b *Broker) Attempt(id string) (AuthorizationAttempt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.attempts[id]
	if a == nil {
		return AuthorizationAttempt{}, fail("NOT_FOUND", "Authorization attempt is unavailable")
	}
	return a.public, nil
}
func (b *Broker) Cancel(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.attempts[id]
	if a == nil {
		return fail("NOT_FOUND", "Authorization attempt is unavailable")
	}
	if a.public.State == "complete" {
		return fail("CONFLICT", "Authorization is already complete; disconnect the account instead")
	}
	a.cancel()
	if a.server != nil {
		_ = a.server.Close()
	}
	a.credential = nil
	a.public.State = "cancelled"
	a.public.Message = "Authorization cancelled"
	return nil
}
func (b *Broker) finish(id, state, message string, credential *protectedCredential, selected []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.attempts[id]
	if a == nil || b.closed || a.public.State == "cancelled" {
		return
	}
	a.public.State = state
	a.public.Message = message
	a.credential = credential
	a.selected = selected
}
func (b *Broker) Begin(ctx context.Context, workspaceID, workspacePath, accountID string, r Registration, openURL func(string) error) (AuthorizationAttempt, error) {
	if !filepath.IsAbs(workspacePath) || workspaceID == "" || accountID == "" || len(r.ClientID) > 512 || strings.ContainsAny(r.ClientID, "\r\n\x00") {
		return AuthorizationAttempt{}, fail("INVALID_REQUEST", "Invalid native authorization registration")
	}
	if r.Kind != GoogleCalendar && r.Kind != GoogleDrive && r.Kind != Gmail && r.Kind != GitHub {
		return AuthorizationAttempt{}, Unsupported("authorization for " + string(r.Kind))
	}
	if strings.TrimSpace(r.ClientID) == "" {
		return AuthorizationAttempt{}, fail("REGISTRATION_REQUIRED", "Configure the provider's native application client ID")
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return AuthorizationAttempt{}, fail("CLOSED", "Connector account broker is closed")
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
	a := &authAttempt{public: AuthorizationAttempt{ID: id, Kind: r.Kind, State: "waiting", Message: "Complete authorization in your browser", ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)}, registration: r, workspaceID: workspaceID, workspacePath: workspacePath, accountID: accountID, state: secretString(), verifier: secretString(), cancel: cancel}
	b.attempts[id] = a
	b.mu.Unlock()
	if r.Kind == GitHub {
		if err := b.beginDevice(flow, a, openURL); err != nil {
			cancel()
			b.finish(id, "failed", safeAuthError(err), nil, nil)
			public, _ := b.Attempt(id)
			return public, err
		}
		return b.Attempt(id)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		b.finish(id, "failed", "Local authorization callback is unavailable", nil, nil)
		return AuthorizationAttempt{}, fail("CALLBACK_UNAVAILABLE", "Local authorization callback is unavailable")
	}
	b.mu.Lock()
	if b.closed || a.public.State != "waiting" {
		b.mu.Unlock()
		listener.Close()
		cancel()
		return AuthorizationAttempt{}, fail("CANCELLED", "Authorization was cancelled")
	}
	a.redirect = "http://" + listener.Addr().String() + "/callback"
	mux := http.NewServeMux()
	a.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	b.mu.Unlock()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if req.Method != "GET" || req.Host != listener.Addr().String() || len(req.URL.RawQuery) > 32768 || subtle.ConstantTimeCompare([]byte(req.URL.Query().Get("state")), []byte(a.state)) != 1 {
			http.Error(w, "Invalid authorization response", 400)
			return
		}
		b.mu.Lock()
		if a.claimed || a.public.State != "waiting" {
			b.mu.Unlock()
			http.Error(w, "Authorization response already handled", 409)
			return
		}
		a.claimed = true
		b.mu.Unlock()
		q := req.URL.Query()
		if q.Get("error") != "" || q.Get("code") == "" {
			b.finish(id, "failed", "Provider did not authorize this connection", nil, nil)
			_, _ = io.WriteString(w, "Authorization was not completed. Return to Trove.")
			cancel()
			return
		}
		credential, err := b.exchangeGoogle(flow, a, q.Get("code"))
		if err != nil {
			b.finish(id, "failed", safeAuthError(err), nil, nil)
		} else {
			var selected []string
			if r.Kind == GoogleDrive {
				for _, file := range strings.Split(q.Get("picked_file_ids"), ",") {
					if strings.TrimSpace(file) != "" {
						selected = append(selected, file)
					}
				}
			}
			b.finish(id, "authorized", "Authorization received; verifying account identity", credential, selected)
		}
		_, _ = io.WriteString(w, "Return to Trove to finish connecting this account.")
		go func() { _ = a.server.Shutdown(context.Background()); cancel() }()
	})
	go func() { _ = a.server.Serve(listener) }()
	go func() {
		<-flow.Done()
		_ = a.server.Close()
		b.mu.Lock()
		if a.public.State == "waiting" {
			a.public.State = "expired"
			a.public.Message = "Authorization expired; start again"
		}
		b.mu.Unlock()
	}()
	challenge := sha256.Sum256([]byte(a.verifier))
	values := url.Values{"client_id": {r.ClientID}, "redirect_uri": {a.redirect}, "response_type": {"code"}, "scope": {googleScope(r.Kind)}, "state": {a.state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "access_type": {"offline"}, "prompt": {"consent"}}
	if r.Kind == GoogleDrive {
		values.Set("trigger_onepick", "true")
		values.Set("allow_multiple", "true")
		values.Set("allow_folder_selection", "true")
	}
	if err = openURL("https://accounts.google.com/o/oauth2/v2/auth?" + values.Encode()); err != nil {
		_ = b.Cancel(id)
		return AuthorizationAttempt{}, fail("BROWSER_UNAVAILABLE", "Could not open the system browser")
	}
	return b.Attempt(id)
}
func googleScope(kind Kind) string {
	switch kind {
	case GoogleDrive:
		return "https://www.googleapis.com/auth/drive.file"
	case GoogleCalendar:
		return "openid email https://www.googleapis.com/auth/calendar.readonly https://www.googleapis.com/auth/calendar.events"
	case Gmail:
		return "openid email https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send"
	}
	return ""
}
func safeAuthError(err error) string {
	if f, ok := err.(*Failure); ok {
		return f.Message
	}
	return "Authorization could not be completed; reconnect explicitly"
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
}

func (b *Broker) oauthPost(ctx context.Context, target string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(values.Encode()))
	if err != nil {
		return fail("INVALID_REQUEST", "Invalid authorization request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return fail("AUTH_INTERRUPTED", "Authorization response was interrupted; reconnect explicitly")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil || json.Unmarshal(data, out) != nil {
		return fail("AUTH_REJECTED", "Provider returned an invalid authorization response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail("AUTH_REJECTED", "Provider rejected authorization; verify the application registration")
	}
	return nil
}
func (b *Broker) exchangeGoogle(ctx context.Context, a *authAttempt, code string) (*protectedCredential, error) {
	var token tokenResponse
	if err := b.oauthPost(ctx, "https://oauth2.googleapis.com/token", url.Values{"client_id": {a.registration.ClientID}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirect}, "grant_type": {"authorization_code"}}, &token); err != nil {
		return nil, err
	}
	if token.Error != "" || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn < 1 {
		return nil, fail("AUTH_REJECTED", "Provider did not issue a usable access token")
	}
	granted := map[string]bool{}
	for _, scope := range strings.Fields(token.Scope) {
		granted[scope] = true
	}
	for _, scope := range strings.Fields(googleScope(a.registration.Kind)) {
		if !granted[scope] && !(scope == "email" && granted["https://www.googleapis.com/auth/userinfo.email"]) {
			return nil, fail("SCOPE_DENIED", "Provider did not grant all selected connector permissions")
		}
	}
	return &protectedCredential{Version: 1, WorkspaceID: a.workspaceID, WorkspacePath: a.workspacePath, AccountID: a.accountID, Registration: a.registration, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second), Scopes: token.Scope}, nil
}
func (b *Broker) beginDevice(ctx context.Context, a *authAttempt, openURL func(string) error) error {
	var response struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := b.oauthPost(ctx, "https://github.com/login/device/code", url.Values{"client_id": {a.registration.ClientID}}, &response); err != nil {
		return err
	}
	if response.VerificationURI != "https://github.com/login/device" || response.DeviceCode == "" || response.UserCode == "" || response.ExpiresIn < 1 {
		return fail("AUTH_REJECTED", "GitHub device authorization response is invalid")
	}
	if response.Interval < 5 {
		response.Interval = 5
	}
	b.mu.Lock()
	a.public.UserCode = response.UserCode
	a.public.VerificationURL = response.VerificationURI
	a.public.Message = "Enter the displayed code on GitHub to authorize the selected installation repositories"
	b.mu.Unlock()
	if err := openURL(response.VerificationURI); err != nil {
		return fail("BROWSER_UNAVAILABLE", "Could not open the system browser")
	}
	go func() {
		defer a.cancel()
		interval := time.Duration(response.Interval) * time.Second
		deadline := time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)
		for time.Now().Before(deadline) {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				b.finish(a.public.ID, "expired", "Authorization ended; start again", nil, nil)
				return
			case <-timer.C:
			}
			var token tokenResponse
			err := b.oauthPost(ctx, "https://github.com/login/oauth/access_token", url.Values{"client_id": {a.registration.ClientID}, "device_code": {response.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &token)
			if err != nil {
				b.finish(a.public.ID, "failed", safeAuthError(err), nil, nil)
				return
			}
			switch token.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			case "":
			default:
				b.finish(a.public.ID, "failed", "GitHub authorization was denied or expired", nil, nil)
				return
			}
			if token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") {
				b.finish(a.public.ID, "failed", "GitHub did not issue a usable token", nil, nil)
				return
			}
			credential := &protectedCredential{Version: 1, WorkspaceID: a.workspaceID, WorkspacePath: a.workspacePath, AccountID: a.accountID, Registration: a.registration, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, Scopes: token.Scope}
			if token.ExpiresIn > 0 {
				credential.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
			}
			b.finish(a.public.ID, "authorized", "Authorization received; verifying account identity", credential, nil)
			return
		}
		b.finish(a.public.ID, "expired", "GitHub authorization expired", nil, nil)
	}()
	return nil
}

// Complete consumes a successful browser grant once and verifies identity using
// the adapter before storing credentials. No secrets are returned to the UI.
func (b *Broker) Complete(ctx context.Context, id string, adapter Adapter) (Account, error) {
	b.mu.Lock()
	a := b.attempts[id]
	if a == nil || a.public.State != "authorized" || a.credential == nil {
		b.mu.Unlock()
		return Account{}, fail("CONFLICT", "Authorization has not completed")
	}
	credential := *a.credential
	selected := append([]string{}, a.selected...)
	a.public.State = "verifying"
	b.mu.Unlock()
	account := Account{Version: 1, ID: credential.AccountID, Kind: credential.Registration.Kind, Endpoint: credential.Registration.Endpoint, RegistrationID: uuid.NewString(), Selections: []Selection{}}
	s := Session{Account: account, Token: Secret{AccessToken: credential.AccessToken, RefreshToken: credential.RefreshToken, ExpiresAt: credential.ExpiresAt}, Client: b.client}
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		b.finish(id, "failed", safeAuthError(err), nil, nil)
		return Account{}, err
	}
	credential.Identity = identity
	account.Identity = identity
	for _, file := range selected {
		account.Selections = append(account.Selections, Selection{ID: file, Kind: "file", Label: file})
	}
	data, _ := json.Marshal(credential)
	b.mu.Lock()
	valid := !b.closed && a.public.State == "verifying"
	b.mu.Unlock()
	if !valid {
		return Account{}, fail("CANCELLED", "Authorization was cancelled")
	}
	if err = b.secrets.Put(ctx, account.RegistrationID, data); err != nil {
		b.finish(id, "failed", "The operating system could not store connector credentials", nil, nil)
		return Account{}, fail("CREDENTIAL_STORE_FAILED", "Connector credentials could not be stored")
	}
	b.mu.Lock()
	valid = !b.closed && a.public.State == "verifying"
	if valid {
		a.public.State = "complete"
		a.public.Message = "Account authorization completed"
		a.credential = nil
	}
	b.mu.Unlock()
	if !valid {
		_ = b.secrets.Delete(context.WithoutCancel(ctx), account.RegistrationID)
		return Account{}, fail("CANCELLED", "Authorization was cancelled")
	}
	return account, nil
}
func (b *Broker) Session(ctx context.Context, workspaceID, workspacePath string, a Account) (Session, error) {
	b.tokenMu.Lock()
	defer b.tokenMu.Unlock()
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return Session{}, fail("CLOSED", "Connector broker is closed")
	}
	if a.State != "active" || a.RegistrationID == "" {
		return Session{}, fail("AUTH_REQUIRED", "Authorize this connection explicitly")
	}
	data, err := b.secrets.Get(ctx, a.RegistrationID)
	if err != nil {
		return Session{}, fail("AUTH_REQUIRED", "Connector credentials are unavailable; reconnect this account")
	}
	var c protectedCredential
	if json.Unmarshal(data, &c) != nil || c.Version != 1 || c.WorkspaceID != workspaceID || c.WorkspacePath != workspacePath || c.AccountID != a.ID || c.Registration.Kind != a.Kind || c.Registration.Endpoint != a.Endpoint || RequireIdentity(a.Identity, c.Identity) != nil {
		return Session{}, fail("AUTH_REQUIRED", "Stored authorization does not match this workspace and account")
	}
	if !c.ExpiresAt.IsZero() && time.Until(c.ExpiresAt) < time.Minute {
		if c.Registration.Kind == GitHub || c.RefreshToken == "" {
			return Session{}, fail("AUTH_REQUIRED", "Account authorization expired; reconnect explicitly")
		}
		var token tokenResponse
		if err = b.oauthPost(ctx, "https://oauth2.googleapis.com/token", url.Values{"client_id": {c.Registration.ClientID}, "refresh_token": {c.RefreshToken}, "grant_type": {"refresh_token"}}, &token); err != nil {
			return Session{}, err
		}
		if token.AccessToken == "" || token.Error != "" || token.ExpiresIn < 1 {
			return Session{}, fail("AUTH_REQUIRED", "Account refresh was rejected; reconnect explicitly")
		}
		c.AccessToken = token.AccessToken
		if token.RefreshToken != "" {
			c.RefreshToken = token.RefreshToken
		}
		c.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		data, _ = json.Marshal(c)
		if err = b.secrets.Put(ctx, a.RegistrationID, data); err != nil {
			return Session{}, fail("CREDENTIAL_STORE_FAILED", "Refreshed account credentials could not be stored")
		}
	}
	return Session{Account: a, Token: Secret{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, ExpiresAt: c.ExpiresAt}, Client: b.client}, nil
}
func (b *Broker) Forget(ctx context.Context, registrationID string) error {
	b.tokenMu.Lock()
	defer b.tokenMu.Unlock()
	if _, err := uuid.Parse(registrationID); err != nil {
		return fail("INVALID_REQUEST", "Invalid credential registration")
	}
	return b.secrets.Delete(ctx, registrationID)
}
