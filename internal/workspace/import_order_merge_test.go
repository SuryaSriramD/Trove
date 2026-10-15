package workspace

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func orderMergeFixture(ids []string) map[string]json.RawMessage {
	item := ImportItem{Title: "Order", BlockParents: map[string]string{}}
	for _, id := range ids {
		item.Blocks = append(item.Blocks, ImportBlock{ID: id, Type: "paragraph", Data: map[string]any{"richText": richImport(id)}})
	}
	return snapshotImportItem(&item).Fields
}
func TestImportSiblingReorderIsOneAtomicReviewedList(t *testing.T) {
	a, b, c := NewID(), NewID(), NewID()
	base, local, incoming := orderMergeFixture([]string{a, b, c}), orderMergeFixture([]string{b, a, c}), orderMergeFixture([]string{a, c, b})
	fields := mergeImportContentFields(base, local, incoming, true)
	conflicts := 0
	for _, f := range fields {
		if f.Outcome == "conflict" {
			conflicts++
			if f.Path != "/siblingOrder/root" {
				t.Fatalf("order split across independent fields: %s", f.Path)
			}
		}
	}
	if conflicts != 1 {
		t.Fatalf("wanted one atomic list conflict, got %d", conflicts)
	}
	for choice, want := range map[string][]string{"keep_local": {b, a, c}, "take_incoming": {a, c, b}} {
		item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: choice}}}}
		if err := materializeImportMerge(&item); err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, block := range item.Blocks {
			got = append(got, block.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s produced %v wanted %v", choice, got, want)
		}
		if _, ok := item.ResolvedFields["/siblingOrder/root"]; ok {
			t.Fatal("virtual list field leaked into source normalizer")
		}
	}
	if _, present := base["/blocks/"+a+"/order"]; !present {
		t.Fatal("review mutated retained baseline")
	}
}
func TestImportSiblingReorderRejectsDuplicateOrMovedMembers(t *testing.T) {
	a, b, c := NewID(), NewID(), NewID()
	fields := mergeImportContentFields(orderMergeFixture([]string{a, b, c}), orderMergeFixture([]string{b, a, c}), orderMergeFixture([]string{a, c, b}), true)
	for _, order := range [][]string{{a, a, c}, {a, b}, {a, b, NewID()}} {
		item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: "merged", Value: &ImportPresence{Present: true, Value: marshal(order)}}}}}
		if err := materializeImportMerge(&item); err == nil {
			t.Fatal("invalid complete order accepted", order)
		}
	}
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: "take_incoming"}, {Path: "/blocks/" + a + "/parent", Choice: "merged", Value: &ImportPresence{Present: true, Value: marshal(b)}}}}}
	if err := materializeImportMerge(&item); err == nil {
		t.Fatal("atomic order silently moved a block")
	}
}
func TestImportSiblingOrderDoesNotInventIdentityForMembershipChanges(t *testing.T) {
	a, b, c := NewID(), NewID(), NewID()
	base := orderMergeFixture([]string{a, b})
	local := orderMergeFixture([]string{b, a})
	incoming := orderMergeFixture([]string{a, c, b})
	for _, f := range mergeImportContentFields(base, local, incoming, true) {
		if f.Path == "/siblingOrder/root" {
			if string(f.Base.Value) != string(marshal([]string{a, b})) || string(f.Incoming.Value) != string(marshal([]string{a, c, b})) {
				t.Fatal("membership list invented source identity")
			}
		}
	}
	for _, f := range mergeImportContentFields(base, local, base, false) {
		if f.Path == "/siblingOrder/root" {
			t.Fatal("unknown normalizer fabricated a list baseline")
		}
	}
}
func TestImportSiblingOrderMergesIndependentContent(t *testing.T) {
	a, b, c := NewID(), NewID(), NewID()
	base := orderMergeFixture([]string{a, b, c})
	local := orderMergeFixture([]string{a, b, c})
	local["/blocks/"+b+"/content"] = marshal(map[string]any{"type": "paragraph", "data": map[string]any{"richText": richImport("Local text survives")}})
	incoming := orderMergeFixture([]string{c, a, b})
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: mergeImportContentFields(base, local, incoming, true)}}
	if err := materializeImportMerge(&item); err != nil {
		t.Fatal(err)
	}
	if item.Blocks[0].ID != c || item.Blocks[2].ID != b || !reflect.DeepEqual(item.Blocks[2].Data["richText"], asObject(map[string]any{"richText": richImport("Local text survives")})["richText"]) {
		// JSON normalization changes Go slice types, so compare retained bytes.
		if item.Blocks[0].ID != c || item.Blocks[2].ID != b || string(marshal(item.Blocks[2].Data["richText"])) != string(marshal(richImport("Local text survives"))) {
			t.Fatalf("independent changes lost: %+v", item.Blocks)
		}
	}
}

