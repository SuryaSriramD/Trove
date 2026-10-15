package workspace

import (
	"encoding/json"
	"reflect"
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
			t.Fatal("membership change treated as stable reorder")
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
