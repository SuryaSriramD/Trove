// Package connectors implements selected external accounts independently of
// Trove's inbound MCP server and managed inference providers. Adapters never
// receive a canonical workspace database or permission to mutate it.
package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Kind string

const (
	GoogleCalendar Kind = "google_calendar"
	GoogleDrive    Kind = "google_drive"
	Gmail          Kind = "gmail"
	GitHub         Kind = "github"
	Notion         Kind = "notion"
	MCP            Kind = "mcp"
)

func (k Kind) Valid() bool {
	return k == GoogleCalendar || k == GoogleDrive || k == Gmail || k == GitHub || k == Notion || k == MCP
}

type Identity struct {
	Subject  string `json:"subject"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
}
type Selection struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Query    string `json:"query,omitempty"`
	ParentID string `json:"parentId,omitempty"`
}
type Account struct {
	Version        int         `json:"version"`
	ID             string      `json:"id"`
	Kind           Kind        `json:"kind"`
	Identity       Identity    `json:"identity"`
	RegistrationID string      `json:"registrationId"`
	Revision       string      `json:"revision"`
	State          string      `json:"state"`
	Selections     []Selection `json:"selections"`
	Endpoint       string      `json:"endpoint,omitempty"`
	UpdatedAt      string      `json:"updatedAt"`
}

// Snapshot is external evidence, not a canonical page. A removed/revoked
// snapshot never authorizes deleting a previously imported canonical record.
type Snapshot struct {
	Version           int             `json:"version"`
	ID                string          `json:"id"`
	AccountID         string          `json:"accountId"`
	SelectionID       string          `json:"selectionId"`
	RemoteID          string          `json:"remoteId"`
	Kind              string          `json:"kind"`
	Title             string          `json:"title"`
	Text              string          `json:"text"`
	URL               string          `json:"url"`
	RemoteRevision    string          `json:"remoteRevision"`
	State             string          `json:"state"`
	Content           json.RawMessage `json:"content"`
	RetrievedAt       string          `json:"retrievedAt"`
	ExternalAIAllowed bool            `json:"externalAiAllowed"`
}
type RefreshPage struct {
	Items           []Snapshot
	NextCursor      string
	SyncCursor      string
	FullEnumeration bool
	Complete        bool
}
type Action struct {
	Version          int             `json:"version"`
	Kind             string          `json:"kind"`
	SelectionID      string          `json:"selectionId"`
	RemoteID         string          `json:"remoteId,omitempty"`
	ExpectedRevision string          `json:"expectedRevision,omitempty"`
	Arguments        json.RawMessage `json:"arguments"`
	Effects          []string        `json:"effects"`
}
type Proposal struct {
	Version         int      `json:"version"`
	ID              string   `json:"id"`
	AccountID       string   `json:"accountId"`
	AccountRevision string   `json:"accountRevision"`
	Identity        Identity `json:"identity"`
	Action          Action   `json:"action"`
	Hash            string   `json:"hash"`
	State           string   `json:"state"`
	CreatedAt       string   `json:"createdAt"`
	Receipt         *Receipt `json:"receipt,omitempty"`
}
type Receipt struct {
	Version        int    `json:"version"`
	ProposalID     string `json:"proposalId"`
	Outcome        string `json:"outcome"`
	RemoteID       string `json:"remoteId,omitempty"`
	RemoteRevision string `json:"remoteRevision,omitempty"`
	URL            string `json:"url,omitempty"`
	Detail         string `json:"detail"`
	RecordedAt     string `json:"recordedAt"`
}

// Secret is in-process only. Its fields are deliberately excluded from all
// service serialization, diagnostics, portable snapshots and model context.
type Secret struct {
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	ExpiresAt    time.Time `json:"-"`
}

func (Secret) String() string   { return "connector credential (redacted)" }
func (Secret) GoString() string { return "connector credential (redacted)" }

type Session struct {
	Account Account
	Token   Secret
	Client  *http.Client
}

// Adapter methods must be bounded and honor context cancellation. Prepare and
// Check are read-only. Send executes one exact reviewed action. Reconcile may
// read remote evidence; it must never resend an uncertain mutation.
type Adapter interface {
	Identity(context.Context, Session) (Identity, error)
	Discover(context.Context, Session, string) ([]Selection, string, error)
	Refresh(context.Context, Session, Selection, string) (RefreshPage, error)
	Prepare(context.Context, Session, Action) (Action, error)
	Check(context.Context, Session, Action) error
	Send(context.Context, Session, Proposal) (Receipt, error)
	Reconcile(context.Context, Session, Proposal) (Receipt, error)
}
type Acquired struct {
	RemoteID       string `json:"remoteId,omitempty"`
	Name           string `json:"name"`
	MediaType      string `json:"mediaType"`
	Bytes          []byte `json:"-"`
	RemoteRevision string `json:"remoteRevision"`
}
type Acquirer interface {
	Acquire(context.Context, Session, Selection, string, string) (Acquired, error)
}
type Previewer interface {
	Preview(context.Context, Session, Selection, string) (Snapshot, error)
}
type Failure struct {
	Code           string
	Message        string
	RetryAfter     time.Duration
	OutcomeUnknown bool
}

func (e *Failure) Error() string      { return e.Message }
func fail(code, message string) error { return &Failure{Code: code, Message: message} }
func Unknown(message string) error {
	return &Failure{Code: "UNKNOWN_OUTCOME", Message: message, OutcomeUnknown: true}
}
func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Selected(a Account, id string) (Selection, error) {
	for _, s := range a.Selections {
		if s.ID == id {
			return s, nil
		}
	}
	return Selection{}, fail("SELECTION_DENIED", "Resource is not in the selected account scope")
}
func RequireIdentity(expected, actual Identity) error {
	if expected.Subject == "" || actual.Subject != expected.Subject || actual.Provider != expected.Provider {
		return fail("ACCOUNT_CHANGED", "Remote account identity changed; reconnect and review again")
	}
	return nil
}
func ReceiptFor(p Proposal, outcome, remoteID, revision, url, detail string) Receipt {
	return Receipt{Version: 1, ProposalID: p.ID, Outcome: outcome, RemoteID: remoteID, RemoteRevision: revision, URL: url, Detail: detail, RecordedAt: utcNow()}
}
func Unsupported(kind string) error {
	return fail("UNSUPPORTED_OPERATION", fmt.Sprintf("%s is not a supported connector operation", kind))
}
