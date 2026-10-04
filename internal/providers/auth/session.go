package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AccessToken never chooses another account or billing route. The manager lock
// and exclusive root lock serialize rotating refresh within and across processes.
func (m *Manager) AccessToken(ctx context.Context, id string) (Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Token{}, fail("AUTH_CLOSED", "Account manager is closed")
	}
	r := m.find(id)
	if r == nil {
		return Token{}, fail("AUTH_NOT_FOUND", "Account registration was not found")
	}
	if r.State != "signed_in" {
		return Token{}, fail("AUTH_REAUTHORIZE", "Sign in to this account again")
	}
	c, err := m.readCredentials(ctx, *r)
	if errors.Is(err, ErrSecretMissing) {
		r.State = "reauthorization_required"
		r.PlanAuthorized = false
		_ = m.save()
		return Token{}, fail("AUTH_REAUTHORIZE", "Sign in to this account again")
	}
	if err != nil {
		return Token{}, fail("CREDENTIAL_STORE_FAILED", "Account credentials could not be read securely")
	}
	if !planScope(c) {
		return Token{}, fail("AUTH_PLAN_DISABLED", "ChatGPT plan usage is not authorized for this registration")
	}
	if c.ExpiresAt.After(m.now().Add(time.Minute)) {
		return Token{Value: c.AccessToken, Identity: c.Identity, ExpiresAt: c.ExpiresAt}, nil
	}
	if c.EarliestRefreshAt.After(m.now()) {
		if c.ExpiresAt.After(m.now()) {
			return Token{Value: c.AccessToken, Identity: c.Identity, ExpiresAt: c.ExpiresAt}, nil
		}
		return Token{}, fail("AUTH_REFRESH_LATER", "OpenAI has not yet allowed this session to refresh")
	}
	if c.RefreshToken == "" {
		return Token{}, fail("AUTH_REAUTHORIZE", "This account needs to sign in again")
	}
	response, err := m.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {c.Identity.ClientID}, "refresh_token": {c.RefreshToken}, "resource": {resource}})
	if err != nil {
		var oauth *oauthFailure
		if errors.As(err, &oauth) && terminalRefresh(oauth.code) {
			r.State = "reauthorization_required"
			r.PlanAuthorized = false
			r.ExpiresAt = time.Time{}
			if m.save() != nil {
				return Token{}, fail("STORAGE_FAILED", "Account revocation could not be recorded")
			}
			clearCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if m.secrets.Delete(clearCtx, id) != nil {
				return Token{}, fail("CREDENTIAL_STORE_FAILED", "Unusable credentials could not be cleared; account access is disabled")
			}
			return Token{}, fail("AUTH_REAUTHORIZE", "Authorization expired or was revoked. Sign in to this account again")
		}
		if errors.As(err, &oauth) && oauth.code == "invalid_client" {
			return Token{}, fail("AUTH_CLIENT_INVALID", "OpenAI rejected this client registration")
		}
		return Token{}, fail("AUTH_REFRESH_FAILED", "Account refresh failed temporarily; credentials were retained")
	}
	identity := c.Identity
	if response.IDToken != "" {
		p, err := m.provider(ctx)
		if err != nil {
			return Token{}, err
		}
		verified, err := m.validateID(ctx, p, response.IDToken, identity.ClientID, "")
		if err != nil || verified.Subject != identity.Subject {
			return Token{}, fail("AUTH_IDENTITY_INVALID", "Refreshed credentials returned a different or invalid account")
		}
		verified.RegistrationID = id
		identity = verified
	}
	replacement, err := makeCredentials(response, identity, m.meta.HostID, &c, m.now())
	if err != nil {
		return Token{}, err
	}
	data, _ := json.Marshal(replacement)
	if m.secrets.Put(ctx, id, data) != nil {
		return Token{}, fail("CREDENTIAL_STORE_FAILED", "Refreshed credentials could not be saved securely")
	}
	r.Identity = identity
	r.ExpiresAt = replacement.ExpiresAt
	r.PlanAuthorized = planScope(replacement)
	if err = m.save(); err != nil {
		return Token{}, err
	}
	if !r.PlanAuthorized {
		return Token{}, fail("AUTH_PLAN_DISABLED", "ChatGPT plan usage is no longer authorized for this registration")
	}
	return Token{Value: replacement.AccessToken, Identity: identity, ExpiresAt: replacement.ExpiresAt}, nil
}

func (m *Manager) SignOut(ctx context.Context, id string) (SignOutResult, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return SignOutResult{}, fail("AUTH_CLOSED", "Account manager is closed")
	}
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return SignOutResult{}, fail("AUTH_NOT_FOUND", "Account registration was not found")
	}
	for _, a := range m.attempts {
		if a.public.RegistrationID == id && (a.public.State == "waiting" || a.public.State == "exchanging") {
			a.public.State = "cancelled"
			a.public.Message = "Sign-in cancelled by sign-out"
			a.cancel()
		}
	}
	c, readErr := m.readCredentials(ctx, *r)
	r.State = "signed_out"
	r.PlanAuthorized = false
	r.ExpiresAt = time.Time{}
	if err := m.save(); err != nil {
		m.mu.Unlock()
		return SignOutResult{}, err
	}
	// Stop all future local use even when the network or caller is cancelled. The
	// captured refresh token is held only in this call for bounded revocation.
	clearCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	clearErr := m.secrets.Delete(clearCtx, id)
	cancel()
	if clearErr != nil {
		r.State = "sign_out_incomplete"
		_ = m.save()
	}
	m.mu.Unlock()
	result := SignOutResult{LocalCleared: clearErr == nil}
	if readErr == nil && c.RefreshToken != "" {
		result.RemoteRevoked = m.revoke(ctx, c)
	}
	if clearErr != nil {
		return result, fail("CREDENTIAL_STORE_FAILED", "Account access is disabled, but protected credentials could not be cleared")
	}
	return result, nil
}
func (m *Manager) revoke(ctx context.Context, c credentials) bool {
	p, err := m.provider(ctx)
	if err != nil {
		return false
	}
	var d struct {
		Endpoint string `json:"revocation_endpoint"`
	}
	if p.Claims(&d) != nil {
		return false
	}
	form := url.Values{"token": {c.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {c.Identity.ClientID}}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", d.Endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := m.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
			if resp.StatusCode < 500 {
				return false
			}
		}
		if attempt == 0 {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
		}
	}
	return false
}
