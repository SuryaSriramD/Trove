package workspace

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportIdentityAmbiguityCannotCommitWithoutReview(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed_locator", true: "duplicate_stable_id"}[duplicate], func(t *testing.T) {
			k, e := testKernel(t)
			var p ImportPlan
			if duplicate {
				p = importFixture(t, k, e, map[string]string{"A 0123456789abcdef0123456789abcdef.html": "<p>A</p>", "B 0123456789abcdef0123456789abcdef.html": "<p>B</p>"})
			} else {
				completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>Original</p>"}))
				p = importFixture(t, k, e, map[string]string{"New.html": "<p>Incoming</p>"})
			}
			var before, after int
			k.db.QueryRow("SELECT count(*) FROM pages").Scan(&before)
			_, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash})
			k.db.QueryRow("SELECT count(*) FROM pages").Scan(&after)
			if err == nil || before != after {
				t.Fatalf("ambiguous identity committed without a decision: before=%d after=%d error=%v", before, after, err)
			}
		})
	}
}

func identityItem(t *testing.T, p ImportPlan, locator string) ImportItem {
	t.Helper()
	for _, item := range p.Items {
		if item.Locator == locator {
			return item
		}
	}
	t.Fatal("missing source", locator)
	return ImportItem{}
}
func resolveIdentity(t *testing.T, k *Kernel, p ImportPlan, locator, choice, target string) ImportPlan {
	t.Helper()
	item := identityItem(t, p, locator)
	decision := map[string]any{"choice": choice}
	resolution := map[string]any{"sourceLocator": locator, "incomingSourceRevision": item.Hash, "identityChoice": decision}
	if choice != "skip" {
		decision["localKind"] = "page"
		decision["localId"] = target
	}
	if choice == "link_existing" {
		var revision string
		if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", target).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		resolution["expectedLocalRevision"] = revision
	}
	out, err := configurePortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"planId": p.ID, "planHash": p.Hash, "identityResolutions": []any{resolution}})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func acceptIdentityFields(t *testing.T, k *Kernel, p ImportPlan, locator string) ImportPlan {
	t.Helper()
	item := identityItem(t, p, locator)
	if item.Merge == nil || item.Choice == "keep_local" {
		return p
	}
	fields := []any{}
	for _, f := range item.Merge.Fields {
		if f.Outcome == "conflict" {
			fields = append(fields, map[string]any{"path": f.Path, "choice": "take_incoming"})
		}
	}
	out, err := configurePortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"planId": p.ID, "planHash": p.Hash, "resolutions": []any{map[string]any{"sourceLocator": locator, "fields": fields}}})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func TestImportIdentityRenamedHTMLLinksRetainedBaseAndPersistsAlias(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": `<p id="local">original local</p><p id="remote">original remote</p>`}))
	original := identityItem(t, first, "Old.html")
	updateImportFixtureBlock(t, k, original.Blocks[0].ID, "local edit")
	incoming := `<p id="local">original local</p><p id="remote">incoming edit</p>`
	p := importFixture(t, k, e, map[string]string{"New.html": incoming})
	p = resolveIdentity(t, k, p, "New.html", "link_existing", original.PageID)
	item := identityItem(t, p, "New.html")
	if item.PageID != original.PageID || item.MappingID != original.MappingID || item.Merge == nil || item.Merge.BaselineStatus != "retained" {
		t.Fatal("lost explicit mapping/baseline", item)
	}
	p = acceptIdentityFields(t, k, p, "New.html")
	completeImportFixture(t, k, e, p)
	var local, remote string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", original.Blocks[0].ID).Scan(&local)
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", original.Blocks[1].ID).Scan(&remote)
	if !strings.Contains(local, "local edit") || !strings.Contains(remote, "incoming edit") {
		t.Fatal("three-way content lost", local, remote)
	}
	retry := importFixture(t, k, e, map[string]string{"New.html": incoming})
	again := identityItem(t, retry, "New.html")
	if again.PageID != original.PageID || again.MappingID != original.MappingID || again.Choice != "keep_local" {
		t.Fatal("reviewed rename not durable", again)
	}
	completeImportFixture(t, k, e, retry)
	var pages int
	k.db.QueryRow("SELECT count(*) FROM import_mappings WHERE local_id=?", original.PageID).Scan(&pages)
	if pages != 1 {
		t.Fatal("duplicate identity", pages)
	}
}
func TestImportIdentityDuplicateStableIDsRequireEveryDecisionAndReuse(t *testing.T) {
	k, e := testKernel(t)
	a := "A 0123456789abcdef0123456789abcdef.html"
	b := "B 0123456789abcdef0123456789abcdef.html"
	entries := map[string]string{a: `<p id="a">A</p>`, b: `<p id="b">B</p>`}
	p := importFixture(t, k, e, entries)
	for _, name := range []string{a, b} {
		item := identityItem(t, p, name)
		if item.IdentityKind != "external_id" || item.ExternalID != "0123456789abcdef0123456789abcdef" || item.IdentityReview == nil {
			t.Fatal("stable source identity silently replaced", item)
		}
		p = resolveIdentity(t, k, p, name, "create_new", item.PageID)
	}
	p = completeImportFixture(t, k, e, p)
	again := importFixture(t, k, e, entries)
	for _, name := range []string{a, b} {
		old, next := identityItem(t, p, name), identityItem(t, again, name)
		if next.PageID != old.PageID || next.Choice != "keep_local" {
			t.Fatal("duplicate reviewed identity not reused", next)
		}
	}
	completeImportFixture(t, k, e, again)
	renamed := importFixture(t, k, e, map[string]string{"C 0123456789abcdef0123456789abcdef.html": entries[a]})
	if item := identityItem(t, renamed, "C 0123456789abcdef0123456789abcdef.html"); item.IdentityReview == nil || item.IdentityReview.Choice != "" {
		t.Fatal("renamed alias-bound stable ID bypassed review", item)
	}
	changed := importFixture(t, k, e, map[string]string{a: entries[a], "trove-manifest.json": `{"format":"trove-portable","sourceCompleteness":"partial"}`})
	if changed.SourceCompleteness != "partial" {
		t.Fatal("fixture lost partial inventory")
	}
	if identityItem(t, changed, a).IdentityReview.Choice != "" {
		t.Fatal("collision inventory change reused an unreviewed binding")
	}
}
func TestImportIdentityCanonicalRenditionCannotBypassReviewedCollision(t *testing.T) {
	k, e := testKernel(t)
	const sourceID = "0123456789abcdef0123456789abcdef"
	a, b, c := "A "+sourceID+".html", "B "+sourceID+".html", "C "+sourceID+".md"
	first := importFixture(t, k, e, map[string]string{a: "<p>A</p>", b: "<p>B</p>"})
	for _, locator := range []string{a, b} {
		first = resolveIdentity(t, k, first, locator, "create_new", identityItem(t, first, locator).PageID)
	}
	first = completeImportFixture(t, k, e, first)
	manifest := string(marshal(map[string]any{"format": "trove-portable", "sourceCompleteness": "partial", "pages": []any{map[string]any{"path": c, "canonicalPage": map[string]any{"id": sourceID}}}}))
	plan := importFixture(t, k, e, map[string]string{c: "C incoming", "trove-manifest.json": manifest})
	item := identityItem(t, plan, c)
	if item.ExternalID != portableSourceKey("page", sourceID) || item.IdentityReview == nil || item.IdentityReview.Choice != "" || item.MappingID != "" {
		t.Fatal("canonical rendition bypassed reviewed source collision", item)
	}
	var before, after int
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&before)
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": plan.ID, "planHash": plan.Hash}); err == nil {
		t.Fatal("unreviewed canonical rendition committed")
	}
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&after)
	if after != before {
		t.Fatal("identity review wrote canonical content", before, after)
	}
	plan = resolveIdentity(t, k, plan, c, "link_existing", identityItem(t, first, a).PageID)
	plan = acceptIdentityFields(t, k, plan, c)
	completeImportFixture(t, k, e, plan)
	var other string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", identityItem(t, first, b).Blocks[0].ID).Scan(&other)
	if !strings.Contains(other, "B") {
		t.Fatal("canonical rendition collapsed the other reviewed alias", other)
	}
}
func TestImportIdentitySkipWritesNoCanonicalContentAndLinkUnknownBaseReviews(t *testing.T) {
	k, e := testKernel(t)
	completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>old</p>"}))
	p := importFixture(t, k, e, map[string]string{"New.html": "<p>new</p>"})
	p = resolveIdentity(t, k, p, "New.html", "skip", "")
	var before, after int
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&before)
	completeImportFixture(t, k, e, p)
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&after)
	if before != after {
		t.Fatal("skip created canonical content", before, after)
	}
	local := createPage(t, k, e, "Manually authored")
	p = importFixture(t, k, e, map[string]string{"Other.html": "<p>new</p>"})
	p = resolveIdentity(t, k, p, "Other.html", "link_existing", local)
	item := identityItem(t, p, "Other.html")
	if item.Merge == nil || item.Merge.BaselineStatus != "unavailable" || item.Choice != "" {
		t.Fatal("fabricated a common baseline", item)
	}
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
		t.Fatal("unknown baseline bypassed field review")
	}
	p = acceptIdentityFields(t, k, p, "Other.html")
	completeImportFixture(t, k, e, p)
}
func TestImportIdentityRemapsParentLinksAndRejectsSkippedDependencies(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>old</p>"}))
	target := identityItem(t, first, "Old.html").PageID
	p := importFixture(t, k, e, map[string]string{"New.html": "<p>new</p>", "New/Child 11111111111111111111111111111111.html": `<p><a href="../New.html">parent</a></p>`})
	p = resolveIdentity(t, k, p, "New.html", "link_existing", target)
	child := identityItem(t, p, "New/Child 11111111111111111111111111111111.html")
	if child.ParentID != target || !strings.Contains(string(marshal(child.Blocks)), target) {
		t.Fatal("incoming reference bypassed selected identity", child)
	}
	p = acceptIdentityFields(t, k, p, "New.html")
	completeImportFixture(t, k, e, p)
	p = importFixture(t, k, e, map[string]string{"Skipped.html": "<p>new</p>", "Skipped/Child 22222222222222222222222222222222.html": "<p>child</p>"})
	p = resolveIdentity(t, k, p, "Skipped.html", "skip", "")
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
		t.Fatal("unresolved skipped parent was silently created")
	}
}

