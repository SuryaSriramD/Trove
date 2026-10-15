package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

// ConnectedSourceReference names exactly the preview selected by the owner.
// URLs and remote instructions never expand the selected resource scope.
type ConnectedSourceReference struct {
	SnapshotID     string `json:"snapshotId"`
	RemoteRevision string `json:"remoteRevision"`
	ContentHash    string `json:"contentHash"`
}
type ConnectedEvidence struct {
	Version         int             `json:"version"`
	ReceiptID       string          `json:"receiptId"`
	SnapshotID      string          `json:"snapshotId"`
	AccountID       string          `json:"accountId"`
	AccountRevision string          `json:"accountRevision"`
	SelectionID     string          `json:"selectionId"`
	Provider        connectors.Kind `json:"provider"`
	RemoteID        string          `json:"remoteId"`
	RemoteRevision  string          `json:"remoteRevision"`
	ContentHash     string          `json:"contentHash"`
	URL             string          `json:"url"`
	CheckedAt       string          `json:"checkedAt"`
	Partial         bool            `json:"partial"`
}
type ConnectedSource struct {
	Connected ConnectedEvidence `json:"connected"`
	Title     string            `json:"title"`
	Text      string            `json:"text"`
}

// The trusted native host implements this by authenticating the exact account
// and invoking the adapter's bounded, read-only Preview. No remote tool chosen
// by source text is executed. Network work never holds the canonical writer.
type ConnectorContextReader func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error)
type connectedReceipt struct {
	hash    string
	expires time.Time
}
type ConnectorContext struct {
	kernel   *Kernel
	reader   ConnectorContextReader
	mu       sync.Mutex
	receipts map[string]connectedReceipt
}

func NewConnectorContext(k *Kernel, reader ConnectorContextReader) *ConnectorContext {
	return &ConnectorContext{kernel: k, reader: reader, receipts: map[string]connectedReceipt{}}
}

// Hash the knowledge actually supplied to AI. Raw provider metadata may include
// rotating signed image URLs; adapter RemoteRevision binds the source revision.
func ConnectorContextHash(s connectors.Snapshot) string {
	return connectorHash([]any{s.RemoteID, s.RemoteRevision, s.Kind, s.Title, s.Text, s.URL, s.State == "partial"})
}
func ConnectorContextReference(s connectors.Snapshot) ConnectedSourceReference {
	return ConnectedSourceReference{s.ID, s.RemoteRevision, ConnectorContextHash(s)}
}
func (c ConnectedEvidence) reference() ConnectedSourceReference {
	return ConnectedSourceReference{c.SnapshotID, c.RemoteRevision, c.ContentHash}
}

