package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

type p1State struct {
	Base                                           State `json:"base"`
	Formula, View, LinkedPage, Rule, Embed, Synced string
	ReceiptID, EffectID                            string
}

// RunP1 is invoked from the real desktop lifecycle with networking denied by
// the qualification runner. Only public, guarded services create fixture data.
func RunP1(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, step, statePath string) Result {
	r := Result{Step: step, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Checks: []string{}}
	basePath := statePath + ".base"
	var s p1State
	var err error
	switch step {
	case "p1-create":
		if _, err = os.Stat(statePath); !os.IsNotExist(err) {
			r.Error = "refusing to overwrite P1 fixture"
			return r
		}
		base := Run(ctx, k, e, "create", basePath)
		if !base.Passed {
			return base
		}
		s = p1State{Base: base.State, Formula: workspace.NewID(), View: workspace.NewID(), LinkedPage: workspace.NewID(), Rule: workspace.NewID(), Embed: workspace.NewID(), Synced: workspace.NewID()}
		cfg := map[string]any{"sourceType": "qualification.formula", "sourceVersion": 1, "readableText": "Exact decimal", "reason": "unsupported_behavior", "raw": map[string]any{"originalExpression": "0.1 + 0.2", "translatedExpression": "0.1 + 0.2", "importedCachedValue": "captured 999"}}
		err = batch(ctx, k, e, []map[string]any{
			{"kind": "page.create", "pageId": s.LinkedPage, "title": "P1 linked gallery"},
			{"kind": "collection.addProperty", "collectionId": s.Base.CollectionID, "propertyId": s.Formula, "name": "Exact decimal", "type": "formula", "schemaVersion": 1, "config": cfg},
			{"kind": "view.save", "viewId": s.View, "collectionId": s.Base.CollectionID, "owningPageId": s.LinkedPage, "name": "Linked gallery", "type": "gallery", "schemaVersion": 1, "config": map[string]any{"coverPropertyId": nil, "cardSize": "medium"}, "query": map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": []string{s.Base.PropertyID, s.Formula}, "groupByPropertyId": nil, "subgroupByPropertyId": nil}},
			{"kind": "block.insert", "block": map[string]any{"blockId": s.Embed, "pageId": s.Base.PageID, "type": "embed", "schemaVersion": 1, "data": map[string]any{"url": "https://example.test/offline", "provider": "qualification", "sandboxPolicy": "no_scripts_no_network", "offlineText": "P1OfflineEvidence retained card"}}},
			{"kind": "block.insert", "block": map[string]any{"blockId": s.Synced, "pageId": s.Base.PageID, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": s.Base.BlockID}}},
		}, map[string]string{s.LinkedPage: "page", s.Formula: "property", s.View: "view", s.Embed: "block", s.Synced: "block"})
		if err == nil {
			rule := workspace.AutomationRule{ID: s.Rule, Name: "Offline record change", Enabled: true, AllowedOrigins: []string{"user"}, Trigger: workspace.AutomationTrigger{Type: "event", CollectionID: s.Base.CollectionID}, Actions: []map[string]any{{"kind": "page.create", "pageId": "$id:note", "parentPageId": s.LinkedPage, "title": "P1 committed automation effect"}}}
			err = batch(ctx, k, e, []map[string]any{{"kind": "automation.save", "pageId": s.LinkedPage, "rule": rule}}, nil)
		}
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "page.update", "pageId": s.Base.RecordPageID, "title": "P1 shared record edit"}}, nil)
		}
		if err == nil {
			err = k.RunAutomationTick(ctx, e, time.Now())
		}
		if err == nil {
			var history map[string]any
			history, err = dispatch(ctx, k, e, "automationHistory", map[string]any{"pageId": s.LinkedPage})
			if err == nil {
				for _, raw := range history["runs"].([]any) {
					run := raw.(map[string]any)
					if run["status"] == "succeeded" {
						if s.ReceiptID != "" {
							err = fmt.Errorf("duplicate initial automation receipt")
							break
						}
						s.ReceiptID, _ = run["id"].(string)
						s.EffectID, _ = run["commands"].([]any)[0].(map[string]any)["pageId"].(string)
					}
				}
				if s.ReceiptID == "" || s.EffectID == "" {
					err = fmt.Errorf("initial automation did not commit")
				}
			}
		}
		if err == nil {
			var raw []byte
			raw, err = json.MarshalIndent(s, "", "  ")
			if err == nil {
				err = os.WriteFile(statePath, raw, 0600)
			}
		}
	case "p1-verify":
		var raw []byte
		raw, err = os.ReadFile(statePath)
		if err == nil {
			err = json.Unmarshal(raw, &s)
		}
	default:
		err = fmt.Errorf("unknown P1 step %q", step)
	}
	r.State = s.Base
	if err == nil {
		err = verifyP1(ctx, k, e, s)
	}
	if err == nil && step == "p1-verify" {
		backup := filepath.Join(filepath.Dir(statePath), "p1-backup-"+workspace.NewID())
		var manifest workspace.BackupManifest
		manifest, err = k.CreateBackup(ctx, e, backup)
		if err == nil {
			restoredPath := filepath.Join(filepath.Dir(statePath), "p1-restored-"+workspace.NewID())
			_, err = workspace.RestoreBackupReviewed(ctx, backup, restoredPath, manifest)
			if err == nil {
				var restored *workspace.Kernel
				restored, err = workspace.Open(restoredPath)
				if err == nil {
					defer restored.Close()
					err = verifyP1(ctx, restored, restored.OwnerContext(), s)
					if err == nil {
						var before, after map[string]any
						before, err = snapshot(ctx, k, e)
						if err == nil {
							after, err = snapshot(ctx, restored, restored.OwnerContext())
						}
						if err == nil {
							for _, field := range []string{"pages", "blocks", "collections", "properties", "records", "views", "files", "importMappings"} {
								a, _ := json.Marshal(before[field])
								b, _ := json.Marshal(after[field])
								if !bytes.Equal(a, b) {
									err = fmt.Errorf("P1 restore changed %s", field)
									break
								}
							}
						}
					}
				}
			}
		}
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Passed = true
	r.Checks = []string{"canonical linked gallery shares edited record identity", "formula computes exact 0.3 separately from captured source 999", "advanced reusable content and inert card survive offline", "keyword search includes retained offline card", "automation receipt prevents duplicate effects across repeated ticks", "managed attachment hash preserved"}
	if step == "p1-verify" {
		r.Checks = append(r.Checks, "separate process restart and native backup restore preserve P1 objects and effects")
	}
	return r
}