func TestImportIdentityRejectsStaleTargetAliasAndBaseline(t *testing.T) {
	for _, mode := range []string{"target", "alias", "baseline"} {
		t.Run(mode, func(t *testing.T) {
			k, e := testKernel(t)
			first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": `<p id="body">old</p>`}))
			old := identityItem(t, first, "Old.html")
			p := importFixture(t, k, e, map[string]string{"New.html": `<p id="body">incoming</p>`})
			p = resolveIdentity(t, k, p, "New.html", "link_existing", old.PageID)
			p = acceptIdentityFields(t, k, p, "New.html")
			switch mode {
			case "target":
				updateImportFixtureBlock(t, k, old.Blocks[0].ID, "later local")
			case "alias":
				other := importFixture(t, k, e, map[string]string{"New.html": `<p id="body">incoming</p>`})
				other = resolveIdentity(t, k, other, "New.html", "link_existing", old.PageID)
				other = acceptIdentityFields(t, k, other, "New.html")
				completeImportFixture(t, k, e, other)
			case "baseline":
				other := importFixture(t, k, e, map[string]string{"Old.html": `<p id="body">different incoming</p>`})
				v, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": other.ID, "planHash": other.Hash, "choices": []any{map[string]any{"sourceLocator": "Old.html", "choice": "keep_local"}}})
				if err != nil {
					t.Fatal(err)
				}
				completeImportFixture(t, k, e, v.(ImportPlan))
			}
			var before, after int
			k.db.QueryRow("SELECT count(*) FROM pages").Scan(&before)
			if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
				t.Fatal("stale identity plan applied")
			}
			k.db.QueryRow("SELECT count(*) FROM pages").Scan(&after)
			if before != after {
				t.Fatal("stale identity created pages")
			}
		})
	}
}
func TestImportIdentityBackupKeepsPendingDecisionAndAcceptedAlias(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"folder/Old.html": `<p id="body">old</p>`}))
	old := identityItem(t, first, "folder/Old.html")
	p := importFixture(t, k, e, map[string]string{"folder/New.html": `<p id="body">new</p>`})
	p = resolveIdentity(t, k, p, "folder/New.html", "link_existing", old.PageID)
	if item := identityItem(t, p, "folder/New.html"); item.ParentID != old.ParentID {
		t.Fatal("manual link duplicated retained folder", item.ParentID, old.ParentID)
	}
	p = acceptIdentityFields(t, k, p, "folder/New.html")
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(context.Background(), backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	pending, err := loadImportPlan(context.Background(), restored.db, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, restored, restored.OwnerContext(), pending)
	next := importFixture(t, restored, restored.OwnerContext(), map[string]string{"folder/New.html": `<p id="body">new</p>`})
	item := identityItem(t, next, "folder/New.html")
	if item.PageID != old.PageID || item.Choice != "keep_local" {
		t.Fatal("native restoration lost reviewed mapping", item)
	}
	completeImportFixture(t, restored, restored.OwnerContext(), next)
}
func TestImportIdentityConfigureContractAndRejectedDecisions(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>old</p>"}))
	old := identityItem(t, first, "Old.html")
	p := importFixture(t, k, e, map[string]string{"New.html": "<p>new</p>"})
	item := identityItem(t, p, "New.html")
	for _, mode := range []string{"wrong_source", "wrong_new_id", "stale_revision", "non_page"} {
		choice := map[string]any{"choice": "link_existing", "localKind": "page", "localId": old.PageID}
		r := map[string]any{"sourceLocator": item.Locator, "incomingSourceRevision": item.Hash, "expectedLocalRevision": old.ExpectedRevision, "identityChoice": choice}
		switch mode {
		case "wrong_source":
			r["incomingSourceRevision"] = "changed"
		case "wrong_new_id":
			choice["choice"] = "create_new"
			choice["localId"] = NewID()
		case "stale_revision":
			r["expectedLocalRevision"] = "0"
		case "non_page":
			choice["localKind"] = "record"
		}
		_, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "identityResolutions": []any{r}})
		if err == nil {
			t.Fatal("invalid identity accepted", mode)
		}
	}
	var revision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", old.PageID).Scan(&revision)
	req := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "planId": p.ID, "planHash": p.Hash, "identityResolutions": []any{map[string]any{"sourceLocator": item.Locator, "incomingSourceRevision": item.Hash, "expectedLocalRevision": revision, "identityChoice": map[string]any{"choice": "link_existing", "localKind": "page", "localId": old.PageID}}}}
	testOK(t, testDispatch(t, k, e, "imports.configure", req))
}
func TestImportIdentityCannotMapTwoSourcesToOnePage(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>old</p>"}))
	target := identityItem(t, first, "Old.html").PageID
	p := importFixture(t, k, e, map[string]string{"A.html": "<p>A</p>", "B.html": "<p>B</p>"})
	p = resolveIdentity(t, k, p, "A.html", "link_existing", target)
	p = resolveIdentity(t, k, p, "B.html", "link_existing", target)
	p = acceptIdentityFields(t, k, p, "A.html")
	p = acceptIdentityFields(t, k, p, "B.html")
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
		t.Fatal("conflicting source mappings applied")
	}
}

