package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/net/netutil"
	"golang.org/x/time/rate"
)

var errInvalid = errors.New("remote MCP configuration or authorization is invalid")

type Server struct {
	mu                  sync.Mutex
	config              Config
	callbacks           Callbacks
	now                 func() time.Time
	fetch               func(context.Context, string) (clientMetadata, error)
	connections         map[string]*connection
	clients             map[string]client
	pending             map[string]*pending
	codes               map[[32]byte]*code
	access              map[[32]byte]*token
	refresh             map[[32]byte]*token
	http                *http.Server
	listener            net.Listener
	enabled, closed     bool
	message             string
	requests            chan struct{}
	limiter             *rate.Limiter
	registrationLimiter *rate.Limiter
}

func New(config Config, callbacks Callbacks) (*Server, error) {
	return newServer(config, callbacks, time.Now, fetchMetadata)
}
func newServer(config Config, callbacks Callbacks, now func() time.Time, fetch func(context.Context, string) (clientMetadata, error)) (*Server, error) {
	origin, err := httpsURL(config.PublicOrigin, false)
	if err != nil || origin.Path != "" || origin.Host != strings.ToLower(origin.Host) || net.ParseIP(origin.Hostname()) != nil || !strings.Contains(origin.Hostname(), ".") || len(config.AllowedOrigins) > 8 || callbacks.Forward == nil || callbacks.Validate == nil || callbacks.Disconnect == nil || callbacks.RecordDecision == nil {
		return nil, errInvalid
	}
	bind, port, err := net.SplitHostPort(config.BindAddress)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(bind) == nil || portErr != nil || portNumber < 0 || portNumber > 65535 {
		return nil, errInvalid
	}
	for _, raw := range config.AllowedOrigins {
		u, err := httpsURL(raw, false)
		if err != nil || u.Path != "" {
			return nil, errInvalid
		}
	}
	if len(config.Certificate.Certificate) == 0 || config.Certificate.PrivateKey == nil {
		return nil, errInvalid
	}
	leaf, err := x509.ParseCertificate(config.Certificate.Certificate[0])
	if err != nil || leaf.VerifyHostname(origin.Hostname()) != nil || now().Before(leaf.NotBefore) || !now().Before(leaf.NotAfter) {
		return nil, errInvalid
	}
	allowed := len(leaf.ExtKeyUsage) == 0
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny {
			allowed = true
		}
	}
	if !allowed {
		return nil, errInvalid
	}
	config.Certificate.Leaf = leaf
	config.AllowedOrigins = append([]string{}, config.AllowedOrigins...)
	return &Server{config: config, callbacks: callbacks, now: now, fetch: fetch, connections: map[string]*connection{}, clients: map[string]client{}, pending: map[string]*pending{}, codes: map[[32]byte]*code{}, access: map[[32]byte]*token{}, refresh: map[[32]byte]*token{}, requests: make(chan struct{}, 64), limiter: rate.NewLimiter(20, 80), registrationLimiter: rate.NewLimiter(rate.Every(6*time.Second), 10)}, nil
}
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled || s.closed {
		return errInvalid
	}
	listener, err := net.Listen("tcp", s.config.BindAddress)
	if err != nil {
		return errors.New("the HTTPS listener could not bind to the selected address")
	}
	listener = netutil.LimitListener(listener, 128)
	s.listener = listener
	s.enabled = true
	s.http = &http.Server{Handler: s.Handler(), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.config.Certificate}}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, ErrorLog: log.New(io.Discard, "", 0)}
	go func() {
		err := s.http.ServeTLS(listener, "", "")
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.message = "The remote HTTPS listener stopped; enable it again after checking its configuration"
			s.mu.Unlock()
			_ = s.Close()
		}
	}()
	return nil
}
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.enabled = false
	ids := []string{}
	for id, c := range s.connections {
		ids = append(ids, id)
		c.public.State = "disconnected"
		for _, f := range c.generations {
			f.revoked = true
		}
	}
	server := s.http
	s.pending = map[string]*pending{}
	s.codes = map[[32]byte]*code{}
	s.access = map[[32]byte]*token{}
	s.refresh = map[[32]byte]*token{}
	s.clients = map[string]client{}
	s.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range ids {
		_ = s.callbacks.Disconnect(ctx, id)
	}
	return nil
}
func (s *Server) AddConnection(ctx context.Context, b Binding) (Connection, error) {
	if b.ID == "" || b.Token == "" || len(b.PageIDs) < 1 || len(b.PageIDs) > 100 || len(b.Tools) < 1 || len(b.Tools) > 16 {
		return Connection{}, errInvalid
	}
	if s.callbacks.Validate(ctx, b.ID, b.Token) != nil {
		return Connection{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if !s.enabled || s.closed || len(s.connections) >= 32 || s.connections[b.ID] != nil {
		return Connection{}, errInvalid
	}
	resourceID := opaque()
	c := &connection{public: Connection{ID: b.ID, URL: s.config.PublicOrigin + "/mcp/" + resourceID, PageIDs: append([]string{}, b.PageIDs...), Tools: append([]string{}, b.Tools...), State: "awaiting_client"}, token: b.Token, resourceID: resourceID, generations: map[string]*family{}}
	s.connections[b.ID] = c
	return c.public, nil
}
func (s *Server) Disconnect(ctx context.Context, id string) error {
	s.mu.Lock()
	c := s.connections[id]
	if c == nil {
		s.mu.Unlock()
		return errInvalid
	}
	c.public.State = "disconnected"
	for _, f := range c.generations {
		f.revoked = true
	}
	for _, p := range s.pending {
		if p.public.ConnectionID == id {
			p.public.State = "denied"
		}
	}
	s.mu.Unlock()
	return s.callbacks.Disconnect(ctx, id)
}
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	out := Status{Enabled: s.enabled, Origin: s.config.PublicOrigin, BindAddress: s.config.BindAddress, CertificateExpiresAt: s.config.Certificate.Leaf.NotAfter, Connections: []Connection{}, Authorizations: []Authorization{}, Message: s.message}
	for _, c := range s.connections {
		value := c.public
		value.PageIDs = append([]string{}, value.PageIDs...)
		value.Tools = append([]string{}, value.Tools...)
		out.Connections = append(out.Connections, value)
	}
	for _, p := range s.pending {
		out.Authorizations = append(out.Authorizations, p.public)
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].ID < out.Connections[j].ID })
	sort.Slice(out.Authorizations, func(i, j int) bool { return out.Authorizations[i].ID < out.Authorizations[j].ID })
	return out
}
func (s *Server) Decide(ctx context.Context, id, reviewHash string, approve bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	p := s.pending[id]
	if p == nil || p.public.State != "pending" || !equal(reviewHash, p.public.ReviewHash) || !s.enabled {
		return errInvalid
	}
	c := s.connections[p.public.ConnectionID]
	if c == nil || c.public.State == "disconnected" || s.callbacks.Validate(ctx, c.public.ID, c.token) != nil {
		return errInvalid
	}
	if approve && c.public.ClientID != "" && c.public.ClientID != p.client.metadata.ID {
		return errInvalid
	}
	if err := s.callbacks.RecordDecision(ctx, c.public.ID, p.client.metadata.ID, p.public.ReviewHash, approve); err != nil {
		return err
	}
	if !approve {
		p.public.State = "denied"
		return nil
	}
	c.public.ClientID = p.client.metadata.ID
	c.public.State = "authorized"
	p.public.State = "approved"
	return nil
}
func opaque() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func equal(a, b string) bool { return constantEqual(a, b) }
func (s *Server) byResource(resource string) *connection {
	for _, c := range s.connections {
		if c.public.URL == resource && c.public.State != "disconnected" {
			return c
		}
	}
	return nil
}
func (s *Server) prune() {
	now := s.now()
	for id, p := range s.pending {
		if !now.Before(p.public.ExpiresAt) {
			delete(s.pending, id)
		}
	}
	for hash, c := range s.codes {
		if !now.Before(c.expires) {
			delete(s.codes, hash)
		}
	}
	for hash, t := range s.access {
		if t.family.revoked || !now.Before(t.expires) {
			delete(s.access, hash)
		}
	}
	for hash, t := range s.refresh {
		if t.family.revoked || !now.Before(t.family.expires) {
			delete(s.refresh, hash)
		}
	}
	for id, c := range s.clients {
		if !now.Before(c.expires) {
			delete(s.clients, id)
		}
	}
	for _, c := range s.connections {
		for id, f := range c.generations {
			if f.revoked || !now.Before(f.expires) {
				delete(c.generations, id)
			}
		}
	}
}
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; form-action 'self'")
	origin, _ := url.Parse(s.config.PublicOrigin)
	if r.TLS == nil || r.Host != origin.Host || r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") {
		http.Error(w, "Invalid HTTPS host", http.StatusForbidden)
		return
	}
	suppliedOrigin := r.Header.Get("Origin")
	if suppliedOrigin != "" && suppliedOrigin != s.config.PublicOrigin && !contains(s.config.AllowedOrigins, suppliedOrigin) {
		http.Error(w, "Origin is not allowed", 403)
		return
	}
	if suppliedOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", suppliedOrigin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version, MCP-Session-Id, Mcp-Method, Mcp-Name")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, MCP-Session-Id, MCP-Protocol-Version")
	}
	s.mu.Lock()
	enabled := s.enabled && !s.closed && s.now().Before(s.config.Certificate.Leaf.NotAfter)
	s.prune()
	s.mu.Unlock()
	if !enabled {
		http.Error(w, "Remote access is disabled", 503)
		return
	}
	if !s.limiter.Allow() {
		w.Header().Set("Retry-After", "3")
		http.Error(w, "Request limit reached", 429)
		return
	}
	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	default:
		http.Error(w, "Too many concurrent requests", 429)
		return
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	switch {
	case r.URL.Path == "/.well-known/oauth-authorization-server":
		s.authorizationMetadata(w, r)
	case strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource/mcp/"):
		s.resourceMetadata(w, r)
	case r.URL.Path == "/oauth/register":
		s.register(w, r)
	case r.URL.Path == "/oauth/authorize":
		s.authorize(w, r)
	case strings.HasPrefix(r.URL.Path, "/oauth/review/"):
		s.review(w, r)
	case r.URL.Path == "/oauth/token":
		s.exchange(w, r)
	case r.URL.Path == "/oauth/revoke":
		s.revoke(w, r)
	case strings.HasPrefix(r.URL.Path, "/mcp/"):
		s.forward(w, r)
	default:
		http.NotFound(w, r)
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}
func (s *Server) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, 200, map[string]any{"issuer": s.config.PublicOrigin, "authorization_endpoint": s.config.PublicOrigin + "/oauth/authorize", "token_endpoint": s.config.PublicOrigin + "/oauth/token", "revocation_endpoint": s.config.PublicOrigin + "/oauth/revoke", "registration_endpoint": s.config.PublicOrigin + "/oauth/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{connectionScope}, "client_id_metadata_document_supported": true, "authorization_response_iss_parameter_supported": true})
}
func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	resource := s.config.PublicOrigin + strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
	s.mu.Lock()
	c := s.byResource(resource)
	s.mu.Unlock()
	if c == nil {
		http.NotFound(w, r)
		return
	}
	mcpauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{Resource: resource, AuthorizationServers: []string{s.config.PublicOrigin}, ScopesSupported: []string{connectionScope}}).ServeHTTP(w, r)
}
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request")
		return
	}
	resource := s.config.PublicOrigin + r.URL.Path
	s.mu.Lock()
	c := s.byResource(resource)
	s.mu.Unlock()
	if c == nil {
		http.NotFound(w, r)
		return
	}
	verifier := func(ctx context.Context, bearer string, request *http.Request) (*mcpauth.TokenInfo, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		t := s.access[sha256.Sum256([]byte(bearer))]
		if t == nil || t.family.revoked || t.family.resource != resource || !s.now().Before(t.expires) || !s.now().Before(t.family.expires) || c.public.State != "authorized" || c.public.ClientID != t.family.clientID || s.callbacks.Validate(ctx, c.public.ID, c.token) != nil {
			return nil, mcpauth.ErrInvalidToken
		}
		return &mcpauth.TokenInfo{Scopes: []string{connectionScope}, Expiration: t.expires, UserID: t.family.clientID + "|" + c.public.ID}, nil
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		id, credential := c.public.ID, c.token
		s.mu.Unlock()
		forwarded := r.Clone(r.Context())
		forwarded.Header = r.Header.Clone()
		forwarded.Header.Set("Trove-Connection-Id", id)
		forwarded.Header.Set("Authorization", "Bearer "+credential)
		s.callbacks.Forward.ServeHTTP(w, forwarded)
	})
	mcpauth.RequireBearerToken(verifier, &mcpauth.RequireBearerTokenOptions{Scopes: []string{connectionScope}, ResourceMetadataURL: s.config.PublicOrigin + "/.well-known/oauth-protected-resource" + r.URL.Path})(handler).ServeHTTP(w, r)
}
func reviewDigest(p *pending) string {
	raw, _ := json.Marshal(struct {
		Public                     Authorization
		State, Challenge, Resource string
	}{p.public, p.state, p.challenge, p.resource})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
