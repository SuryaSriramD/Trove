package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

type p2State struct {
	Base                                                                      State
	Amount, Button, Row, RowPage, Link, Conversation, RawConversation, Review string
}

// This synthetic export deliberately contains two alternative assistant turns
// and a non-text payload. Both branches must survive, without executing or
// projecting the tool payload. This is structural evidence, not a live account.
const p2Conversation = `[{"id":"p2-branches","title":"P2 conversation branches","create_time":1700000000,"current_node":"b","mapping":{"root":{"parent":null,"children":["q"],"message":null},"q":{"parent":"root","children":["a","b"],"message":{"id":"user-turn","author":{"role":"user"},"create_time":1700000001,"content":{"parts":["Explain the aurora"]}}},"a":{"parent":"q","children":[],"message":{"id":"alpha-turn","author":{"role":"assistant"},"create_time":1700000002,"content":{"parts":["P2AuroraAlpha northern branch"]}}},"b":{"parent":"q","children":[],"message":{"id":"beta-turn","author":{"role":"assistant"},"create_time":1700000003,"content":{"parts":["P2AuroraBeta southern branch",{"tool_payload":"NEVER_EXECUTE_P2"}]}}}}}]`

// RunP2 runs through the same guarded service boundary as the desktop. It never
// substitutes synthetic model output for OCR, image or generative review proof.
func RunP2(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, step, statePath string) Result {
	r := Result{Step: step, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Checks: []string{}}
	var s p2State
	var err error
	switch step {
	case "p2-create":
		if _, err = os.Stat(statePath); !os.IsNotExist(err) {
			r.Error = "refusing to overwrite P2 fixture"
			return r
		}
		base := Run(ctx, k, e, "create", statePath+".base")
		if !base.Passed {
			return base
		}
		s = p2State{Base: base.State, Amount: workspace.NewID(), Button: workspace.NewID(), Row: workspace.NewID(), RowPage: workspace.NewID(), Link: workspace.NewID(), Review: workspace.NewID()}
		err = createP2(ctx, k, e, &s, filepath.Dir(statePath))
		if err == nil {
			var data []byte
			data, err = json.MarshalIndent(s, "", "  ")
			if err == nil {
				err = os.WriteFile(statePath, data, 0600)
			}
		}
	case "p2-verify":
		var data []byte
		data, err = os.ReadFile(statePath)
		if err == nil {
			err = json.Unmarshal(data, &s)
		}
	default:
		err = fmt.Errorf("unknown P2 step %q", step)
	}
	r.State = s.Base
	if err == nil {
		err = verifyP2(ctx, k, e, s)
	}
	if err == nil && step == "p2-verify" {
		err = restoreP2(ctx, k, e, s, filepath.Dir(statePath))
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Passed = true
	r.Checks = []string{"exact decimal chart follows canonical form and approved row-action values", "missing required form value and unconfirmed row action commit nothing", "graph link retains source block and resolves at its current revision", "display preferences persist independently of page contents", "conversation branches, roles, timestamps and exact original bytes survive", "personal review schedule requires separate activation"}
	if step == "p2-verify" {
		r.Checks = append(r.Checks, "separate process reopen and verified native restore retain all P2 assertions", "portable conversation and display contents round-trip while operational controls remain inactive")
	}
	return r
}

func decimalValue(value string) map[string]any {
	return map[string]any{"type": "number", "schemaVersion": 1, "value": map[string]any{"decimal": value, "format": "decimal"}}
}

func createP2(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s *p2State, root string) error {
	if err := batch(ctx, k, e, []map[string]any{
		{"kind": "collection.addProperty", "collectionId": s.Base.CollectionID, "propertyId": s.Amount, "name": "P2 amount", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "decimal"}},
		{"kind": "page.display.set", "pageId": s.Base.PageID, "fullWidth": true, "presentation": true},
	}, map[string]string{s.Amount: "property"}); err != nil {
		return err
	}
	cfg := workspace.P2DatabaseConfig{
		Chart:   workspace.P2ChartConfig{Aggregation: "sum", Visualization: "bar", ValuePropertyID: &s.Amount},
		Form:    workspace.P2FormConfig{Enabled: true, RequiredPropertyIDs: []string{s.Amount}},
		Buttons: []workspace.P2RowButton{{ID: s.Button, Label: "Set reviewed amount", PropertyID: s.Amount, Value: decimalValue("0.5"), ConfirmationRequired: true}},
	}
	if err := batch(ctx, k, e, []map[string]any{{"kind": "database.p2Configure", "collectionId": s.Base.CollectionID, "config": cfg}}, nil); err != nil {
		return err
	}
	missing, missingPage := workspace.NewID(), workspace.NewID()
	err := batch(ctx, k, e, []map[string]any{{"kind": "database.formSubmit", "collectionId": s.Base.CollectionID, "recordId": missing, "pageId": missingPage, "title": "Rejected missing value", "values": []any{}}}, map[string]string{missing: "record", missingPage: "page"})
	if err == nil || !strings.Contains(err.Error(), "INVALID_REQUEST") {
		return fmt.Errorf("required form field was not rejected: %v", err)
	}
	snap, err := snapshot(ctx, k, e)
	if err != nil {
		return err
	}
	for _, raw := range snap["pages"].([]any) {
		if raw.(map[string]any)["id"] == missingPage {
			return fmt.Errorf("invalid form created a page")
		}
	}
	if err = batch(ctx, k, e, []map[string]any{
		{"kind": "record.setValue", "recordId": s.Base.RecordID, "propertyId": s.Amount, "value": decimalValue("0.1")},
		{"kind": "database.formSubmit", "collectionId": s.Base.CollectionID, "recordId": s.Row, "pageId": s.RowPage, "title": "P2 canonical form row", "values": []any{map[string]any{"propertyId": s.Amount, "value": decimalValue("0.2")}}},
	}, map[string]string{s.Row: "record", s.RowPage: "page"}); err != nil {
		return err
	}
	if err = checkP2Chart(ctx, k, e, *s, "0.3"); err != nil {
		return err
	}
	action := map[string]any{"kind": "database.rowButtonRun", "collectionId": s.Base.CollectionID, "recordId": s.Row, "buttonId": s.Button, "confirmed": false}
	if err = batch(ctx, k, e, []map[string]any{action}, nil); err == nil || !strings.Contains(err.Error(), "APPROVAL_REQUIRED") {
		return fmt.Errorf("unconfirmed row action was not rejected: %v", err)
	}
	if err = checkP2Chart(ctx, k, e, *s, "0.3"); err != nil {
		return err
	}
	action["confirmed"] = true
	if err = batch(ctx, k, e, []map[string]any{action}, nil); err != nil {
		return err
	}
	plan := workspace.PersonalReviewPlan{Type: "trove.personal-review/v1", Name: "P2 review consent", Enabled: true, SourcePageIDs: []string{s.RowPage}, Schedule: workspace.AutomationTrigger{Type: "schedule", Timezone: "UTC", Frequency: "daily", Hour: 9, MissedPolicy: "skip", CatchUpLimit: 1}}
	planJSON, _ := json.Marshal(plan)
	if err = batch(ctx, k, e, []map[string]any{
		{"kind": "block.insert", "block": map[string]any{"blockId": s.Link, "pageId": s.Base.PageID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "P2 canonical form row", "link": "trove://page/" + s.RowPage}}}}},
		{"kind": "block.insert", "block": map[string]any{"blockId": s.Review, "pageId": s.Base.PageID, "type": "code", "schemaVersion": 1, "data": map[string]any{"language": "trove-review", "source": string(planJSON)}}},
	}, map[string]string{s.Link: "block", s.Review: "block"}); err != nil {
		return err
	}
	path := filepath.Join(root, "p2-conversations.json")
	if err = os.WriteFile(path, []byte(p2Conversation), 0600); err != nil {
		return err
	}
	imported, err := importP2(ctx, k, e, path)
	if err != nil {
		return err
	}
	for _, raw := range imported["items"].([]any) {
		item := raw.(map[string]any)
		if item["kind"] == "conversation" {
			s.Conversation, _ = item["pageId"].(string)
			s.RawConversation, _ = item["retainedFileId"].(string)
		}
	}
	if s.Conversation == "" || s.RawConversation == "" {
		return fmt.Errorf("conversation import did not expose source identities")
	}
	return nil
}

