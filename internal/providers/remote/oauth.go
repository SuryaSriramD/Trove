package remote

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if !s.registrationLimiter.Allow() {
		oauthError(w, 429, "temporarily_unavailable")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	var meta clientMetadata
	if err != nil || len(body) > 65536 || json.Unmarshal(body, &meta) != nil || validateMetadata(meta) != nil || meta.ID != "" || meta.AuthMethod != "" && meta.AuthMethod != "none" {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	meta.ID = "trove_" + opaque()
	meta.AuthMethod = "none"
	meta.AuthMethods = nil
	if len(meta.GrantTypes) == 0 {
		meta.GrantTypes = []string{"authorization_code"}
	}
	if len(meta.ResponseTypes) == 0 {
		meta.ResponseTypes = []string{"code"}
	}
	s.mu.Lock()
	s.prune()
	if !s.enabled || s.closed {
		s.mu.Unlock()
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	if len(s.clients) >= 128 {
		s.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable")
		return
	}
	s.clients[meta.ID] = client{metadata: meta, expires: s.now().Add(8 * time.Hour)}
	s.mu.Unlock()
	writeJSON(w, 201, meta)
}
func (s *Server) resolveClient(ctx context.Context, id string) (client, error) {
	if strings.HasPrefix(id, "https://") {
		if !s.registrationLimiter.Allow() {
			return client{}, errMetadata
		}
		meta, err := s.fetch(ctx, id)
		if err != nil || meta.ID != id || validateMetadata(meta) != nil {
			return client{}, errMetadata
		}
		return client{metadata: meta, url: id, expires: s.now().Add(8 * time.Hour)}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[id]
	if !ok || !s.now().Before(c.expires) {
		return client{}, errMetadata
	}
	return c, nil
}
func single(values url.Values, keys ...string) bool {
	for _, k := range keys {
		if len(values[k]) > 1 {
			return false
		}
	}
	return true
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > 8192 || !single(q, "client_id", "redirect_uri", "response_type", "scope", "state", "resource", "code_challenge", "code_challenge_method") || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) > 1024 || q.Get("scope") != "" && q.Get("scope") != connectionScope {
		oauthError(w, 400, "invalid_request")
		return
	}
	challenge, err := base64.RawURLEncoding.DecodeString(q.Get("code_challenge"))
	if err != nil || len(challenge) != 32 {
		oauthError(w, 400, "invalid_request")
		return
	}
	c, err := s.resolveClient(r.Context(), q.Get("client_id"))
	if err != nil || !contains(c.metadata.RedirectURIs, q.Get("redirect_uri")) {
		oauthError(w, 400, "invalid_client")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	// Some clients (including SDK 1.8's default oauth2 TokenSource) omit the
	// resource on refresh. Infer only the existing single-resource grant; never
	// infer during authorization/code exchange or override an explicit target.
	if q.Get("grant_type") == "refresh_token" && !q.Has("resource") {
		t := s.refresh[sha256.Sum256([]byte(q.Get("refresh_token")))]
		if t != nil && !t.family.revoked && s.now().Before(t.family.expires) && t.family.clientID == q.Get("client_id") {
			q.Set("resource", t.family.resource)
		}
	}
	conn := s.byResource(q.Get("resource"))
	if conn == nil || conn.public.ClientID != "" && conn.public.ClientID != c.metadata.ID || len(s.pending) >= 32 || !s.enabled {
		oauthError(w, 400, "invalid_target")
		return
	}
	if s.callbacks.Validate(r.Context(), conn.public.ID, conn.token) != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	id := opaque()
	p := &pending{public: Authorization{ID: id, ConnectionID: conn.public.ID, ClientID: c.metadata.ID, ClientName: c.metadata.Name, MetadataURL: c.url, RedirectURI: q.Get("redirect_uri"), ComparisonCode: strings.ToUpper(opaque()[:8]), State: "pending", ExpiresAt: s.now().Add(5 * time.Minute)}, client: c, state: q.Get("state"), challenge: q.Get("code_challenge"), resource: conn.public.URL, cookie: opaque()}
	p.public.ReviewHash = reviewDigest(p)
	s.pending[id] = p
	path := "/oauth/review/" + id
	http.SetCookie(w, &http.Cookie{Name: "trove_review", Value: p.cookie, Path: path, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, path, http.StatusSeeOther)
}
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/oauth/review/")
	cookie, err := r.Cookie("trove_review")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	p := s.pending[id]
	if err != nil || p == nil || !equal(cookie.Value, p.cookie) || p.delivered {
		oauthError(w, 400, "invalid_request")
		return
	}
	if p.public.State == "pending" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><meta charset=utf-8><meta http-equiv=refresh content=3><title>Trove connection review</title><h1>Review this connection in Trove</h1><p>Client: "+html.EscapeString(p.public.ClientName)+"</p><p>Comparison code: <strong>"+html.EscapeString(p.public.ComparisonCode)+"</strong></p><p>Return address: "+html.EscapeString(p.public.RedirectURI)+"</p><p>Only approve the matching request in your native Trove window. This page does not grant access.</p>")
		return
	}
	conn := s.byResource(p.resource)
	destination, _ := url.Parse(p.public.RedirectURI)
	q := destination.Query()
	q.Set("state", p.state)
	q.Set("iss", s.config.PublicOrigin)
	if p.public.State == "approved" && conn != nil && conn.public.ClientID == p.client.metadata.ID && s.callbacks.Validate(r.Context(), conn.public.ID, conn.token) == nil {
		rawCode := opaque()
		s.codes[sha256.Sum256([]byte(rawCode))] = &code{pending: p, expires: s.now().Add(time.Minute)}
		q.Set("code", rawCode)
		p.public.State = "issued"
	} else {
		q.Set("error", "access_denied")
		p.public.State = "denied"
	}
	p.delivered = true
	p.cookie = ""
	destination.RawQuery = q.Encode()
	http.SetCookie(w, &http.Cookie{Name: "trove_review", Value: "", Path: r.URL.Path, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, destination.String(), http.StatusSeeOther)
}
func form(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return nil, false
	}
	if r.Header.Get("Authorization") != "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, 400, "invalid_request")
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(data) > 65536 {
		oauthError(w, 400, "invalid_request")
		return nil, false
	}
	q, err := url.ParseQuery(string(data))
	if err != nil || !single(q, "client_id", "client_secret", "client_assertion", "grant_type", "code", "redirect_uri", "resource", "code_verifier", "refresh_token", "token", "token_type_hint") || q.Get("client_id") == "" || q.Has("client_secret") || q.Has("client_assertion") {
		oauthError(w, 400, "invalid_request")
		return nil, false
	}
	return q, true
}
func (s *Server) exchange(w http.ResponseWriter, r *http.Request) {
	q, ok := form(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	// Some clients (including SDK 1.8's default oauth2 TokenSource) omit the
	// resource on refresh. Infer only the existing single-resource grant; never
	// infer during authorization/code exchange or override an explicit target.
	if q.Get("grant_type") == "refresh_token" && !q.Has("resource") {
		t := s.refresh[sha256.Sum256([]byte(q.Get("refresh_token")))]
		if t != nil && !t.family.revoked && s.now().Before(t.family.expires) && t.family.clientID == q.Get("client_id") {
			q.Set("resource", t.family.resource)
		}
	}
	conn := s.byResource(q.Get("resource"))
	if conn == nil || conn.public.ClientID != q.Get("client_id") || conn.public.State != "authorized" || s.callbacks.Validate(r.Context(), conn.public.ID, conn.token) != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	var f *family
	switch q.Get("grant_type") {
	case "authorization_code":
		c := s.codes[sha256.Sum256([]byte(q.Get("code")))]
		if c == nil || !s.now().Before(c.expires) || c.pending.client.metadata.ID != q.Get("client_id") || c.pending.resource != q.Get("resource") || c.pending.public.RedirectURI != q.Get("redirect_uri") {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if c.used {
			s.invalidateConnectionLocked(conn)
			_ = s.callbacks.Disconnect(r.Context(), conn.public.ID)
			oauthError(w, 400, "invalid_grant")
			return
		}
		c.used = true
		verifier := q.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(digest[:])
		if len(verifier) < 43 || len(verifier) > 128 || strings.IndexFunc(verifier, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r))
		}) >= 0 || !equal(challenge, c.pending.challenge) {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if len(conn.generations) >= 8 {
			oauthError(w, 429, "temporarily_unavailable")
			return
		}
		f = &family{allowRefresh: contains(c.pending.client.metadata.GrantTypes, "refresh_token"), clientID: q.Get("client_id"), resource: q.Get("resource"), connectionID: conn.public.ID, expires: s.now().Add(8 * time.Hour), refreshHashes: map[[32]byte]bool{}}
		conn.generations[opaque()] = f
	case "refresh_token":
		t := s.refresh[sha256.Sum256([]byte(q.Get("refresh_token")))]
		if t == nil || t.family.revoked || !s.now().Before(t.family.expires) || t.family.clientID != q.Get("client_id") || t.family.resource != q.Get("resource") {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if t.used {
			s.invalidateConnectionLocked(conn)
			_ = s.callbacks.Disconnect(r.Context(), conn.public.ID)
			oauthError(w, 400, "invalid_grant")
			return
		}
		if len(t.family.refreshHashes) >= 128 {
			t.family.revoked = true
			oauthError(w, 400, "invalid_grant")
			return
		}
		t.used = true
		f = t.family
	default:
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	access, refresh := opaque(), opaque()
	expires := s.now().Add(10 * time.Minute)
	if f.expires.Before(expires) {
		expires = f.expires
	}
	s.access[sha256.Sum256([]byte(access))] = &token{family: f, expires: expires}
	result := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(expires.Sub(s.now()).Seconds()), "scope": connectionScope}
	if f.allowRefresh {
		hash := sha256.Sum256([]byte(refresh))
		f.refreshHashes[hash] = true
		s.refresh[hash] = &token{family: f, expires: f.expires}
		result["refresh_token"] = refresh
	}
	writeJSON(w, 200, result)
}
func (s *Server) invalidateConnectionLocked(c *connection) {
	c.public.State = "disconnected"
	for _, f := range c.generations {
		f.revoked = true
	}
	for _, p := range s.pending {
		if p.public.ConnectionID == c.public.ID {
			p.public.State = "denied"
		}
	}
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	q, ok := form(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	hash := sha256.Sum256([]byte(q.Get("token")))
	t := s.refresh[hash]
	if t == nil {
		t = s.access[hash]
	}
	id := ""
	if t != nil && t.family.clientID == q.Get("client_id") {
		if c := s.connections[t.family.connectionID]; c != nil {
			s.invalidateConnectionLocked(c)
			id = c.public.ID
		}
	}
	s.mu.Unlock()
	if id != "" {
		_ = s.callbacks.Disconnect(r.Context(), id)
	}
	w.WriteHeader(200)
}
func constantEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
