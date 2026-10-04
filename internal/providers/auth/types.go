// Package auth owns Trove's Sign in with ChatGPT registrations. Tokens never
// cross the renderer boundary and never enter workspace files or logs.
package auth

import (
	"context"
	"errors"
	"time"
)

const issuer = "https://auth.openai.com"
const authorizeEndpoint = issuer + "/api/accounts/authorize"
const tokenEndpoint = issuer + "/api/accounts/oauth/token"
const resource = "https://api.openai.com/v1"
const scope = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
const dynamicClient = "dynamic_agent_client"

var ErrSecretMissing = errors.New("credential record not found")
var ErrUnsupported = errors.New("protected credential storage is unavailable on this platform")

// Error contains only locally selected messages, never endpoint bodies/tokens.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string        { return e.Message }
func fail(code, message string) error { return &Error{code, message} }

type Identity struct {
	RegistrationID string `json:"registrationId"`
	Subject        string `json:"subject"`
	ClientID       string `json:"clientId"`
	Email          string `json:"email"`
	Name           string `json:"name"`
}
type Registration struct {
	ID             string    `json:"id"`
	Identity       Identity  `json:"identity"`
	State          string    `json:"state"`
	PlanAuthorized bool      `json:"planAuthorized"`
	ExpiresAt      time.Time `json:"expiresAt"`
}
type Attempt struct {
	ID             string    `json:"id"`
	RegistrationID string    `json:"registrationId"`
	State          string    `json:"state"`
	Message        string    `json:"message"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// Token is for the trusted in-process runtime only. JSON serialization cannot
// expose its bearer value even if a future caller accidentally marshals it.
type Token struct {
	Value     string    `json:"-"`
	Identity  Identity  `json:"identity"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (Token) String() string   { return "SIWC access token (redacted)" }
func (Token) GoString() string { return "SIWC access token (redacted)" }

type SignOutResult struct {
	RemoteRevoked bool `json:"remoteRevoked"`
	LocalCleared  bool `json:"localCleared"`
}

// SecretStore is injected only by package tests; NewManager always uses the OS
// credential backend. The record is bound to the host and registration identity.
type SecretStore interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}
type credentials struct {
	Identity          Identity  `json:"identity"`
	HostID            string    `json:"hostId"`
	AccessToken       string    `json:"accessToken"`
	RefreshToken      string    `json:"refreshToken"`
	IDToken           string    `json:"idToken"`
	Scope             string    `json:"scope"`
	ExpiresAt         time.Time `json:"expiresAt"`
	EarliestRefreshAt time.Time `json:"earliestRefreshAt"`
}
