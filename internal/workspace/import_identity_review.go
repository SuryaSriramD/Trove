package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"trove.local/phase0/contracts"
)

// A reviewed address supplements the original source identity; it never turns
// repeated external IDs into invented derived source IDs. Locator/group changes
// invalidate this evidence and require another deliberate identity decision.
type ImportIdentityReview struct {
	MappingStateHash string `json:"mappingStateHash,omitempty"`
	SourceMappingID  string `json:"sourceMappingId,omitempty"`
	Version          int    `json:"version"`
	Reason           string `json:"reason"`
	GroupHash        string `json:"groupHash"`
	NewPageID        string `json:"newPageId"`
	Choice           string `json:"choice,omitempty"`
	LocalID          string `json:"localId,omitempty"`
	MappingID        string `json:"mappingId,omitempty"`
}
type reviewedImportBinding struct {
	Version   int    `json:"version"`
	GroupHash string `json:"groupHash"`
	MappingID string `json:"mappingId"`
	PageID    string `json:"pageId"`
	RunID     string `json:"runId"`
}

func importIdentityKey(p *ImportPlan, item ImportItem) string {
	return importItemSourceSystem(p, &item) + ":" + item.IdentityKind + ":" + item.ExternalID
}
func prepareImportIdentityGroups(p *ImportPlan, canonical map[string]string) map[string][]string {
	groups := map[string][]string{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.PageID == "" {
			continue
		}
		if identity := canonical[item.Locator]; identity != "" {
			item.IdentityKind, item.ExternalID = "external_id", identity
			item.PortableIdentity = identity
		}
		if item.IdentityKind == "" {
			item.IdentityKind, item.ExternalID = importIdentity(item.Locator)
		}
		key := importIdentityKey(p, *item)
		groups[key] = append(groups[key], item.Locator)
	}
	for _, v := range groups {
		sort.Strings(v)
	}
	return groups
}
func readImportIdentityAlias(ctx context.Context, q querier, p *ImportPlan, item *ImportItem) (string, error) {
	var raw string
	err := q.QueryRowContext(ctx, "SELECT binding_json FROM import_reviewed_identities_v1 WHERE source_system=? AND source_namespace=? AND identity_kind=? AND identity_value=? AND source_locator=?", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID, item.Locator).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}