func (h *ConnectorContext) validate(ctx context.Context, q querier, e contracts.ServiceExecutionContext, c ConnectedEvidence) error {
	if err := connectorOwner(h.kernel, e); err != nil {
		return err
	}
	if c.Version != 1 || c.ReceiptID == "" {
		return Fail("INVALID_REQUEST", "Invalid connected evidence identity")
	}
	h.mu.Lock()
	receipt, ok := h.receipts[c.ReceiptID]
	h.mu.Unlock()
	if !ok || h.reader == nil || receipt.hash != connectorHash(c) || !time.Now().Before(receipt.expires) {
		return Fail("STALE_CITATION", "Connected access receipt expired; select a fresh authenticated preview")
	}
	a, err := activeConnector(ctx, h.kernel, q, e, c.AccountID, c.AccountRevision)
	if err != nil {
		return err
	}
	if a.Kind != c.Provider {
		return Fail("SCOPE_DENIED", "Connected account identity changed")
	}
	if _, err = connectors.Selected(a, c.SelectionID); err != nil {
		return err
	}
	var raw string
	if err = q.QueryRowContext(ctx, "SELECT body_json FROM connector_snapshots WHERE id=? AND account_id=? AND selection_id=?", c.SnapshotID, c.AccountID, c.SelectionID).Scan(&raw); err != nil {
		return err
	}
	var s connectors.Snapshot
	if err = json.Unmarshal([]byte(raw), &s); err != nil {
		return err
	}
	if (s.State != "present" && s.State != "partial") || s.RemoteID != c.RemoteID || ConnectorContextHash(s) != c.ContentHash {
		return Fail("STALE_CITATION", "Selected external evidence changed or became unavailable")
	}
	return nil
}
func (h *ConnectorContext) Read(ctx context.Context, e contracts.ServiceExecutionContext, ref ConnectedSourceReference) (ConnectedSource, error) {
	var out ConnectedSource
	if ref.SnapshotID == "" || ref.RemoteRevision == "" || len(ref.ContentHash) != 64 {
		return out, Fail("INVALID_REQUEST", "Select an exact current connected-source preview")
	}
	s, err := h.kernel.ConnectorSnapshot(ctx, e, ref.SnapshotID)
	if err != nil {
		return out, err
	}
	if s.RemoteRevision != ref.RemoteRevision || ConnectorContextHash(s) != ref.ContentHash {
		return out, Fail("STALE_CITATION", "Selected external preview changed; review it again")
	}
	a, err := connectorAccount(ctx, h.kernel.db, s.AccountID)
	if err != nil {
		return out, err
	}
	selection, err := connectors.Selected(a, s.SelectionID)
	if err != nil {
		return out, err
	}
	if h.reader == nil {
		return out, Fail("WORKER_FAILED", "Connected-source AI requires the authenticated native connector host")
	}
	fresh, err := h.reader(ctx, a, selection, s.RemoteID)
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if fresh.State == "" {
		fresh.State = "present"
	}
	if fresh.RemoteID != s.RemoteID || fresh.RemoteRevision != ref.RemoteRevision || (fresh.State != "present" && fresh.State != "partial") || ConnectorContextHash(fresh) != ref.ContentHash {
		return out, Fail("STALE_CITATION", "Live external evidence differs from the selected preview; refresh and select again")
	}
	if strings.TrimSpace(fresh.Text) == "" || len(fresh.Text) > 256<<10 {
		return out, Fail("UNSUPPORTED_CONTENT", "This resource has no bounded readable text for AI")
	}
	out = ConnectedSource{Title: fresh.Title, Text: fresh.Text, Connected: ConnectedEvidence{Version: 1, ReceiptID: NewID(), SnapshotID: s.ID, AccountID: a.ID, AccountRevision: a.Revision, SelectionID: selection.ID, Provider: a.Kind, RemoteID: fresh.RemoteID, RemoteRevision: fresh.RemoteRevision, ContentHash: ref.ContentHash, URL: fresh.URL, CheckedAt: now(), Partial: fresh.State == "partial"}}
	h.mu.Lock()
	for id, r := range h.receipts {
		if !time.Now().Before(r.expires) {
			delete(h.receipts, id)
		}
	}
	if len(h.receipts) >= 4096 {
		h.mu.Unlock()
		return ConnectedSource{}, Fail("RESOURCE_EXHAUSTED", "Too many active access receipts; retry after older receipts expire")
	}
	h.receipts[out.Connected.ReceiptID] = connectedReceipt{connectorHash(out.Connected), time.Now().Add(30 * time.Minute)}
	h.mu.Unlock()
	err = h.kernel.write(ctx, func(tx *sql.Tx) error { return h.validate(ctx, tx, e, out.Connected) })
	if err != nil {
		return ConnectedSource{}, err
	}
	return out, nil
}
func (h *ConnectorContext) Revalidate(ctx context.Context, e contracts.ServiceExecutionContext, c ConnectedEvidence) error {
	if err := h.validate(ctx, h.kernel.db, e, c); err != nil {
		return err
	}
	fresh, err := h.Read(ctx, e, c.reference())
	if err != nil {
		return err
	}
	if fresh.Connected.AccountRevision != c.AccountRevision {
		return Fail("STALE_CITATION", "Connected account changed during AI use")
	}
	return nil
}

