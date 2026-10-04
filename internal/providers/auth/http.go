package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type safeTransport struct{ base http.RoundTripper }

func (s safeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "auth.openai.com" || r.URL.User != nil {
		return nil, fail("AUTH_ENDPOINT_INVALID", "Authentication endpoint is not trusted")
	}
	response, err := s.base.RoundTrip(r)
	if err != nil {
		return nil, fail("AUTH_NETWORK", "Authentication service could not be reached")
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: 256 * 1024}
	return response, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, fail("AUTH_RESPONSE_INVALID", "Authentication response exceeds its limit")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
func (m *Manager) provider(ctx context.Context) (*oidc.Provider, error) {
	ctx = oidc.ClientContext(ctx, m.client)
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fail("AUTH_DISCOVERY_FAILED", "OpenAI authentication discovery failed")
	}
	ep := p.Endpoint()
	var discovery struct {
		JWKS   string `json:"jwks_uri"`
		Revoke string `json:"revocation_endpoint"`
	}
	if p.Claims(&discovery) != nil || ep.AuthURL != authorizeEndpoint || ep.TokenURL != tokenEndpoint || discovery.JWKS != issuer+"/.well-known/jwks.json" || discovery.Revoke != issuer+"/api/accounts/oauth/revoke" {
		return nil, fail("AUTH_DISCOVERY_FAILED", "OpenAI authentication endpoints could not be verified")
	}
	return p, nil
}

type tokenResponse struct {
	AccessToken     string          `json:"access_token"`
	RefreshToken    string          `json:"refresh_token"`
	IDToken         string          `json:"id_token"`
	Type            string          `json:"token_type"`
	Scope           *string         `json:"scope"`
	ExpiresIn       int64           `json:"expires_in"`
	EarliestRefresh json.RawMessage `json:"earliest_refresh_at"`
}
type oauthFailure struct {
	code      string
	temporary bool
}

func (e *oauthFailure) Error() string { return "OpenAI authentication request failed" }
func (m *Manager) tokenRequest(ctx context.Context, form url.Values) (tokenResponse, error) {
	r, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, fail("AUTH_RESPONSE_INVALID", "Authentication request is invalid")
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	response, err := m.client.Do(r)
	if err != nil {
		return tokenResponse{}, fail("AUTH_NETWORK", "Authentication service could not be reached; try again")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(response.Body).Decode(&body)
		return tokenResponse{}, &oauthFailure{code: body.Error, temporary: response.StatusCode >= 500 || response.StatusCode == 429}
	}
	var token tokenResponse
	if json.NewDecoder(response.Body).Decode(&token) != nil || token.AccessToken == "" || len(token.AccessToken) > 32768 || len(token.RefreshToken) > 32768 || len(token.IDToken) > 32768 || !strings.EqualFold(token.Type, "Bearer") || token.ExpiresIn < 1 || token.ExpiresIn > 86400 {
		return tokenResponse{}, fail("AUTH_RESPONSE_INVALID", "OpenAI returned an invalid credential response")
	}
	return token, nil
}
func (m *Manager) validateID(ctx context.Context, p *oidc.Provider, raw, clientID, nonce string) (Identity, error) {
	if raw == "" {
		return Identity{}, fail("AUTH_IDENTITY_INVALID", "A verified account identity was not returned")
	}
	token, err := p.VerifierContext(oidc.ClientContext(ctx, m.client), &oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}, Now: m.now}).Verify(ctx, raw)
	if err != nil || token.Subject == "" || len(token.Subject) > 512 || (nonce != "" && !equalSecret(token.Nonce, nonce)) {
		return Identity{}, fail("AUTH_IDENTITY_INVALID", "The returned account identity could not be verified")
	}
	var claims struct {
		Email           string `json:"email"`
		Name            string `json:"name"`
		AuthorizedParty string `json:"azp"`
	}
	if token.Claims(&claims) != nil || len(claims.Email) > 512 || len(claims.Name) > 512 || (claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) || (len(token.Audience) > 1 && claims.AuthorizedParty != clientID) || token.IssuedAt.After(m.now().Add(time.Minute)) {
		return Identity{}, fail("AUTH_IDENTITY_INVALID", "The returned account identity could not be verified")
	}
	return Identity{Subject: token.Subject, ClientID: clientID, Email: claims.Email, Name: claims.Name}, nil
}
func makeCredentials(token tokenResponse, identity Identity, host string, previous *credentials, now time.Time) (credentials, error) {
	c := credentials{Identity: identity, HostID: host, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, IDToken: token.IDToken, ExpiresAt: now.Add(time.Duration(token.ExpiresIn) * time.Second)}
	if token.Scope != nil {
		c.Scope = *token.Scope
	} else if previous != nil {
		c.Scope = previous.Scope
	} else {
		return c, fail("AUTH_RESPONSE_INVALID", "OpenAI did not return the granted permissions")
	}
	if len(c.Scope) > 2048 {
		return c, fail("AUTH_RESPONSE_INVALID", "Granted permissions exceed their limit")
	}
	if previous != nil && c.IDToken == "" {
		c.IDToken = previous.IDToken
	}
	// Rotating refresh responses must supply the replacement. Reusing the old
	// refresh token after successful rotation can destroy the renewable session.
	if hasScope(c.Scope, "offline_access") && c.RefreshToken == "" {
		return c, fail("AUTH_RESPONSE_INVALID", "OpenAI did not return renewable credentials")
	}
	if len(token.EarliestRefresh) > 0 && string(token.EarliestRefresh) != "null" {
		var seconds int64
		if json.Unmarshal(token.EarliestRefresh, &seconds) == nil && seconds > 0 {
			c.EarliestRefreshAt = time.Unix(seconds, 0)
		} else {
			var value string
			if json.Unmarshal(token.EarliestRefresh, &value) != nil {
				return c, fail("AUTH_RESPONSE_INVALID", "Refresh timing is invalid")
			}
			var err error
			c.EarliestRefreshAt, err = time.Parse(time.RFC3339, value)
			if err != nil {
				return c, fail("AUTH_RESPONSE_INVALID", "Refresh timing is invalid")
			}
		}
	}
	return c, nil
}
func hasScope(scopes, want string) bool {
	for _, v := range strings.Fields(scopes) {
		if v == want {
			return true
		}
	}
	return false
}
func planScope(c credentials) bool {
	return hasScope(c.Scope, "chatgpt.tokens.use.direct") && hasScope(c.Scope, "resource.invoke")
}
func terminalRefresh(code string) bool {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	}
	return false
}
