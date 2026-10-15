package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

func (k *Kernel) ConnectorSnapshot(ctx context.Context, e contracts.ServiceExecutionContext, id string) (snapshot connectors.Snapshot, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	var raw string
	if err = k.db.QueryRowContext(ctx, "SELECT body_json FROM connector_snapshots WHERE id=?", id).Scan(&raw); err != nil {
		return
	}
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return
	}
	a, err := connectorAccount(ctx, k.db, snapshot.AccountID)
	if err != nil {
		return snapshot, err
	}
	if a.State != "active" || (snapshot.State != "present" && snapshot.State != "partial") {
		return snapshot, Fail("SCOPE_DENIED", "This external source is no longer active; refresh its connection")
	}
	_, err = connectors.Selected(a, snapshot.SelectionID)
	return snapshot, err
}
func (k *Kernel) StoreConnectorPreview(ctx context.Context, e contracts.ServiceExecutionContext, a connectors.Account, selectionID string, snapshot connectors.Snapshot) (connectors.Snapshot, error) {
	if snapshot.RemoteID == "" || len(snapshot.Text) > 256<<10 || len(snapshot.Content) > 8<<20 {
		return snapshot, Fail("INVALID_REQUEST", "External preview exceeds supported limits")
	}
	snapshot.ID = connectorHash([]string{a.ID, selectionID, snapshot.RemoteID})
	snapshot.AccountID = a.ID
	snapshot.SelectionID = selectionID
	snapshot.Version = 1
	snapshot.RetrievedAt = now()
	snapshot.ExternalAIAllowed = false
	if snapshot.State != "partial" && snapshot.State != "removed" && snapshot.State != "inaccessible" {
		snapshot.State = "present"
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		current, err := activeConnector(ctx, k, tx, e, a.ID, a.Revision)
		if err != nil {
			return err
		}
		if _, err = connectors.Selected(current, selectionID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO connector_snapshots(id,account_id,selection_id,refresh_id,body_json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET body_json=excluded.body_json", snapshot.ID, a.ID, selectionID, "authenticated-preview", string(marshal(snapshot)))
		return err
	})
	return snapshot, err
}

// A connector import uses the same retained base/local/incoming engine as
// portable/API imports. The provider's stable item ID and named body field are
// identities; equal titles and source ordering never establish identity.
func (k *Kernel) PreviewConnectorImport(ctx context.Context, e contracts.ServiceExecutionContext, snapshotID, revision string) (ImportPlan, error) {
	var empty ImportPlan
	snapshot, err := k.ConnectorSnapshot(ctx, e, snapshotID)
	if err != nil {
		return empty, err
	}
	if snapshot.State == "partial" {
		return empty, Fail("CONFLICT", "This source preview is incomplete; capture complete content before importing it")
	}
	if snapshot.RemoteRevision != revision {
		return empty, Fail("CONFLICT", "External source changed; inspect its fresh preview")
	}
	a, err := connectorAccount(ctx, k.db, snapshot.AccountID)
	if err != nil {
		return empty, err
	}
	raw := marshal(snapshot)
	if a.Kind == connectors.Gmail {
		p := gmailExternalContextPolicy
		ctx = context.WithValue(ctx, importExternalPolicyKey{}, &p)
	}
	fileID, err := k.storeImportBytes(ctx, "connector-source.json", raw)
	if err != nil {
		return empty, err
	}
	bodyID, sourceID := NewID(), NewID()
	blocks := []ImportBlock{{ID: bodyID, Type: "paragraph", Data: map[string]any{"richText": richImport(snapshot.Text)}}}
	sources := map[string]string{bodyID: "connector-field:body"}
	if source, parseErr := url.Parse(snapshot.URL); parseErr == nil && source.User == nil && (source.Scheme == "https" || source.Scheme == "http") && source.Host != "" {
		blocks = append(blocks, ImportBlock{ID: sourceID, Type: "paragraph", Data: map[string]any{"richText": []any{map[string]any{"text": "Original source", "link": snapshot.URL}}}})
		sources[sourceID] = "connector-field:source-url"
	}
	item := ImportItem{Locator: "source-" + connectorHash([]string{snapshot.RemoteID}) + ".json", Kind: "connector", Outcome: "adapted", Reason: "Selected external text and exact evidence retained; source instructions grant no permissions", RawFileID: fileID, Hash: importDigest(raw), Size: int64(len(raw)), PageID: NewID(), Title: snapshot.Title, IdentityKind: "external_id", ExternalID: snapshot.RemoteID, Blocks: blocks, BlockSources: sources, Provenance: map[string]any{"channel": "connector", "accountId": a.ID, "provider": a.Kind, "remoteId": snapshot.RemoteID, "remoteRevision": snapshot.RemoteRevision, "retrievedAt": snapshot.RetrievedAt, "url": snapshot.URL, "externalAiAllowed": false}}
	plan := ImportPlan{ID: NewID(), Namespace: "connector:" + a.ID, SourceSystem: "connector:" + string(a.Kind), SourceFileID: fileID, SourceName: snapshot.Title, RootID: NewID(), State: "preview", Items: []ImportItem{item}, SourceCompleteness: "partial", SourceOmissions: []string{"Only this explicitly selected source is imported. Remote permissions, execution activation and unacquired attachments are excluded."}, UnknownCompleteness: []string{"A selected remote item does not establish completeness of its account or parent collection."}, CommittedBatchIDs: []string{}, CreatedAt: now()}
	if err = k.parseImportPlan(ctx, &plan); err != nil {
		return empty, err
	}
	plan.Hash = importPlanHash(plan)
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := activeConnector(ctx, k, tx, e, a.ID, a.Revision); err != nil {
			return err
		}
		var current string
		if err := tx.QueryRowContext(ctx, "SELECT json_extract(body_json,'$.remoteRevision') FROM connector_snapshots WHERE id=?", snapshotID).Scan(&current); err != nil {
			return err
		}
		if current != revision {
			return Fail("CONFLICT", "External preview changed while preparing import")
		}
		return saveImportPlan(ctx, tx, plan)
	})
	return plan, err
}