func importP2(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, path string) (map[string]any, error) {
	handle, err := k.RegisterSelection(path)
	if err != nil {
		return nil, err
	}
	plan, err := dispatch(ctx, k, e, "imports.preview", map[string]any{"sourceHandle": handle, "sourceNamespace": "p2-packaged-qualification"})
	if err != nil {
		return nil, err
	}
	for attempts := 0; plan["state"] != "complete" && attempts < 30; attempts++ {
		plan, err = dispatch(ctx, k, e, "imports.commit", map[string]any{"planId": plan["planId"], "planHash": plan["planHash"]})
		if err != nil {
			return nil, err
		}
	}
	if plan["state"] != "complete" {
		return nil, fmt.Errorf("P2 import did not complete")
	}
	return plan, nil
}

func checkP2Chart(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s p2State, expected string) error {
	chart, err := dispatch(ctx, k, e, "databaseChart", map[string]any{"collectionId": s.Base.CollectionID})
	if err != nil {
		return err
	}
	groups, _ := chart["groups"].([]any)
	if chart["recordCount"] != float64(2) || len(groups) != 1 {
		return fmt.Errorf("chart row inventory changed: %v", chart)
	}
	g := groups[0].(map[string]any)
	if g["decimal"] != expected || g["numericCount"] != float64(2) {
		return fmt.Errorf("chart total %v, expected exact %s", g, expected)
	}
	ids := map[string]bool{}
	for _, id := range g["recordIds"].([]any) {
		ids[id.(string)] = true
	}
	if len(ids) != 2 || !ids[s.Base.RecordID] || !ids[s.Row] {
		return fmt.Errorf("chart does not reference exactly its two canonical source records")
	}
	return nil
}

