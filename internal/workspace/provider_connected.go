package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

type ConnectedSharingApproval struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
type providerConnectedCommitKey struct{}
type providerConnectedRunKey struct{}

// These host-only methods bind a review to the actual authenticated recipient.
// Remote calls and imported source instructions cannot create sharing approval.
func (b *ProviderBridge) PreviewConnectedContext(ctx context.Context, target ConnectedSharingTarget, refs []ConnectedSourceReference) (ConnectedSharingPreview, error) {
	return b.sharing.Preview(ctx, b.owner, target, refs)
}
func (b *ProviderBridge) ConnectManagedReviewed(ctx context.Context, provider, account, authMode, billingMode string, pages, tools []string, approval ConnectedSharingApproval) (ProviderConnection, error) {
	valid := provider == "codex" && authMode == "siwc" && billingMode == "chatgpt_plan" || provider == "claude_code" && authMode == "vendor_owned" && (billingMode == "claude_subscription" || billingMode == "anthropic_api")
	selected := false
	for _, tool := range tools {
		if tool == "readConnectedSource" {
			selected = true
		}
	}
	if !valid || strings.TrimSpace(account) == "" || len(account) > 512 || !selected {
		return ProviderConnection{}, Fail("SCOPE_DENIED", "Choose the exact provider identity and connected-source read capability")
	}
	p, err := b.sharing.Consume(ctx, b.owner, ConnectedSharingTarget{Provider: provider, Account: account}, approval.ID, approval.Hash)
	if err != nil {
		return ProviderConnection{}, err
	}
	sources := make([]ConnectedEvidence, 0, len(p.Sources))
	for _, source := range p.Sources {
		sources = append(sources, source.Connected)
	}
	return b.connect(ctx, ProviderConnection{Provider: provider, Account: account, AuthMode: authMode, BillingMode: billingMode, AdapterVersion: "2"}, pages, tools, sources)
}
func (b *ProviderBridge) connectedWriteContext(ctx context.Context, c *providerConnection) context.Context {
	if len(c.connected) == 0 {
		return ctx
	}
	ctx = context.WithValue(ctx, commitGrantKey{}, c.execution)
	return context.WithValue(ctx, approvalSourceGrantKey{}, b.owner)
}
func (b *ProviderBridge) connectedLocal(ctx context.Context, q querier, sources []ConnectedEvidence) error {
	return b.kernel.validateExternalConnectedLocal(ctx, q, b.owner, sources)
}
func (k *Kernel) validateExternalConnectedLocal(ctx context.Context, q querier, e contracts.ServiceExecutionContext, sources []ConnectedEvidence) error {
	for _, source := range sources {
		if source.Provider == connectors.Gmail {
			return Fail("SCOPE_DENIED", "Gmail-derived context is not qualified for external AI")
		}
		if err := k.connectedEvidenceLocal(ctx, q, e, AIEvidence{Connected: &source}); err != nil {
			return err
		}
	}
	return nil
}
func (b *ProviderBridge) validateConnected(ctx context.Context, c *providerConnection) error {
	if len(c.connected) == 0 {
		return nil
	}
	if err := b.kernel.authorize(c.execution); err != nil {
		return err
	}
	if err := b.connectedLocal(ctx, b.kernel.db, c.connected); err != nil {
		return err
	}
	for _, source := range c.connected {
		if err := b.kernel.connectedContext().Revalidate(ctx, b.owner, source); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := b.kernel.authorize(c.execution); err != nil {
		return err
	}
	return b.connectedLocal(ctx, b.kernel.db, c.connected)
}

// ValidateConnectedContext is a host publication boundary, never a tool.
func (b *ProviderBridge) ValidateConnectedContext(ctx context.Context, id, token string) error {
	c, err := b.authenticate(id, token)
	if err != nil {
		return err
	}
	return b.validateConnected(ctx, c)
}
func (b *ProviderBridge) readConnectedSource(ctx context.Context, c *providerConnection, args map[string]any) (json.RawMessage, error) {
	id, ok := args["snapshotId"].(string)
	if !ok || id == "" || len(id) > 2048 || len(args) != 1 {
		return nil, Fail("INVALID_REQUEST", "Supply only the selected snapshotId")
	}
	for _, source := range c.connected {
		if source.SnapshotID != id {
			continue
		}
		// Revalidate the exact reviewed receipt, rather than minting broader authority.
		if err := b.validateConnected(ctx, c); err != nil {
			return nil, err
		}
		h := b.kernel.connectedContext()
		fresh, err := h.Read(ctx, b.owner, ConnectedSourceReference{SnapshotID: source.SnapshotID, RemoteRevision: source.RemoteRevision, ContentHash: source.ContentHash})
		if err != nil {
			return nil, err
		}
		if fresh.Connected.AccountRevision != source.AccountRevision || fresh.Connected.AccountID != source.AccountID || fresh.Connected.SelectionID != source.SelectionID {
			return nil, Fail("SCOPE_DENIED", "Connected account or selection changed")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := c.ctx.Err(); err != nil {
			return nil, err
		}
		if err := b.kernel.authorize(c.execution); err != nil {
			return nil, err
		}
		if err := b.connectedLocal(ctx, b.kernel.db, c.connected); err != nil {
			return nil, err
		}
		// Return reviewed identity with freshly authenticated text; no canonical page
		// location is invented and remote descriptions cannot select tools.
		fresh.Connected = source
		return success(fresh), nil
	}
	return nil, Fail("SCOPE_DENIED", "Source was not selected in this recipient's sharing review")
}

// Scope identity excludes ephemeral receipt and access-check timestamps so an
// unchanged source can resume only after a new exact review. Changed source,
// account, selection, recipient or remote revision necessarily changes the hash.
func ConnectedProviderScope(sources []ConnectedEvidence) []ConnectedEvidence {
	out := append([]ConnectedEvidence(nil), sources...)
	for i := range out {
		out[i].ReceiptID = ""
		out[i].CheckedAt = ""
	}
	return out
}
func (b *ProviderBridge) SaveManagedRun(ctx context.Context, id, token string, r ManagedRun) error {
	if len(r.ConnectedSources) == 0 {
		return b.kernel.SaveManagedRun(ctx, b.owner, r)
	}
	c, err := b.authenticate(id, token)
	if err != nil {
		return err
	}
	if r.ConnectionID != c.info.ID || r.Provider != c.info.Provider || r.ConnectedRecipient == nil || *r.ConnectedRecipient != (ConnectedSharingTarget{Provider: c.info.Provider, Account: c.info.Account}) || connectorHash(r.ConnectedSources) != connectorHash(c.connected) {
		return Fail("SCOPE_DENIED", "Run differs from the reviewed provider/source binding")
	}
	if err = b.validateConnected(ctx, c); err != nil {
		return err
	}
	ctx = b.connectedWriteContext(ctx, c)
	ctx = context.WithValue(ctx, providerConnectedRunKey{}, connectorHash([]any{r.ConnectionID, r.ConnectedRecipient, r.ConnectedSources}))
	return b.kernel.SaveManagedRun(ctx, b.owner, r)
}
func init() {
	RegisterCommitProjection(func(m *Mutation) error {
		sources, ok := m.Context.Value(providerConnectedCommitKey{}).([]ConnectedEvidence)
		if !ok {
			return nil
		}
		owner, ok := m.Context.Value(approvalSourceGrantKey{}).(contracts.ServiceExecutionContext)
		if !ok || m.Execution.Actor.Kind != "external_client" || m.Request.Mutation.ApprovalId == nil {
			return Fail("APPROVAL_REQUIRED", "Connected provider writes need explicit owner approval")
		}
		return m.Kernel.validateExternalConnectedLocal(m.Context, m.Tx, owner, sources)
	})
}
