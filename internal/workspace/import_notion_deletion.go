package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
)

// Direct evidence proves only this object. It is never an inventory of a root,
// workspace or collection, and cannot authorize deletion of an omitted child.
type ImportSourceDeletionProof struct {
	Version    int    `json:"version"`
	CaptureID  string `json:"captureId"`
	ObjectKind string `json:"objectKind"`
	ObjectID   string `json:"objectId"`
	RootScope  string `json:"rootScope"`
}

func notionCaptureRootScope(s NotionCapture) string {
	roots := append([]string{}, s.Roots...)
	sort.Strings(roots)
	return "notion-selected-roots-v1:" + importDigest(marshal(roots))
}
func notionObjectScope(kind, id string) string { return "notion-object-presence-v1:" + kind + ":" + id }
func notionTrashStatus(response map[string]any) (trashed, known bool, err error) {
	for _, key := range []string{"in_trash", "archived"} {
		raw, present := response[key]
		if !present {
			continue
		}
		value, ok := raw.(bool)
		if !ok {
			return false, false, Fail("INVALID_REQUEST", "Notion trash status must be a boolean; cursor retained")
		}
		if known && value != trashed {
			return false, false, Fail("INVALID_REQUEST", "Notion trash status is contradictory; cursor retained")
		}
		trashed, known = value, true
	}
	return
}
func captureNotionDeletion(s *NotionCapture, p *ImportPlan, t notionCaptureTask, response map[string]any, raw []byte, rawID string, provenance map[string]any) (bool, error) {
	if t.Kind != "page" && t.Kind != "database" && t.Kind != "data_source" {
		return false, nil
	}
	trashed, known, err := notionTrashStatus(response)
	if err != nil {
		return false, err
	}
	if !known {
		return false, Fail("INVALID_REQUEST", "Notion object omitted its explicit trash status; cursor retained")
	}
	if notionNormalizeID(str(response["id"])) != t.ID || str(response["object"]) != t.Kind {
		return false, Fail("INVALID_REQUEST", "Notion object status has an unexpected identity or kind; cursor retained")
	}
	if !trashed {
		return false, nil
	}
	if s.APIVersion == notionCaptureVersion {
		if _, ok := response["in_trash"].(bool); !ok {
			return false, Fail("INVALID_REQUEST", "Current Notion response omitted in_trash; cursor retained")
		}
	}
	s.Objects["trashed:"+t.Kind+":"+t.ID] = notionCapturedObject{Task: t, Raw: append([]byte{}, raw...), RawFileID: rawID}
	p.Items = append(p.Items, ImportItem{Locator: "notion/trashed/" + t.Kind + "/" + t.ID, Kind: "asset", Outcome: "unsupported_but_retained", Reason: "Exact source trash response retained; existing local content requires a separate reviewed deletion", RawFileID: rawID, Hash: importDigest(raw), Size: int64(len(raw)), Provenance: provenance})
	return true, nil
}
func validateNotionDeletionProof(ctx context.Context, q querier, p *ImportPlan, item ImportItem) error {
	proof := item.SourceDeletionProof
	if proof == nil || proof.Version != 1 || p.SourceSystem != "notion_api" || item.SourceSystem != "notion_api" || item.IdentityKind != "external_id" {
		return Fail("CONFLICT", "Source deletion has no supported direct object proof")
	}
	s, err := loadNotionCapture(ctx, q, proof.CaptureID)
	if err != nil {
		return err
	}
	obj, ok := s.Objects["trashed:"+proof.ObjectKind+":"+proof.ObjectID]
	if !ok || s.State != "finished" || s.PlanID != p.ID || proof.RootScope != notionCaptureRootScope(s) || obj.Task.ID != proof.ObjectID || obj.Task.Kind != proof.ObjectKind || obj.RawFileID != item.RawFileID || importDigest(obj.Raw) != item.Hash {
		return Fail("CONFLICT", "Source deletion proof does not match this exact capture")
	}
	response := notionObject(obj.Raw)
	trashed, known, err := notionTrashStatus(response)
	external := proof.ObjectID
	if proof.ObjectKind != "page" {
		external = proof.ObjectKind + ":" + external
	}
	if err != nil || !known || !trashed || notionNormalizeID(str(response["id"])) != proof.ObjectID || str(response["object"]) != proof.ObjectKind || item.ExternalID != external || item.PageSourceScope != notionObjectScope(proof.ObjectKind, proof.ObjectID) {
		return Fail("CONFLICT", "Source deletion proof does not identify a trashed source object")
	}
	var count int
	err = q.QueryRowContext(ctx, `SELECT count(*) FROM import_mappings WHERE id=? AND local_id=? AND source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind='external_id' AND identity_value=?`, item.MappingID, item.PageID, p.SourceSystem, p.Namespace, external).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return Fail("CONFLICT", "Source deletion mapping or namespace changed")
	}
	return nil
}
func appendNotionSourceDeletions(ctx context.Context, q querier, s *NotionCapture, p *ImportPlan) error {
	for _, key := range notionSortedKeys(s.Objects) {
		obj := s.Objects[key]
		if key != "trashed:"+obj.Task.Kind+":"+obj.Task.ID {
			continue
		}
		external := obj.Task.ID
		if obj.Task.Kind != "page" {
			external = obj.Task.Kind + ":" + external
		}
		item := ImportItem{Kind: "source_deleted_page", SourceSystem: p.SourceSystem, IdentityKind: "external_id", ExternalID: external, RawFileID: obj.RawFileID, Hash: importDigest(obj.Raw), Size: int64(len(obj.Raw)), PageSourceScope: notionObjectScope(obj.Task.Kind, obj.Task.ID), Outcome: "partial", Reason: "Successful API response proves this source object is in Trash; reviewed local deletion retains its content and identity", Conflict: "Source object is explicitly trashed; keep local or review this exact deletion", SourceDeletionProof: &ImportSourceDeletionProof{Version: 1, CaptureID: s.ID, ObjectKind: obj.Task.Kind, ObjectID: obj.Task.ID, RootScope: notionCaptureRootScope(*s)}}
		var deleted bool
		err := q.QueryRowContext(ctx, `SELECT m.id,m.local_id,p.revision,p.title,p.deleted_at IS NOT NULL FROM import_mappings m JOIN pages p ON p.id=m.local_id WHERE m.source_system=? AND m.source_namespace=? AND m.external_kind='page' AND m.identity_kind='external_id' AND m.identity_value=?`, p.SourceSystem, p.Namespace, external).Scan(&item.MappingID, &item.PageID, &item.ExpectedRevision, &item.Title, &deleted)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		base, _, known, err := loadImportBaseline(ctx, q, item.MappingID)
		if err != nil {
			return err
		}
		if known && base.PageSourceScope == item.PageSourceScope && base.Fields["/exists"] == nil {
			continue
		}
		for _, other := range p.Items {
			if other.PageID != "" && other.ExternalID == external {
				return Fail("CONFLICT", "Source object changed between live and trashed responses; recapture")
			}
		}
		item.Locator = "notion/source-deletion/" + obj.Task.Kind + "/" + obj.Task.ID
		incoming := base
		incoming.Version = importNormalizerVersion
		incoming.PageSourceScope = item.PageSourceScope
		incoming.SourceRevision = item.Hash
		incoming.Fields = map[string]json.RawMessage{}
		for field, value := range base.Fields {
			if field != "/exists" {
				incoming.Fields[field] = value
			}
		}
		item.Incoming = &incoming
		baselineStatus := "unavailable"
		basePresence := ImportPresence{}
		if known && base.PageSourceScope == item.PageSourceScope && base.Fields["/exists"] != nil {
			baselineStatus = "retained"
			basePresence = importPresence(base.Fields, "/exists")
		}
		local := ImportPresence{Present: !deleted}
		if local.Present {
			local.Value = marshal(true)
		}
		item.Merge = &ImportMergeReview{ID: NewID(), Version: 1, BaselineStatus: baselineStatus, LocalDeleted: deleted, Fields: []ImportFieldDecision{{Path: "/exists", Outcome: "conflict", Base: basePresence, Local: local, Incoming: ImportPresence{}}}, Resolutions: []ImportFieldChoice{}}
		if deleted {
			item.Choice = "keep_local"
			item.Conflict = ""
		}
		if baselineStatus == "unavailable" {
			item.Reason += "; prior source existence evidence is unavailable, so deletion requires explicit baseline-unavailable review"
		}
		item.MergeGuards = map[string]string{guardKey("page", item.PageID): item.ExpectedRevision}
		for _, query := range []string{"SELECT 'collection',id,revision FROM collections WHERE owning_page_id=?", "SELECT 'record',id,revision FROM records WHERE page_id=?", "SELECT 'collection',c.id,c.revision FROM records r JOIN collections c ON c.id=r.collection_id WHERE r.page_id=?"} {
			rows, err := q.QueryContext(ctx, query, item.PageID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var kind, id, revision string
				if err = rows.Scan(&kind, &id, &revision); err != nil {
					rows.Close()
					return err
				}
				item.MergeGuards[guardKey(kind, id)] = revision
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		if p.PageDeletionOperationID == "" {
			p.PageDeletionOperationID = NewID()
		}
		p.Items = append(p.Items, item)
	}
	return nil
}
