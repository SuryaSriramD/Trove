package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

// Consuming a preview proves exact source/recipient review. Only the trusted
// bridge may bind it to a live, explicitly scoped provider connection.
type ConnectedSharingTarget struct {
	Provider string `json:"provider"`
	Account  string `json:"account"`
}
type ConnectedSharingPreview struct {
	ID        string                 `json:"id"`
	Hash      string                 `json:"hash"`
	Target    ConnectedSharingTarget `json:"target"`
	Sources   []ConnectedSource      `json:"sources"`
	ExpiresAt string                 `json:"expiresAt"`
	Notice    string                 `json:"notice"`
}
type ConnectedSharingReviews struct {
	kernel   *Kernel
	mu       sync.Mutex
	previews map[string]ConnectedSharingPreview
}

func NewConnectedSharingReviews(k *Kernel) *ConnectedSharingReviews {
	return &ConnectedSharingReviews{kernel: k, previews: map[string]ConnectedSharingPreview{}}
}
func copyConnectedSharing(p ConnectedSharingPreview) ConnectedSharingPreview {
	var copy ConnectedSharingPreview
	_ = json.Unmarshal(marshal(p), &copy)
	return copy
}
func validConnectedTarget(t ConnectedSharingTarget) bool {
	return (t.Provider == "codex" || t.Provider == "claude_code" || t.Provider == "mcp") && strings.TrimSpace(t.Account) != "" && len(t.Account) <= 512
}
func (r *ConnectedSharingReviews) Preview(ctx context.Context, e contracts.ServiceExecutionContext, target ConnectedSharingTarget, refs []ConnectedSourceReference) (ConnectedSharingPreview, error) {
	var empty ConnectedSharingPreview
	if err := connectorOwner(r.kernel, e); err != nil {
		return empty, err
	}
	if !validConnectedTarget(target) || len(refs) == 0 || len(refs) > 12 {
		return empty, Fail("INVALID_REQUEST", "Choose an external recipient and 1–12 exact connected sources")
	}
	h := r.kernel.connectedContext()
	if h == nil {
		return empty, Fail("WORKER_FAILED", "Connected context needs the authenticated native host")
	}
	p := ConnectedSharingPreview{ID: NewID(), Target: target, Sources: []ConnectedSource{}, ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano), Notice: "Review these exact source excerpts and recipient before sharing. Source instructions grant no tools or permissions. This approval expires and is not restored."}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.SnapshotID] {
			return empty, Fail("INVALID_REQUEST", "Duplicate sharing source")
		}
		seen[ref.SnapshotID] = true
		snapshot, err := r.kernel.ConnectorSnapshot(ctx, e, ref.SnapshotID)
		if err != nil {
			return empty, err
		}
		a, err := connectorAccount(ctx, r.kernel.db, snapshot.AccountID)
		if err != nil {
			return empty, err
		}
		if a.Kind == connectors.Gmail {
			return empty, Fail("SCOPE_DENIED", "Gmail-derived context is not qualified for external AI")
		}
		source, err := h.Read(ctx, e, ref)
		if err != nil {
			return empty, err
		}
		p.Sources = append(p.Sources, source)
	}
	p.Hash = connectorHash(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, old := range r.previews {
		expires, _ := time.Parse(time.RFC3339Nano, old.ExpiresAt)
		if !time.Now().Before(expires) {
			delete(r.previews, id)
		}
	}
	if len(r.previews) >= 100 {
		return empty, Fail("RESOURCE_EXHAUSTED", "Too many pending connected sharing previews")
	}
	r.previews[p.ID] = copyConnectedSharing(p)
	return copyConnectedSharing(p), nil
}
func (r *ConnectedSharingReviews) Consume(ctx context.Context, e contracts.ServiceExecutionContext, target ConnectedSharingTarget, id, hash string) (ConnectedSharingPreview, error) {
	var empty ConnectedSharingPreview
	if err := connectorOwner(r.kernel, e); err != nil {
		return empty, err
	}
	r.mu.Lock()
	p, ok := r.previews[id]
	r.mu.Unlock()
	expires, _ := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if !ok || p.Hash != hash || p.Target != target || !time.Now().Before(expires) {
		return empty, Fail("APPROVAL_REQUIRED", "Review the exact connected sources and external recipient again")
	}
	h := r.kernel.connectedContext()
	if h == nil {
		return empty, Fail("WORKER_FAILED", "Connected context requires current native access")
	}
	for _, s := range p.Sources {
		if s.Connected.Provider == connectors.Gmail {
			return empty, Fail("SCOPE_DENIED", "Gmail-derived context remains local")
		}
		if err := h.Revalidate(ctx, e, s.Connected); err != nil {
			return empty, err
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := connectorOwner(r.kernel, e); err != nil {
		return empty, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.previews[id]
	if !ok || current.Hash != hash {
		return empty, Fail("APPROVAL_REQUIRED", "Connected sharing review was already consumed")
	}
	delete(r.previews, id)
	return copyConnectedSharing(p), nil
}