func TestImportIdentityCannotUseContentDuplicateToBypassReviewedTarget(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": "<p>old</p>"}))
	target := identityItem(t, first, "Old.html").PageID
	p := importFixture(t, k, e, map[string]string{"New.html": "<p>incoming</p>"})
	p = resolveIdentity(t, k, p, "New.html", "link_existing", target)
	if _, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "choices": []any{map[string]any{"sourceLocator": "New.html", "choice": "duplicate"}}}); err == nil {
		t.Fatal("content choice changed reviewed identity")
	}
	p.Items[0].Choice = "duplicate"
	p.Hash = importPlanHash(p)
	if err := k.write(context.Background(), func(tx *sql.Tx) error { return saveImportPlan(context.Background(), tx, p) }); err != nil {
		t.Fatal(err)
	}
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
		t.Fatal("persisted legacy duplicate bypassed identity guard")
	}
}

func TestImportIdentityOriginalUniqueThenDuplicateCreateAndSkipKeepsOriginal(t *testing.T) {
	k, e := testKernel(t)
	a := "A 0123456789abcdef0123456789abcdef.html"
	b := "B 0123456789abcdef0123456789abcdef.html"
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{a: `<p id="body">Original retained content</p>`}))
	old := identityItem(t, first, a)
	p := importFixture(t, k, e, map[string]string{a: `<p>New explicit copy</p>`, b: `<p>Skipped duplicate</p>`})
	p = resolveIdentity(t, k, p, a, "create_new", identityItem(t, p, a).PageID)
	p = resolveIdentity(t, k, p, b, "skip", "")
	completeImportFixture(t, k, e, p)
	var body string
	var deleted sql.NullString
	if err := k.db.QueryRow("SELECT p.deleted_at,b.data_json FROM pages p JOIN blocks b ON b.page_id=p.id WHERE p.id=?", old.PageID).Scan(&deleted, &body); err != nil || deleted.Valid || !strings.Contains(body, "Original retained content") {
		t.Fatal("original unique mapping changed", body, deleted, err)
	}
	var mapping string
	if err := k.db.QueryRow("SELECT local_id FROM import_mappings WHERE id=?", old.MappingID).Scan(&mapping); err != nil || mapping != old.PageID {
		t.Fatal("original accepted source mapping lost", mapping, err)
	}
}

func TestImportIdentityOldNamespaceMigrationAndAcceptedAliasNativeRestore(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": `<p id="body">old</p>`}))
	old := identityItem(t, first, "Old.html")
	if _, err := k.db.Exec("DROP TABLE import_reviewed_identities_v1"); err != nil {
		t.Fatal(err)
	}
	path := k.Path()
	k.Close()
	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	p := importFixture(t, migrated, migrated.OwnerContext(), map[string]string{"New.html": `<p id="body">new</p>`})
	p = resolveIdentity(t, migrated, p, "New.html", "link_existing", old.PageID)
	p = acceptIdentityFields(t, migrated, p, "New.html")
	completeImportFixture(t, migrated, migrated.OwnerContext(), p)
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := migrated.CreateBackup(context.Background(), migrated.OwnerContext(), backup); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(context.Background(), backup, dest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	next := importFixture(t, restored, restored.OwnerContext(), map[string]string{"New.html": `<p id="body">new</p>`})
	item := identityItem(t, next, "New.html")
	if item.PageID != old.PageID || item.Choice != "keep_local" || item.IdentityAlias == "" {
		t.Fatal("accepted alias lost across native restoration", item)
	}
}