func verifyP1(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s p1State) error {
	// Check durability before maintenance, so replay cannot conceal lost data.
	if err := verifyP1Contents(ctx, k, e, s); err != nil {
		return err
	}
	if err := k.RunAutomationTick(ctx, e, time.Now()); err != nil {
		return err
	}
	return verifyP1Contents(ctx, k, e, s)
}

func verifyP1Contents(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s p1State) error {
	if k.ID() != s.Base.WorkspaceID {
		return fmt.Errorf("P1 workspace identity changed")
	}
	snap, err := snapshot(ctx, k, e)
	if err != nil {
		return err
	}
	effects := 0
	for _, item := range snap["pages"].([]any) {
		p := item.(map[string]any)
		if p["parentPageId"] == s.LinkedPage && p["title"] == "P1 committed automation effect" {
			effects++
			if p["id"] != s.EffectID {
				return fmt.Errorf("automation effect identity changed")
			}
		}
	}
	if effects != 1 {
		return fmt.Errorf("P1 automation effects=%d, want 1", effects)
	}
	viewFound := false
	for _, item := range snap["views"].([]any) {
		view := item.(map[string]any)
		if view["id"] == s.View {
			viewFound = view["type"] == "gallery" && view["owningPageId"] == s.LinkedPage && view["collectionId"] == s.Base.CollectionID
		}
	}
	if !viewFound {
		return fmt.Errorf("linked gallery type, placement or collection changed")
	}
	for _, id := range []string{s.Embed, s.Synced, s.Base.BlockID} {
		found := false
		for _, item := range snap["blocks"].([]any) {
			b := item.(map[string]any)
			if b["id"] == id {
				found = true
				if b["pageId"] != s.Base.PageID {
					return fmt.Errorf("advanced/source block ownership changed")
				}
				d := b["data"].(map[string]any)
				if id == s.Base.BlockID {
					text, _ := d["richText"].([]any)
					if b["type"] != "paragraph" || len(text) != 1 || text[0].(map[string]any)["text"] != "Persistent notes — తెలుగు 中文 العربية 🌱" {
						return fmt.Errorf("canonical reusable source content changed")
					}
				}
				if id == s.Embed && (b["type"] != "embed" || d["offlineText"] != "P1OfflineEvidence retained card" || d["sandboxPolicy"] != "no_scripts_no_network") {
					return fmt.Errorf("offline card changed")
				}
				if id == s.Synced && (b["type"] != "synced_block" || d["contentSourceId"] != s.Base.BlockID) {
					return fmt.Errorf("reusable source changed")
				}
			}
		}
		if !found {
			return fmt.Errorf("missing P1 block %s", id)
		}
	}
	q, err := dispatch(ctx, k, e, "queryRecords", map[string]any{"collectionId": s.Base.CollectionID, "viewId": s.View, "limit": 10})
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(q)
	ids, _ := q["recordIds"].([]any)
	if len(ids) != 1 || ids[0] != s.Base.RecordID || !bytes.Contains(raw, []byte(s.Base.RecordPageID)) {
		return fmt.Errorf("linked gallery lost canonical record: %s", raw)
	}
	page, err := dispatch(ctx, k, e, "readPage", map[string]any{"pageId": s.Base.RecordPageID})
	if err != nil {
		return err
	}
	if page["page"].(map[string]any)["title"] != "P1 shared record edit" {
		return fmt.Errorf("shared record edit lost")
	}
	d, err := dispatch(ctx, k, e, "databaseDerived", map[string]any{"collectionId": s.Base.CollectionID, "at": "2026-09-30T20:00:00Z", "timezone": "Asia/Kolkata"})
	if err != nil {
		return err
	}
	matched := false
	for _, v := range d["values"].(map[string]any)[s.Base.RecordID].([]any) {
		cell := v.(map[string]any)
		if cell["propertyId"] == s.Formula {
			value, _ := cell["value"].(map[string]any)
			matched = value["decimal"] == "0.3" && cell["importedCachedValue"] == "captured 999" && cell["status"] == "computed"
		}
	}
	if !matched {
		return fmt.Errorf("P1 derived result changed: %v", d)
	}
	search, err := dispatch(ctx, k, e, "search.query", map[string]any{"query": "P1OfflineEvidence", "mode": "keyword", "limit": 10})
	if err != nil {
		return err
	}
	if len(search["hits"].([]any)) != 1 {
		return fmt.Errorf("P1 offline card not searchable")
	}
	file, meta, err := k.ReadFile(ctx, e, s.Base.FileID)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return err
	}
	if string(meta.ContentHash) != s.Base.ContentHash || hex.EncodeToString(hash.Sum(nil)) != s.Base.ContentHash {
		return fmt.Errorf("P1 managed attachment hash changed")
	}
	h, err := dispatch(ctx, k, e, "automationHistory", map[string]any{"pageId": s.LinkedPage})
	if err != nil {
		return err
	}
	runs := h["runs"].([]any)
	succeeded := 0
	for _, raw := range runs {
		run := raw.(map[string]any)
		if run["status"] == "succeeded" {
			succeeded++
			if run["id"] != s.ReceiptID {
				return fmt.Errorf("automation receipt identity changed")
			}
		}
		if run["status"] != "succeeded" && run["status"] != "skipped" {
			return fmt.Errorf("P1 automation failure: %v", run)
		}
	}
	if succeeded != 1 {
		return fmt.Errorf("P1 automation receipt changed: %v", h)
	}
	return nil
}
