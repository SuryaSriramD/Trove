// Package remote provides an explicitly enabled HTTPS authorization boundary for
// the owning desktop process. It never opens a workspace or implements tools.
package remote

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"
)

const connectionScope = "trove:connection"

type Config struct {
	PublicOrigin   string
	BindAddress    string
	AllowedOrigins []string
	Certificate    tls.Certificate
}

// Callbacks are trusted in-process hooks into the one existing ProviderBridge.
type Callbacks struct {
	RecordDecision func(context.Context, string, string, string, bool) error
	Forward        http.Handler
	Validate       func(context.Context, string, string) error
	Disconnect     func(context.Context, string) error
}
type Binding struct {
	ID      string
	Token   string `json:"-"` // internal bridge credential; never returned by remote APIs
	PageIDs []string
	Tools   []string
}
type Connection struct {
	ID       string   `json:"id"`
	URL      string   `json:"url"`
	PageIDs  []string `json:"pageIds"`
	Tools    []string `json:"tools"`
	ClientID string   `json:"clientId"`
	State    string   `json:"state"`
}
type Authorization struct {
	ID             string    `json:"id"`
	ConnectionID   string    `json:"connectionId"`
	ClientID       string    `json:"clientId"`
	ClientName     string    `json:"clientName"`
	MetadataURL    string    `json:"metadataUrl"`
	RedirectURI    string    `json:"redirectUri"`
	ComparisonCode string    `json:"comparisonCode"`
	ReviewHash     string    `json:"reviewHash"`
	State          string    `json:"state"`
	ExpiresAt      time.Time `json:"expiresAt"`
}
type Status struct {
	Enabled              bool            `json:"enabled"`
	Origin               string          `json:"origin"`
	BindAddress          string          `json:"bindAddress"`
	CertificateExpiresAt time.Time       `json:"certificateExpiresAt"`
	Connections          []Connection    `json:"connections"`
	Authorizations       []Authorization `json:"authorizations"`
	Message              string          `json:"message"`
}
type clientMetadata struct {
	ID            string   `json:"client_id"`
	Name          string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	GrantTypes    []string `json:"grant_types,omitempty"`
	ResponseTypes []string `json:"response_types,omitempty"`
	AuthMethod    string   `json:"token_endpoint_auth_method,omitempty"`
	AuthMethods   []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}
type client struct {
	metadata clientMetadata
	url      string
	expires  time.Time
}
type connection struct {
	public      Connection
	token       string
	resourceID  string
	generations map[string]*family
}
type family struct {
	clientID, resource, connectionID string
	expires                          time.Time
	revoked                          bool
	allowRefresh                     bool
	refreshHashes                    map[[32]byte]bool
}
type token struct {
	family  *family
	expires time.Time
	used    bool
}
type pending struct {
	public                             Authorization
	client                             client
	state, challenge, resource, cookie string
	delivered                          bool
}
type code struct {
	pending *pending
	expires time.Time
	used    bool
}