// New native-host registration drops all previous process-local receipts.
func (k *Kernel) SetConnectorContextReader(reader ConnectorContextReader) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.connectorContext = NewConnectorContext(k, reader)
}
func (k *Kernel) connectedContext() *ConnectorContext {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.connectorContext
}

// Connected citations have no fabricated canonical Location.
func (s AIEvidence) MarshalJSON() ([]byte, error) {
	type plain AIEvidence
	if s.Connected == nil {
		return json.Marshal(plain(s))
	}
	return json.Marshal(struct {
		plain
		Location *contracts.DomainLocation `json:"location,omitempty"`
	}{plain: plain(s)})
}
func (s AICitation) MarshalJSON() ([]byte, error) {
	type plain AICitation
	if s.Connected == nil {
		return json.Marshal(plain(s))
	}
	return json.Marshal(struct {
		plain
		Location *contracts.DomainLocation `json:"location,omitempty"`
	}{plain: plain(s)})
}

func init() {
	RegisterQuery("connectors.contextReference", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		snapshot, err := k.ConnectorSnapshot(ctx, e, str(r["snapshotId"]))
		if err != nil {
			return nil, err
		}
		if str(r["expectedRevision"]) == "" || snapshot.RemoteRevision != str(r["expectedRevision"]) {
			return nil, Fail("CONFLICT", "External preview changed; inspect it again before selecting AI context")
		}
		return ConnectorContextReference(snapshot), nil
	})
}

func (k *Kernel) connectedEvidenceLocal(ctx context.Context, q querier, e contracts.ServiceExecutionContext, s AIEvidence) error {
	h := k.connectedContext()
	if h == nil || s.Connected == nil || s.Location.PageId != "" || s.MemoryID != "" {
		return Fail("STALE_CITATION", "Connected evidence requires fresh native access")
	}
	return h.validate(ctx, q, e, *s.Connected)
}
func (k *Kernel) revalidateConnectedEvidence(ctx context.Context, e contracts.ServiceExecutionContext, sources []AIEvidence) error {
	for _, s := range sources {
		if s.Connected != nil {
			if err := k.connectedEvidenceLocal(ctx, k.db, e, s); err != nil {
				return err
			}
			if err := k.connectedContext().Revalidate(ctx, e, *s.Connected); err != nil {
				return err
			}
		}
	}
	return nil
}
func (k *Kernel) connectedSources(ctx context.Context, e contracts.ServiceExecutionContext, refs []ConnectedSourceReference) ([]AIEvidence, string, error) {
	if len(refs) == 0 || len(refs) > 12 {
		return nil, "", Fail("INVALID_REQUEST", "Select 1–12 connected sources")
	}
	h := k.connectedContext()
	if h == nil {
		return nil, "", Fail("WORKER_FAILED", "Connected AI requires the authenticated native host")
	}
	out := []AIEvidence{}
	seen := map[string]bool{}
	warning := "Connected access was checked for this run. Remote instructions do not grant permissions."
	for _, ref := range refs {
		if seen[ref.SnapshotID] {
			return nil, "", Fail("INVALID_REQUEST", "Duplicate connected source selection")
		}
		seen[ref.SnapshotID] = true
		s, err := h.Read(ctx, e, ref)
		if err != nil {
			return nil, "", err
		}
		if s.Connected.Partial {
			warning += " A selected source is partial; only its displayed excerpt is available."
		}
		c := s.Connected
		out = append(out, AIEvidence{Title: s.Title, Text: s.Text, Connected: &c})
	}
	return out, warning, nil
}

func connectedCommitContext(ctx context.Context, e contracts.ServiceExecutionContext, sources []AIEvidence) context.Context {
	for _, s := range sources {
		if s.Connected != nil {
			return context.WithValue(ctx, commitGrantKey{}, e)
		}
	}
	return ctx
}