func verifyP2(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s p2State) error {
	if s.Base.WorkspaceID != k.ID() {
		return fmt.Errorf("P2 workspace identity changed")
	}
	if err := checkP2Chart(ctx, k, e, s, "0.6"); err != nil {
		return err
	}
	display, err := dispatch(ctx, k, e, "pageDisplay.get", map[string]any{"pageId": s.Base.PageID})
	if err != nil {
		return err
	}
	if display["fullWidth"] != true || display["presentation"] != true {
		return fmt.Errorf("page display preference changed")
	}
	graph, err := dispatch(ctx, k, e, "graph.query", map[string]any{"pageId": s.Base.PageID})
	if err != nil {
		return err
	}
	found := false
	for _, raw := range graph["edges"].([]any) {
		edge := raw.(map[string]any)
		if edge["target"] == s.RowPage && edge["source"] == s.Base.PageID {
			location := edge["location"].(map[string]any)
			if edge["kind"] != "explicit" || location["blockId"] != s.Link || edge["evidence"] != "Canonical link in source block" {
				return fmt.Errorf("graph lost explicit source evidence")
			}
			resolved, err := dispatch(ctx, k, e, "resolveCitation", map[string]any{"location": location})
			if err != nil {
				return err
			}
			if resolved["state"] != "current" {
				return fmt.Errorf("graph source is not current")
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("graph lost canonical form-row link")
	}
	reviews, err := dispatch(ctx, k, e, "review.list", map[string]any{})
	if err != nil {
		return err
	}
	plans, _ := reviews["plans"].([]any)
	if len(plans) != 1 || plans[0].(map[string]any)["activated"] != false {
		return fmt.Errorf("review plan activated without consent: %v", reviews)
	}
	page, err := dispatch(ctx, k, e, "readPage", map[string]any{"pageId": s.Conversation})
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(page)
	for _, want := range []string{"P2AuroraAlpha northern branch", "P2AuroraBeta southern branch", "Parent: q", "Reply: b", "assistant", "chatgpt"} {
		if !bytes.Contains(raw, []byte(want)) {
			return fmt.Errorf("conversation lost %q", want)
		}
	}
	if bytes.Contains(raw, []byte("NEVER_EXECUTE_P2")) {
		return fmt.Errorf("non-text conversation payload entered canonical page")
	}
	provenance := page["page"].(map[string]any)["provenance"].(map[string]any)["raw"].(map[string]any)["supplied"].(map[string]any)
	if provenance["currentNode"] != "b" || provenance["conversationId"] != "p2-branches" {
		return fmt.Errorf("conversation source identity or selected branch changed")
	}
	nodes := provenance["nodes"].([]any)
	if len(nodes) != 4 {
		return fmt.Errorf("conversation branch inventory changed")
	}
	seen := map[string]bool{}
	for _, raw := range nodes {
		n := raw.(map[string]any)
		id := n["id"].(string)
		seen[id] = true
		switch id {
		case "a", "b":
			wantTime := float64(1700000002)
			if id == "b" {
				wantTime++
			}
			if n["parentId"] != "q" || n["role"] != "assistant" || n["createdAt"] != wantTime {
				return fmt.Errorf("conversation reply role, parent or timestamp changed")
			}
		case "q":
			if n["role"] != "user" || n["createdAt"] != float64(1700000001) {
				return fmt.Errorf("conversation user role or timestamp changed")
			}
		}
	}
	for _, id := range []string{"root", "q", "a", "b"} {
		if !seen[id] {
			return fmt.Errorf("conversation node %s lost", id)
		}
	}
	file, _, err := k.ReadFile(ctx, e, s.RawConversation)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(p2Conversation)+1)))
	file.Close()
	if err != nil {
		return err
	}
	if string(data) != p2Conversation {
		return fmt.Errorf("original conversation bytes changed")
	}
	return nil
}

