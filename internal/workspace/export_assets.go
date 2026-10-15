package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"

	"trove.local/phase0/contracts"
)

type portableAsset struct {
	metadata contracts.DomainFile
	// Non-nil only for an isolated source item. Export never changes the managed
	// original, its identity, or the evidence retained by a native backup.
	content []byte
}

const portableEvidencePolicy = "Current-content export retains live attachments and isolated live-item provenance. Import containers, full import inventories, multi-item CSV/API responses, and unreferenced source evidence are omitted; native backups retain the originals. Conversation evidence is isolated to its exported conversation, preserving all supplied fields."

// portableAssets resolves the live graph, excluding the automatic evidence
// attachments that an import places on its root and item pages. Those references
// are archival inventory, not evidence that the user still exports every item.
func (k *Kernel) portableAssets(ctx context.Context, tx *sql.Tx, pages map[string]contracts.ServiceReadPageResult, metadata map[string]any) ([]portableAsset, []any, error) {
	automatic := map[string]map[string]bool{}
	broad := map[string]bool{}
	owners := map[string]map[string]bool{}
	items := map[string]ImportItem{}
	mark := func(page, file string) {
		if page == "" || file == "" {
			return
		}
		if automatic[page] == nil {
			automatic[page] = map[string]bool{}
		}
		automatic[page][file] = true
	}
	rows, err := tx.QueryContext(ctx, "SELECT plan_json FROM import_runs ORDER BY created_at")
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var raw string
		var plan ImportPlan
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if err = json.Unmarshal([]byte(raw), &plan); err != nil {
			rows.Close()
			return nil, nil, err
		}
		mark(plan.RootID, plan.SourceFileID)
		broad[plan.SourceFileID] = true
		for _, item := range plan.Items {
			mark(plan.RootID, item.RawFileID)
			mark(item.PageID, item.RawFileID)
			items[plan.ID+"\x00"+item.Locator] = item
			if item.RawFileID != "" && item.PageID != "" {
				if owners[item.RawFileID] == nil {
					owners[item.RawFileID] = map[string]bool{}
				}
				owners[item.RawFileID][item.PageID] = true
			}
			if item.Kind == "csv" || item.Kind == "portable_collection" || item.Kind == "notion_block" || path.Base(item.Locator) == "trove-manifest.json" {
				broad[item.RawFileID] = true
			}
		}
		// A directly imported single document has no container or sibling items.
		if len(plan.Items) == 1 && !strings.EqualFold(path.Ext(plan.SourceName), ".zip") {
			i := plan.Items[0]
			if i.Kind == "markdown" || i.Kind == "html" || i.Kind == "document" {
				delete(broad, i.RawFileID)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	for file, pages := range owners {
		if len(pages) > 1 {
			broad[file] = true
		}
	}

	selected := map[string]bool{}
	rows, err = tx.QueryContext(ctx, "SELECT r.page_id,r.source_kind,r.file_id FROM file_references r JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL ORDER BY r.page_id,r.file_id")
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var page, kind, file string
		if err = rows.Scan(&page, &kind, &file); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if _, exported := pages[page]; !exported {
			continue
		}
		if kind == "import_evidence" {
			continue
		}
		if kind != "explicit" || !automatic[page][file] {
			selected[file] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	// Saved templates and commands are current, inert user content too.
	collectFileIDs(metadata, selected)
	evidence := []any{}
	synthetic := []portableAsset{}
	pageIDs := make([]string, 0, len(pages))
	for id := range pages {
		pageIDs = append(pageIDs, id)
	}
	sort.Strings(pageIDs)
	for _, id := range pageIDs {
		page := pages[id]
		if page.Page.Provenance == nil {
			continue
		}
		var provenance map[string]any
		if err = json.Unmarshal(marshal(page.Page.Provenance), &provenance); err != nil {
			return nil, nil, err
		}
		raw, _ := provenance["raw"].(map[string]any)
		file := str(raw["rawFileId"])
		item := items[str(raw["importRunId"])+"\x00"+str(raw["sourceLocator"])]
		if file != "" && item.Kind == "conversation" {
			asset, isolateErr := k.portableConversationAsset(ctx, tx, file, item)
			if isolateErr != nil {
				return nil, nil, isolateErr
			}
			if asset.content == nil {
				selected[file] = true // The original contains this conversation alone.
			} else {
				synthetic = append(synthetic, asset)
				replaceExportFileRef(provenance, file, string(asset.metadata.Id))
				selected[string(asset.metadata.Id)] = true
			}
		} else if file != "" && !broad[file] {
			selected[file] = true
		}
		// Preserve non-file provenance and remove references to omitted evidence.
		// This also handles nested provenance from previous portable round trips.
		pruneExportEvidenceRefs(provenance, selected)
		var cleaned contracts.DomainProvenance
		if err = json.Unmarshal(marshal(provenance), &cleaned); err != nil {
			return nil, nil, err
		}
		page.Page.Provenance = &cleaned
		pages[id] = page
		evidence = append(evidence, map[string]any{"pageId": id, "provenance": provenance})
	}
	assets := []portableAsset{}
	syntheticIDs := map[string]bool{}
	for _, a := range synthetic {
		syntheticIDs[string(a.metadata.Id)] = true
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		if !syntheticIDs[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		f, err := readFileMetadata(ctx, tx, id)
		if err != nil {
			return nil, nil, err
		}
		assets = append(assets, portableAsset{metadata: f})
	}
	return append(assets, synthetic...), evidence, nil
}

// Older importers materialized the manifest itself as an unsupported page.
// Do not re-export that automatic full-workspace inventory as current content.
func (k *Kernel) portableInventoryPage(ctx context.Context, tx *sql.Tx, page contracts.ServiceReadPageResult) (bool, error) {
	if page.Page.Provenance == nil || page.Page.Provenance.Raw == nil {
		return false, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(*page.Page.Provenance.Raw, &raw); err != nil {
		return false, err
	}
	if str(raw["sourceLocator"]) != "trove-manifest.json" || str(raw["rawFileId"]) == "" {
		return false, nil
	}
	f, err := readFileMetadata(ctx, tx, str(raw["rawFileId"]))
	if err != nil {
		return false, err
	}
	input, err := openAsset(k.path + "/" + f.StorageKey)
	if err != nil {
		return false, err
	}
	defer input.Close()
	var inventory struct {
		Format string `json:"format"`
	}
	if err = json.NewDecoder(input).Decode(&inventory); err != nil {
		return false, nil
	}
	return inventory.Format == "trove-portable", nil
}

func replaceExportFileRef(value any, old, replacement string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if (key == "rawFileId" || key == "responseFileId") && str(child) == old {
				v[key] = replacement
			} else {
				replaceExportFileRef(child, old, replacement)
			}
		}
	case []any:
		for _, child := range v {
			replaceExportFileRef(child, old, replacement)
		}
	}
}

func pruneExportEvidenceRefs(value any, selected map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "rawFileId" || key == "responseFileId" || key == "sourceFileId" || key == "retainedFileId" {
				if !selected[str(child)] {
					delete(v, key)
					v["evidenceOmission"] = "Original source evidence is omitted from this current-content export; use a native backup."
				}
			} else {
				pruneExportEvidenceRefs(child, selected)
			}
		}
	case []any:
		for _, child := range v {
			pruneExportEvidenceRefs(child, selected)
		}
	}
}

func (k *Kernel) portableConversationAsset(ctx context.Context, tx *sql.Tx, file string, item ImportItem) (portableAsset, error) {
	f, err := readFileMetadata(ctx, tx, file)
	if err != nil {
		return portableAsset{}, err
	}
	full := k.path + "/" + f.StorageKey
	size, err := strconv.ParseInt(f.SizeBytes, 10, 64)
	if err != nil {
		return portableAsset{}, err
	}
	if err = verifyAsset(full, string(f.ContentHash), size); err != nil {
		return portableAsset{}, err
	}
	input, err := openAsset(full)
	if err != nil {
		return portableAsset{}, err
	}
	defer input.Close()
	var value json.RawMessage
	if err = json.NewDecoder(input).Decode(&value); err != nil {
		return portableAsset{}, err
	}
	var entries []json.RawMessage
	if len(value) > 0 && value[0] == '[' {
		err = json.Unmarshal(value, &entries)
	} else {
		entries = []json.RawMessage{value}
	}
	if err != nil {
		return portableAsset{}, err
	}
	for index, entry := range entries {
		var object map[string]any
		if err = json.Unmarshal(entry, &object); err != nil {
			return portableAsset{}, err
		}
		provider, id := "chatgpt", str(object["id"])
		if id == "" {
			id = str(object["conversation_id"])
		}
		if _, ok := object["chat_messages"]; ok {
			provider, id = "claude", str(object["uuid"])
		}
		if id == "" {
			id = strings.Split(item.Locator, "#conversation=")[0] + "#conversation=" + strconv.Itoa(index+1)
		}
		if provider+":"+id != item.ExternalID {
			continue
		}
		if len(entries) == 1 {
			return portableAsset{metadata: f}, nil
		}
		f.Id = contracts.DomainId(NewID())
		f.ContentHash = contracts.DomainHash(importDigest(entry))
		f.SizeBytes = strconv.Itoa(len(entry))
		f.OriginalName = "conversation.json"
		f.MediaType = "application/json"
		return portableAsset{metadata: f, content: entry}, nil
	}
	return portableAsset{}, Fail("INTEGRITY_ERROR", "Conversation source identity does not match its retained evidence")
}