func prepareImportIdentityReview(ctx context.Context, q querier, p *ImportPlan, item *ImportItem, groups map[string][]string) (bool, error) {
	locators := groups[importIdentityKey(p, *item)]
	group := importDigest(marshal(locators))
	raw, err := readImportIdentityAlias(ctx, q, p, item)
	if err != nil {
		return false, err
	}
	item.IdentityAlias = raw
	var sourceMapping string
	err = q.QueryRowContext(ctx, "SELECT id FROM import_mappings WHERE source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind=? AND identity_value=?", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID).Scan(&sourceMapping)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if raw != "" {
		var binding reviewedImportBinding
		if json.Unmarshal([]byte(raw), &binding) != nil || binding.Version != 1 {
			return false, Fail("UNSUPPORTED_VERSION", "Unsupported reviewed import identity")
		}
		if binding.GroupHash == group {
			var revision, system, namespace, local string
			err = q.QueryRowContext(ctx, "SELECT p.revision,m.source_system,m.source_namespace,m.local_id FROM import_mappings m JOIN pages p ON p.id=m.local_id WHERE m.id=? AND m.local_kind='page'", binding.MappingID).Scan(&revision, &system, &namespace, &local)
			if err != nil || system != importItemSourceSystem(p, item) || namespace != p.Namespace || local != binding.PageID {
				return false, Fail("INTEGRITY_ERROR", "Reviewed identity mapping is unavailable or changed")
			}
			state, err := importMappingState(ctx, q, binding.MappingID)
			if err != nil {
				return false, err
			}
			item.IdentityReview = &ImportIdentityReview{SourceMappingID: sourceMapping, MappingStateHash: state, Version: 1, Reason: "Previously reviewed source identity", GroupHash: group, NewPageID: item.PageID, Choice: "link_existing", LocalID: local, MappingID: binding.MappingID}
			item.PageID, item.MappingID, item.ExpectedRevision = local, binding.MappingID, revision
			return true, nil
		}
	}
	// The known portable normalizer relates raw filename UUIDs to canonical
	// page UUIDs. That relation may reveal ambiguity, but never selects an
	// alias: only the exact reviewed address above can reuse a binding.
	relatedIdentity := item.ExternalID
	if item.IdentityKind == "external_id" && (item.Kind == "markdown" || item.Kind == "html") {
		if id, canonical := strings.CutPrefix(item.ExternalID, "trove-page:"); canonical && notionIDSuffix.MatchString(id) {
			relatedIdentity = id
		} else if notionIDSuffix.MatchString(item.ExternalID) {
			relatedIdentity = portableSourceKey("page", item.ExternalID)
		}
	}
	var priorAliases int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM import_reviewed_identities_v1 WHERE source_system=? AND source_namespace=? AND identity_kind=? AND identity_value IN (?,?)", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID, relatedIdentity).Scan(&priorAliases); err != nil {
		return false, err
	}
	reason := ""
	if len(locators) > 1 {
		reason = "The same stable source identity occurs more than once; choose each local identity explicitly"
	} else if raw != "" || priorAliases > 0 {
		reason = "The reviewed source locator or identity inventory changed; review its mapping again"
	} else if item.IdentityKind == "derived_locator" {
		var exact, prior int
		if err = q.QueryRowContext(ctx, "SELECT count(*) FROM import_mappings WHERE source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind=? AND identity_value=?", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID).Scan(&exact); err != nil {
			return false, err
		}
		if exact == 0 {
			if err = q.QueryRowContext(ctx, "SELECT count(*) FROM import_mappings WHERE source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind='derived_locator'", importItemSourceSystem(p, item), p.Namespace).Scan(&prior); err != nil {
				return false, err
			}
			if prior > 0 {
				reason = "This locator has no accepted mapping in an existing source namespace; a renamed source and a new page cannot be distinguished automatically"
			}
		}
	}
	if reason == "" {
		return false, nil
	}
	item.IdentityReview = &ImportIdentityReview{SourceMappingID: sourceMapping, Version: 1, Reason: reason, GroupHash: group, NewPageID: item.PageID}
	item.Conflict = reason
	item.Choice = ""
	item.Outcome = "partial"
	return true, nil
}
func importIdentitySkipped(item ImportItem) bool {
	return item.IdentityReview != nil && item.IdentityReview.Choice == "skip"
}
func remapReviewedImportPage(p *ImportPlan, old, next string) {
	if old == next {
		return
	}
	ids := map[string]string{old: next}
	for i := range p.Items {
		item := &p.Items[i]
		if item.PageID == old {
			item.PageID = next
		}
		if item.ParentID == old {
			item.ParentID = next
		}
		for j := range item.Blocks {
			rewriteImportBlock(item.Blocks[j].Data, ids)
		}
	}
}
func configureImportIdentities(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, p *ImportPlan, raw []any) error {
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, value := range raw {
		resolution := asObject(value)
		locator := str(resolution["sourceLocator"])
		if seen[locator] {
			return Fail("INVALID_REQUEST", "Resolve each source identity once")
		}
		seen[locator] = true
		index := -1
		for i := range p.Items {
			if p.Items[i].Locator == locator {
				index = i
				break
			}
		}
		if index < 0 {
			return Fail("NOT_FOUND", "Source identity is not in this preview")
		}
		item := &p.Items[index]
		review := item.IdentityReview
		if review == nil || review.Version != 1 {
			return Fail("INVALID_REQUEST", "A matched source cannot be remapped through content review")
		}
		if str(resolution["incomingSourceRevision"]) != item.Hash {
			return Fail("CONFLICT", "Incoming source changed after identity review")
		}
		if review.Choice != "" {
			return Fail("CONFLICT", "Identity decision is already frozen; create a new preview to change it")
		}
		choice := asObject(resolution["identityChoice"])
		action := str(choice["choice"])
		if action == "skip" {
			review.Choice = "skip"
			item.Choice = "keep_local"
			item.Conflict = ""
			item.Reason += "; explicitly skipped; original evidence retained without canonical content writes"
			continue
		}
		if action != "link_existing" && action != "create_new" || str(choice["localKind"]) != "page" {
			return Fail("INVALID_REQUEST", "Choose an existing page, the proposed new page, or skip")
		}
		target := str(choice["localId"])
		if action == "create_new" {
			if target != review.NewPageID {
				return Fail("INVALID_REQUEST", "Create-new must use the exact preview page identity")
			}
			var count int
			if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=?", target).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return Fail("CONFLICT", "Proposed new page already exists")
			}
			review.Choice, review.LocalID, review.MappingID = action, target, NewID()
			item.MappingID = review.MappingID
			// No existing page/base is fabricated. The new mapping is published only
			// after this frozen item commits.
			item.ExpectedRevision = ""
			item.Conflict = ""
			item.Choice = ""
			continue
		}
		if item.CollectionID != "" || item.RecordID != "" || importCollectionFor(p, item) != nil {
			return Fail("CONFLICT", "Linking this typed source requires an explicit compatible collection identity migration; create a new copy or retain it")
		}
		if err := k.authorizePage(ctx, q, e, target); err != nil {
			return err
		}
		var revision string
		var deleted sql.NullString
		if err := q.QueryRowContext(ctx, "SELECT revision,deleted_at FROM pages WHERE id=?", target).Scan(&revision, &deleted); err != nil {
			return err
		}
		if str(resolution["expectedLocalRevision"]) != revision {
			return Fail("CONFLICT", "Selected local page changed; review its current revision")
		}
		if deleted.Valid {
			return Fail("CONFLICT", "Restore the selected local page before linking this source")
		}
		var typed int
		if err := q.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM records WHERE page_id=?)+(SELECT count(*) FROM collections WHERE owning_page_id=?)", target, target).Scan(&typed); err != nil {
			return err
		}
		if typed > 0 {
			return Fail("CONFLICT", "A plain source cannot replace a record or collection identity")
		}
		rows, err := q.QueryContext(ctx, "SELECT id FROM import_mappings WHERE source_system=? AND source_namespace=? AND local_kind='page' AND local_id=? ORDER BY id LIMIT 2", importItemSourceSystem(p, item), p.Namespace, target)
		if err != nil {
			return err
		}
		mappings := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			mappings = append(mappings, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(mappings) > 1 {
			return Fail("CONFLICT", "Selected page has multiple accepted source baselines; its identity needs a separate mapping decision")
		}
		mapping := NewID()
		if len(mappings) == 1 {
			mapping = mappings[0]
		}
		state, err := importMappingState(ctx, q, mapping)
		if err != nil {
			return err
		}
		review.MappingStateHash = state
		review.Choice, review.LocalID, review.MappingID = action, target, mapping
		old := item.PageID
		remapReviewedImportPage(p, old, target)
		item = &p.Items[index]
		item.MappingID, item.ExpectedRevision = mapping, revision
	}
	if err := reconcileImportFolderIDs(ctx, q, p); err != nil {
		return err
	}
	// A newly selected target can change incoming parent/reference fields in
	// other items. Regenerate their reviews, invalidating prior field choices.
	for i := range p.Items {
		item := &p.Items[i]
		if importIdentitySkipped(*item) || item.IdentityReview != nil && (item.IdentityReview.Choice == "" || item.IdentityReview.Choice == "create_new") {
			continue
		}
		item.Merge = nil
		item.ResolvedFields = nil
		if err := prepareImportMerge(ctx, q, p, item); err != nil {
			return err
		}
	}
	return nil
}
func validateImportIdentities(ctx context.Context, q querier, p *ImportPlan) error {
	targets := map[string]string{}
	reviewedTargets := map[string]bool{}
	skipped := map[string]bool{}
	for _, item := range p.Items {
		if importIdentitySkipped(item) {
			skipped[item.PageID] = true
		}
	}
	for i := range p.Items {
		item := &p.Items[i]
		review := item.IdentityReview
		if review != nil {
			if item.Choice == "duplicate" {
				return Fail("CONFLICT", "Reviewed source identity cannot be remapped by a content choice")
			}
			if review.Version != 1 || review.Choice == "" {
				return Fail("CONFLICT", "Resolve source identity for "+item.Locator)
			}
			var sourceMapping string
			err := q.QueryRowContext(ctx, "SELECT id FROM import_mappings WHERE source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind=? AND identity_value=?", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID).Scan(&sourceMapping)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if sourceMapping != review.SourceMappingID {
				return Fail("CONFLICT", "Source identity acquired a different mapping after preview")
			}
			if !importIdentitySkipped(*item) {
				state, err := importMappingState(ctx, q, item.MappingID)
				if err != nil {
					return err
				}
				if state != review.MappingStateHash {
					return Fail("CONFLICT", "Accepted source baseline changed after identity review")
				}
			}
			raw, err := readImportIdentityAlias(ctx, q, p, item)
			if err != nil {
				return err
			}
			if raw != item.IdentityAlias {
				return Fail("CONFLICT", "Source identity was resolved after this preview; reload it")
			}
			if review.Choice == "create_new" && !p.Initialized {
				var count int
				if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=?", item.PageID).Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					return Fail("CONFLICT", "Proposed new identity was created after preview")
				}
			}
			if !importIdentitySkipped(*item) {
				if review.LocalID != item.PageID || review.MappingID != item.MappingID {
					return Fail("INTEGRITY_ERROR", "Reviewed source target changed")
				}
				var local, system, namespace string
				err = q.QueryRowContext(ctx, "SELECT local_id,source_system,source_namespace FROM import_mappings WHERE id=?", item.MappingID).Scan(&local, &system, &namespace)
				if err == nil && (local != item.PageID || system != importItemSourceSystem(p, item) || namespace != p.Namespace) {
					return Fail("CONFLICT", "Accepted source mapping changed")
				}
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
		}
		if importIdentitySkipped(*item) {
			continue
		}
		if item.PageID != "" && item.RawFileID != "" && item.Kind != "portable_collection" {
			if locator := targets[item.PageID]; locator != "" && locator != item.Locator && (reviewedTargets[item.PageID] || item.IdentityReview != nil) {
				return Fail("CONFLICT", "Two source items target the same local page; review separate identities")
			}
			targets[item.PageID] = item.Locator
			reviewedTargets[item.PageID] = reviewedTargets[item.PageID] || item.IdentityReview != nil
		}
		if skipped[item.ParentID] {
			return Fail("CONFLICT", "A skipped source is still the parent of "+item.Locator+"; review a separate capture or retain the dependent source too")
		}
		for _, block := range item.Blocks {
			if referencesSkippedImportPage(block.Data, skipped) {
				return Fail("CONFLICT", "An incoming reference targets a skipped source; retain or resolve that source before applying")
			}
		}
	}
	return nil
}
func referencesSkippedImportPage(v any, skipped map[string]bool) bool {
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			if key == "targetPageId" && skipped[str(child)] {
				return true
			}
			if key == "link" {
				if page, _, ok := parseCanonicalPageLink(str(child)); ok && skipped[page] {
					return true
				}
			}
			if referencesSkippedImportPage(child, skipped) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if referencesSkippedImportPage(child, skipped) {
				return true
			}
		}
	}
	return false
}
func persistReviewedImportMapping(m *Mutation, p *ImportPlan, item *ImportItem) error {
	if item.IdentityReview == nil || importIdentitySkipped(*item) {
		return nil
	}
	var local string
	err := m.Tx.QueryRowContext(m.Context, "SELECT local_id FROM import_mappings WHERE id=?", item.MappingID).Scan(&local)
	if errors.Is(err, sql.ErrNoRows) {
		// This internal address is separate from the original external ID retained
		// unchanged on the item and in the reviewed alias key/provenance.
		_, err = m.Exec(`INSERT INTO import_mappings VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.MappingID, m.Kernel.id, importItemSourceSystem(p, item), p.Namespace, "page", item.IdentityKind, "reviewed-instance:"+item.MappingID, "page", item.PageID, item.Hash, item.Hash, item.RawFileID, p.ID)
	} else if err == nil && local != item.PageID {
		return Fail("CONFLICT", "Reviewed source mapping changed")
	}
	if err != nil {
		return err
	}
	binding := reviewedImportBinding{Version: 1, GroupHash: item.IdentityReview.GroupHash, MappingID: item.MappingID, PageID: item.PageID, RunID: p.ID}
	raw := string(marshal(binding))
	_, err = m.Exec("INSERT INTO import_reviewed_identities_v1 VALUES(?,?,?,?,?,?) ON CONFLICT(source_system,source_namespace,identity_kind,identity_value,source_locator) DO UPDATE SET binding_json=excluded.binding_json", importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID, item.Locator, raw)
	if err == nil {
		item.IdentityAlias = raw
	}
	return err
}

// A skipped item remains in the report with original bytes, but has no content
// effect. Synthetic directory nodes used solely by skipped leaves are excluded.
func importHasContent(p *ImportPlan) bool {
	skipped := false
	for _, item := range p.Items {
		if importIdentitySkipped(item) {
			skipped = true
			continue
		}
		if item.PageID != "" && item.Kind != "folder" {
			return true
		}
	}
	return !skipped
}

func importMappingState(ctx context.Context, q querier, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	var local, system, namespace, kind, value, revision, hash, file string
	err := q.QueryRowContext(ctx, "SELECT local_id,source_system,source_namespace,identity_kind,identity_value,base_source_revision,base_content_hash,base_raw_file_id FROM import_mappings WHERE id=?", id).Scan(&local, &system, &namespace, &kind, &value, &revision, &hash, &file)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return importDigest(marshal([]string{local, system, namespace, kind, value, revision, hash, file})), nil
}
func refreshImportIdentityGuards(ctx context.Context, q querier, p *ImportPlan) error {
	for i := range p.Items {
		item := &p.Items[i]
		if item.IdentityReview == nil || importIdentitySkipped(*item) {
			continue
		}
		state, err := importMappingState(ctx, q, item.MappingID)
		if err != nil {
			return err
		}
		item.IdentityReview.MappingStateHash = state
	}
	return nil
}

// Suppressing a new presentation container is separate from finishing each
// reviewed item. Kept items must still retain aliases, baselines and evidence.
func importKeepsAllMappedContent(p *ImportPlan) bool {
	kept := false
	for _, item := range p.Items {
		if importIdentitySkipped(item) {
			continue
		}
		switch item.Kind {
		case "folder", "directory", "asset", "rejected":
			continue
		}
		if item.Choice != "keep_local" {
			return false
		}
		if item.PageID != "" && item.MappingID != "" {
			kept = true
			continue
		}
		// A kept database suppresses its supplied row work before this decision.
		if item.Kind != "row" && item.Kind != "portable_record" {
			return false
		}
	}
	return kept
}
func retainRootlessImportEvidence(m *Mutation, p *ImportPlan) error {
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind == "folder" && item.MappingID == "" {
			item.PageID, item.ParentID = "", ""
			item.Reason += "; no new directory needed for kept existing content"
		}
		if item.PageID == "" || item.Choice != "keep_local" || importIdentitySkipped(*item) {
			continue
		}
		for _, file := range []string{p.SourceFileID, item.RawFileID} {
			if err := importAttach(m, item.PageID, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func retainKeptTypedRecordProjections(p *ImportPlan) {
	if !p.TypedDatabaseMerge {
		return
	}
	keptPages, keptCollections := map[string]bool{}, map[string]string{}
	for _, item := range p.Items {
		if item.Choice != "keep_local" || item.MappingID == "" || importIdentitySkipped(item) {
			continue
		}
		if item.Kind == "portable_collection" {
			keptCollections[item.Locator] = item.CollectionID
		} else if item.Kind != "row" && item.Kind != "portable_record" && item.PageID != "" {
			keptPages[item.PageID] = true
		}
	}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind == "portable_record" && item.Choice == "" && item.MappingID == "" && item.CollectionID != "" && keptCollections[item.CSVSource] == item.CollectionID && keptPages[item.PageID] {
			item.Choice = "keep_local"
			item.Reason += "; retained row projection of kept canonical page and database"
		}
	}
}