// Native acquisition returns the existing one-use, actor-bound file ingress
// proposal. The user still chooses a destination and reviews file.attach.
func (k *Kernel) PrepareConnectorAcquired(ctx context.Context, e contracts.ServiceExecutionContext, a connectors.Account, selectionID string, file connectors.Acquired) (any, error) {
	current, err := activeConnector(ctx, k, k.db, e, a.ID, a.Revision)
	if err != nil {
		return nil, err
	}
	a = current // Policy follows canonical account kind, never a caller copy.
	if _, err := connectors.Selected(a, selectionID); err != nil {
		return nil, err
	}
	if len(file.Bytes) > 32<<20 || file.Name == "" || filepath.Base(file.Name) != file.Name || strings.ContainsAny(file.Name, "\x00/\\") || file.Name == "." || file.Name == ".." {
		return nil, Fail("INVALID_REQUEST", "Invalid or oversized acquired file")
	}
	stage, err := os.MkdirTemp(filepath.Join(k.path, ".staging"), "connector-acquire-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, file.Name)
	if err = os.WriteFile(path, file.Bytes, 0600); err != nil {
		return nil, err
	}
	handle, err := k.RegisterSelection(path)
	if err != nil {
		return nil, err
	}
	var policy *externalContextPolicy
	if a.Kind == connectors.Gmail {
		p := gmailExternalContextPolicy
		policy = &p
	}
	return prepareFileWithPolicy(ctx, k, e, map[string]any{"selectionHandle": handle}, policy, func(tx *sql.Tx, digest string) error {
		current, err := activeConnector(ctx, k, tx, e, a.ID, a.Revision)
		if err != nil {
			return err
		}
		_, err = connectors.Selected(current, selectionID)
		if err != nil {
			return err
		}
		if current.Kind == connectors.Gmail {
			return retainExternalAcquisitionSource(ctx, tx, digest, externalAcquisitionSource{Version: 1, Provider: "gmail", AccountID: current.ID, SelectionID: selectionID, RemoteID: file.RemoteID, RemoteRevision: file.RemoteRevision})
		}
		return nil
	})
}