func TestImportSiblingMembershipChangeRequiresAtomicFinalList(t *testing.T) {
	a, b, c, inserted := NewID(), NewID(), NewID(), NewID()
	base, local, incoming := orderMergeFixture([]string{a, b, c}), orderMergeFixture([]string{b, a, c}), orderMergeFixture([]string{a, inserted, c, b})
	fields := mergeImportContentFields(base, local, incoming, true)
	atomic := false
	for _, field := range fields {
		if field.Path == "/siblingOrder/root" && field.Outcome == "conflict" {
			atomic = true
		}
		if len(field.Path) > 6 && field.Path[len(field.Path)-6:] == "/order" {
			t.Fatal("changed membership split order into scalar edits", field.Path)
		}
	}
	if !atomic {
		t.Fatal("missing complete sibling list conflict for ordinary source insertion")
	}
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: "merged", Value: &ImportPresence{Present: true, Value: marshal([]string{b, a, inserted, c})}}}}}
	if err := materializeImportMerge(&item); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, block := range item.Blocks {
		got = append(got, block.ID)
	}
	if !reflect.DeepEqual(got, []string{b, a, inserted, c}) {
		t.Fatal("reviewed complete order changed", got)
	}
}

func TestImportSiblingFinalMembershipRejectsOmittedLocalAddition(t *testing.T) {
	a, b, localOnly, incomingOnly := NewID(), NewID(), NewID(), NewID()
	fields := mergeImportContentFields(orderMergeFixture([]string{a, b}), orderMergeFixture([]string{a, localOnly, b}), orderMergeFixture([]string{a, incomingOnly, b}), true)
	for _, choice := range []string{"keep_local", "take_incoming"} {
		item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: choice}}}}
		if err := materializeImportMerge(&item); err == nil {
			t.Fatal("one side's list discarded independent added block", choice)
		}
	}
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: fields, Resolutions: []ImportFieldChoice{{Path: "/siblingOrder/root", Choice: "merged", Value: &ImportPresence{Present: true, Value: marshal([]string{a, localOnly, incomingOnly, b})}}}}}
	if err := materializeImportMerge(&item); err != nil {
		t.Fatal(err)
	}
	if len(item.Blocks) != 4 {
		t.Fatal("reviewed union lost block")
	}
}
func TestImportSiblingFinalMembershipSupportsStableParentMove(t *testing.T) {
	a, b, parent := NewID(), NewID(), NewID()
	base := orderMergeFixture([]string{a, b, parent})
	local := copyImportFields(base)
	incoming := orderMergeFixture([]string{a, parent, b})
	incoming["/blocks/"+b+"/parent"] = marshal(parent)
	incoming["/blocks/"+b+"/order"] = marshal(0)
	local["/blocks/"+a+"/content"] = marshal(map[string]any{"type": "paragraph", "data": map[string]any{"richText": richImport("Unrelated local correction")}})
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: mergeImportContentFields(base, local, incoming, true)}}
	if err := materializeImportMerge(&item); err != nil {
		t.Fatal(err)
	}
	if len(item.Blocks) != 3 || item.BlockParents[b] != parent || !strings.Contains(string(marshal(item.Blocks[0].Data)), "Unrelated local correction") {
		t.Fatal("parent move duplicated identity or lost local content", item)
	}
}
func TestImportSiblingMembershipHTMLReviewPersistsExactOrder(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="a">A</p><p id="b">B</p><p id="c">C</p>`}))
	item := first.Items[0]
	a, b, c := item.Blocks[0].ID, item.Blocks[1].ID, item.Blocks[2].ID
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.move", "blockId": b, "pageId": item.PageID, "parentBlockId": nil}}, "page", item.PageID, "block", a, "block", b, "block", c))
	updateImportFixtureBlock(t, k, a, "Local correction")
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="a">A</p><p id="new">New source block</p><p id="c">C</p><p id="b">B</p>`})
	var inserted string
	for id, source := range preview.Items[0].BlockSources {
		if source == "html-id:new" {
			inserted = id
		}
	}
	if inserted == "" {
		t.Fatal("missing stable source insertion")
	}
	fields := []any{map[string]any{"path": "/siblingOrder/root", "choice": "merged", "value": map[string]any{"present": true, "value": []string{b, a, inserted, c}}}}
	preview = configureMergeFixture(t, k, preview, fields)
	done := completeImportFixture(t, k, e, preview)
	rows, err := k.db.Query("SELECT id FROM blocks WHERE page_id=? ORDER BY sort_key", item.PageID)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		got = append(got, id)
	}
	rows.Close()
	if !reflect.DeepEqual(got, []string{b, a, inserted, c}) {
		t.Fatal("durable order differs from reviewed list", got)
	}
	var raw string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", a).Scan(&raw)
	if !strings.Contains(raw, "Local correction") {
		t.Fatal("independent correction lost")
	}
	if _, err = commitPortableImport(context.Background(), k, e, map[string]any{"planId": done.ID, "planHash": done.Hash}); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", item.PageID).Scan(&count)
	if count != 4 {
		t.Fatal("retry duplicated inserted block")
	}
}