func restoreP2(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, s p2State, root string) error {
	backup := filepath.Join(root, "p2-backup-"+workspace.NewID())
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "p2-restored-"+workspace.NewID())
	if _, err = workspace.RestoreBackupReviewed(ctx, backup, path, manifest); err != nil {
		return err
	}
	restored, err := workspace.Open(path)
	if err != nil {
		return err
	}
	defer restored.Close()
	if err = verifyP2(ctx, restored, restored.OwnerContext(), s); err != nil {
		return err
	}
	archive := filepath.Join(root, "p2-portable-"+workspace.NewID()+".zip")
	if _, err = k.ExportPortable(ctx, e, archive); err != nil {
		return err
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	preserved := false
	for _, entry := range zr.File {
		if entry.Name == "trove-manifest.json" {
			f, openErr := entry.Open()
			if openErr != nil {
				zr.Close()
				return openErr
			}
			data, readErr := io.ReadAll(io.LimitReader(f, 8<<20))
			f.Close()
			if readErr != nil {
				zr.Close()
				return readErr
			}
			preserved = bytes.Contains(data, []byte("Set reviewed amount")) && bytes.Contains(data, []byte("p2DatabaseConfigs"))
		}
	}
	zr.Close()
	if !preserved {
		return fmt.Errorf("portable export lost inert operational source definitions")
	}
	portable, err := workspace.Create(filepath.Join(root, "p2-portable-workspace-"+workspace.NewID()))
	if err != nil {
		return err
	}
	defer portable.Close()
	plan, err := importP2(ctx, portable, portable.OwnerContext(), archive)
	if err != nil {
		return err
	}
	conversationPages := 0
	for _, raw := range plan["items"].([]any) {
		item := raw.(map[string]any)
		id, _ := item["pageId"].(string)
		if id == "" {
			continue
		}
		if item["title"] == "P2 conversation branches" {
			conversationPages++
			page, err := dispatch(ctx, portable, portable.OwnerContext(), "readPage", map[string]any{"pageId": id})
			if err != nil {
				return err
			}
			data, _ := json.Marshal(page)
			if !bytes.Contains(data, []byte("P2AuroraAlpha")) || !bytes.Contains(data, []byte("P2AuroraBeta")) {
				return fmt.Errorf("portable conversation branches missing")
			}
		}
	}
	if conversationPages != 1 {
		return fmt.Errorf("portable conversation count=%d, want 1", conversationPages)
	}
	snap, err := snapshot(ctx, portable, portable.OwnerContext())
	if err != nil {
		return err
	}
	for _, raw := range snap["collections"].([]any) {
		id := raw.(map[string]any)["id"]
		cfg, err := dispatch(ctx, portable, portable.OwnerContext(), "databaseP2", map[string]any{"collectionId": id})
		if err != nil {
			return err
		}
		c := cfg["config"].(map[string]any)
		if c["form"].(map[string]any)["enabled"] != false || len(c["buttons"].([]any)) != 0 {
			return fmt.Errorf("portable import enabled operational database controls")
		}
	}
	displayFound := false
	for _, raw := range snap["pages"].([]any) {
		page := raw.(map[string]any)
		if page["title"] == "Phase 1 integration" {
			displayFound = true
			display, err := dispatch(ctx, portable, portable.OwnerContext(), "pageDisplay.get", map[string]any{"pageId": page["id"]})
			if err != nil {
				return err
			}
			if display["fullWidth"] != true || display["presentation"] != true {
				return fmt.Errorf("portable display preferences lost")
			}
		}
	}
	if !displayFound {
		return fmt.Errorf("portable display page missing")
	}
	reviews, err := dispatch(ctx, portable, portable.OwnerContext(), "review.list", map[string]any{})
	if err != nil {
		return err
	}
	plans, _ := reviews["plans"].([]any)
	if len(plans) != 1 || plans[0].(map[string]any)["activated"] != false {
		return fmt.Errorf("portable review plan missing or activated")
	}
	return nil
}
