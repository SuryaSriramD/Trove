package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"trove.local/phase0/contracts"
)

// A template is immutable captured data. It never contains live automation or
// renderer-authored mutation plans. Old body-only snapshots remain readable.
type unifiedTemplate struct {
	recoverySnapshot
	TemplateVersion int                `json:"templateVersion,omitempty"`
	OwnedTasks      []recoverySnapshot `json:"ownedTasks,omitempty"`
}
type templateDecision struct {
	Source string          `json:"sourcePropertyId"`
	Action string          `json:"action"`
	Target string          `json:"targetPropertyId,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}
type templatePlan struct {
	TemplateID  string                              `json:"templateId"`
	Hash        string                              `json:"hash"`
	PageID      string                              `json:"pageId"`
	RecordID    string                              `json:"recordId,omitempty"`
	Actions     []map[string]any                    `json:"actions"`
	Guards      []contracts.ServiceExpectedRevision `json:"guards"`
	Attachments map[string][]string                 `json:"attachments"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS template_previews(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,scope_grant_id TEXT NOT NULL,policy_revision TEXT NOT NULL,plan_json TEXT NOT NULL,expires_at TEXT NOT NULL,consumed INTEGER NOT NULL DEFAULT 0);`)
			return err
		})
	})
	RegisterQuery("template.preview", previewTemplate)
	RegisterCommand("template.apply", applyUnifiedTemplate)
}

func captureUnifiedTemplate(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, p contracts.ServiceReadPageResult) (unifiedTemplate, error) {
	out := unifiedTemplate{TemplateVersion: 1}
	raw, err := captureRecoverySnapshot(ctx, tx, string(p.Page.Id), p)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.recoverySnapshot); err != nil {
		return out, err
	}
	seen := map[string]bool{}
	if out.Record != nil {
		seen[string(out.Record.Id)] = true
	}
	all := []recoverySnapshot{out.recoverySnapshot}
	blockCount := 0
	for index := 0; index < len(all); index++ {
		snap := all[index]
		if snap.Record != nil && len(snap.Record.Values) > 256 {
			return out, Fail("RESOURCE_EXHAUSTED", "Template exceeds 256 property values per record")
		}
		blockCount += len(snap.Blocks)
		if blockCount > 400 || len(all) > 65 {
			return out, Fail("RESOURCE_EXHAUSTED", "Template exceeds 400 blocks or 64 owned tasks")
		}
		for _, b := range snap.Blocks {
			if b.Type != "todo" {
				continue
			}
			var data map[string]any
			if err = json.Unmarshal(b.Data, &data); err != nil {
				return out, err
			}
			record := str(data["taskRecordId"])
			if seen[record] {
				continue
			}
			seen[record] = true
			if err = k.authorizeEntity(ctx, tx, e, "record", record); err != nil {
				return out, err
			}
			var page string
			if err = tx.QueryRowContext(ctx, "SELECT page_id FROM records WHERE id=?", record).Scan(&page); err != nil {
				return out, err
			}
			body, readErr := readPageTx(ctx, tx, page)
			if readErr != nil {
				return out, readErr
			}
			raw, err = captureRecoverySnapshot(ctx, tx, page, body)
			if err != nil {
				return out, err
			}
			var task recoverySnapshot
			if err = json.Unmarshal(raw, &task); err != nil {
				return out, err
			}
			if task.Task == nil {
				return out, Fail("INTEGRITY_ERROR", "Todo template source has no canonical task")
			}
			all = append(all, task)
			out.OwnedTasks = append(out.OwnedTasks, task)
		}
	}
	if len(marshal(out)) > 2<<20 {
		return out, Fail("RESOURCE_EXHAUSTED", "Template capture exceeds 2 MiB")
	}
	return out, nil
}

func loadUnifiedTemplate(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, id string) (unifiedTemplate, string, error) {
	var out unifiedTemplate
	var page, raw string
	if err := q.QueryRowContext(ctx, "SELECT source_page_id,snapshot_json FROM advanced_templates WHERE id=?", id).Scan(&page, &raw); err != nil {
		return out, "", err
	}
	if err := k.authorizePage(ctx, q, e, page); err != nil {
		return out, "", err
	}
	if len(raw) > 2<<20 {
		return out, "", Fail("RESOURCE_EXHAUSTED", "Template is too large")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, "", err
	}
	if out.TemplateVersion > 1 {
		return out, "", Fail("UNSUPPORTED_VERSION", "Unsupported template snapshot")
	}
	for _, task := range out.OwnedTasks {
		if err := k.authorizePage(ctx, q, e, string(task.Page.Id)); err != nil {
			return out, "", err
		}
	}
	return out, snapshotDigest(raw), nil
}

func templateEditable(kind string) bool { return historicalMapType(kind) }

// buildTemplatePlan is shared by interactive preview and future recurring
// consumers. All identity allocation happens here once; apply only uses the
// stored plan, so a retried batch never allocates a second object graph.
func buildTemplatePlan(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, r map[string]any) (templatePlan, map[string]any, error) {
	return buildTemplatePlanWithIDs(ctx, k, tx, e, r, func(string) string { return NewID() })
}

// Saved owner rules supply occurrence-bound IDs; interactive copies retain random IDs.
func buildTemplatePlanWithIDs(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, r map[string]any, allocate func(string) string) (templatePlan, map[string]any, error) {
	if strings.TrimSpace(str(r["title"])) == "" {
		return templatePlan{}, nil, Fail("INVALID_REQUEST", "Template copy requires a title")
	}
	plan := templatePlan{TemplateID: str(r["templateId"]), Actions: []map[string]any{}, Attachments: map[string][]string{}}
	snap, hash, err := loadUnifiedTemplate(ctx, k, tx, e, plan.TemplateID)
	if err != nil {
		return plan, nil, err
	}
	plan.Hash = hash
	collection := str(r["collectionId"])
	if snap.Record != nil && collection == "" {
		return plan, nil, Fail("INVALID_REQUEST", "A record template requires a destination database")
	}
	if collection != "" {
		if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
			return plan, nil, err
		}
	}
	targets := []contracts.DomainPropertyDefinition{}
	if collection != "" {
		rows, qerr := tx.QueryContext(ctx, "SELECT id FROM properties WHERE collection_id=? ORDER BY id", collection)
		if qerr != nil {
			return plan, nil, qerr
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return plan, nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return plan, nil, err
		}
		if len(ids) > 256 {
			return plan, nil, Fail("RESOURCE_EXHAUSTED", "Template destination exceeds 256 properties")
		}
		for _, id := range ids {
			p, perr := propertyDefinition(ctx, tx, id)
			if perr != nil {
				return plan, nil, perr
			}
			if templateEditable(p.Type) {
				targets = append(targets, p)
			}
		}
	}
	var decisions []templateDecision
	if err = json.Unmarshal(marshal(r["propertyDecisions"]), &decisions); err != nil {
		return plan, nil, err
	}
	decisionMap := map[string]templateDecision{}
	for _, d := range decisions {
		if _, ok := decisionMap[d.Source]; ok {
			return plan, nil, Fail("INVALID_REQUEST", "Duplicate template property decision")
		}
		decisionMap[d.Source] = d
	}
	warnings := []string{}
	issues := []any{}
	all := append([]recoverySnapshot{snap.recoverySnapshot}, snap.OwnedTasks...)
	remap := map[string]string{}
	fresh := map[string]string{}
	blockCount, valueCount := 0, 0
	newID := func(kind, old string) string {
		if id := remap[old]; id != "" {
			return id
		}
		id := allocate(kind + ":" + old)
		remap[old] = id
		fresh[guardKey(kind, id)] = kind
		return id
	}
	for _, source := range all {
		newID("page", string(source.Page.Id))
		if source.Record != nil {
			newID("record", string(source.Record.Id))
		}
		for _, b := range source.Blocks {
			newID("block", string(b.Id))
			blockCount++
		}
	}
	if blockCount > 400 || len(all) > 65 {
		return plan, nil, Fail("RESOURCE_EXHAUSTED", "Template exceeds application bounds")
	}
	plan.PageID = remap[string(snap.Page.Id)]
	if collection != "" {
		if snap.Record != nil {
			plan.RecordID = remap[string(snap.Record.Id)]
		} else {
			plan.RecordID = allocate("record:destination")
			fresh[guardKey("record", plan.RecordID)] = "record"
		}
	}
	// Create every record before its values, allowing self-relations and todo
	// references between owned tasks to resolve to fresh canonical identities.
	for index, source := range all {
		page := remap[string(source.Page.Id)]
		title := source.Page.Title
		dest := collection
		record := plan.RecordID
		if index == 0 {
			title = str(r["title"])
		} else {
			dest = string(source.Record.CollectionId)
			record = remap[string(source.Record.Id)]
		}
		if dest != "" {
			plan.Actions = append(plan.Actions, map[string]any{"kind": "record.create", "recordId": record, "pageId": page, "collectionId": dest, "title": title})
		} else {
			plan.Actions = append(plan.Actions, map[string]any{"kind": "page.create", "pageId": page, "title": title})
		}
	}
	usedDecisions := map[string]bool{}
	for index, source := range all {
		page := remap[string(source.Page.Id)]
		record := plan.RecordID
		dest := collection
		if index > 0 {
			record = remap[string(source.Record.Id)]
			dest = string(source.Record.CollectionId)
		}
		mappedProperties := map[string]string{}
		occupied := map[string]bool{}
		if source.Record != nil {
			defs := map[string]contracts.DomainPropertyDefinition{}
			for _, p := range source.Properties {
				defs[string(p.Id)] = p
			}
			for _, value := range source.Record.Values {
				p, ok := defs[string(value.PropertyId)]
				if !ok {
					return plan, nil, Fail("INTEGRITY_ERROR", "Template value has no captured property definition")
				}
				if !templateEditable(p.Type) {
					continue
				}
				target := string(p.Id)
				var input any
				if err = json.Unmarshal(value.Value, &input); err != nil {
					return plan, nil, err
				}
				d, explicit := decisionMap[string(p.Id)]
				explicit = explicit && index == 0
				if explicit {
					usedDecisions[d.Source] = true
					if d.Action == "omit" {
						if source.Task != nil && source.Task.StatusPropertyId == p.Id {
							return plan, nil, Fail("INVALID_REQUEST", "Task status cannot be omitted")
						}
						warnings = append(warnings, "Omit captured default for "+p.Name)
						continue
					}
					target = d.Target
					if err = json.Unmarshal(d.Value, &input); err != nil {
						return plan, nil, err
					}
				}
				current, pErr := propertyDefinition(ctx, tx, target)
				compatible := pErr == nil && string(current.CollectionId) == dest && templateEditable(current.Type) && current.SchemaVersion == 1
				if !explicit {
					compatible = compatible && current.Type == p.Type && current.SchemaVersion == p.SchemaVersion && string(current.Config) == string(p.Config)
				}
				if !compatible {
					if index > 0 {
						return plan, nil, Fail("CONFLICT", "Owned task property changed; recapture the template")
					}
					issues = append(issues, map[string]any{"sourceProperty": p, "value": input, "reason": "Choose a current destination property and explicit typed value, or omit this default."})
					continue
				}
				if occupied[target] {
					return plan, nil, Fail("INVALID_REQUEST", "Two template defaults map to the same destination property")
				}
				occupied[target] = true
				mappedProperties[string(p.Id)] = target
				if current.Type == "relation" {
					if ids, ok := input.([]any); ok {
						for i, id := range ids {
							if mapped := remap[str(id)]; mapped != "" {
								ids[i] = mapped
							}
						}
					}
				}
				input = remapTemplateValue(input, remap, "", false, false, allocate)
				plan.Actions = append(plan.Actions, map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": target, "value": map[string]any{"type": current.Type, "schemaVersion": current.SchemaVersion, "value": input}})
				valueCount++
			}
			if source.Task != nil {
				status := mappedProperties[string(source.Task.StatusPropertyId)]
				if status == "" && len(issues) == 0 {
					return plan, nil, Fail("CONFLICT", "Template task status default is unavailable")
				}
				if status != "" {
					plan.Actions = append(plan.Actions, map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": status})
				}
			}
		}
		pending := append([]contracts.DomainBlock{}, source.Blocks...)
		inserted := map[string]bool{}
		for len(pending) > 0 {
			next := []contracts.DomainBlock{}
			for _, b := range pending {
				if b.ParentBlockId != nil && !inserted[string(*b.ParentBlockId)] {
					next = append(next, b)
					continue
				}
				var data any
				if err = json.Unmarshal(b.Data, &data); err != nil {
					return plan, nil, err
				}
				kind := b.Type
				if kind == "button" {
					warnings = append(warnings, "Button action retained as inert text; configure a new action explicitly.")
					kind = "code"
					data = map[string]any{"language": "json", "source": string(b.Data)}
				} else {
					if kind == "todo" && remap[str(data.(map[string]any)["taskRecordId"])] == "" {
						return plan, nil, Fail("CONFLICT", "Legacy template does not capture its owned task; recapture the template")
					}
					data = remapTemplateValue(data, remap, "", kind == "unsupported_external", kind == "table" || kind == "columns" || kind == "tab_container", allocate)
				}
				block := map[string]any{"blockId": remap[string(b.Id)], "pageId": page, "type": kind, "schemaVersion": b.SchemaVersion, "data": data}
				if b.ParentBlockId != nil {
					block["parentBlockId"] = remap[string(*b.ParentBlockId)]
				}
				plan.Actions = append(plan.Actions, map[string]any{"kind": "block.insert", "block": block})
				inserted[string(b.Id)] = true
			}
			if len(next) == len(pending) {
				return plan, nil, Fail("INTEGRITY_ERROR", "Template block ownership contains a missing parent or cycle")
			}
			pending = next
		}
		if source.Page.Icon != nil || source.Page.CoverFileId != nil {
			action := map[string]any{"kind": "page.update", "pageId": page}
			if source.Page.Icon != nil {
				action["icon"] = *source.Page.Icon
			}
			if source.Page.CoverFileId != nil {
				action["coverFileId"] = string(*source.Page.CoverFileId)
			}
			plan.Actions = append(plan.Actions, action)
		}
	}
	for source := range decisionMap {
		if !usedDecisions[source] {
			return plan, nil, Fail("INVALID_REQUEST", "Unknown or non-editable template property decision")
		}
	}
	result := map[string]any{"pageId": plan.PageID, "blockCount": blockCount, "valueCount": valueCount, "taskCount": len(snap.OwnedTasks), "warnings": warnings, "propertyIssues": issues, "targetProperties": targets, "canApply": len(issues) == 0}
	if snap.Task != nil {
		result["taskCount"] = len(snap.OwnedTasks) + 1
	}
	if plan.RecordID != "" {
		result["recordId"] = plan.RecordID
	}
	if len(issues) > 0 {
		return plan, result, nil
	}
	if len(plan.Actions) > 1200 {
		return plan, nil, Fail("RESOURCE_EXHAUSTED", "Template requires too many mutations")
	}
	// Todo references have an immediate foreign key to task facets. All records,
	// values and task facets must exist before any page body refers to them.
	priority := map[string]int{"page.create": 0, "record.create": 0, "record.setValue": 1, "task.promote": 2, "block.insert": 3, "page.update": 4}
	sort.SliceStable(plan.Actions, func(i, j int) bool {
		return priority[str(plan.Actions[i]["kind"])] < priority[str(plan.Actions[j]["kind"])]
	})
	if err = templatePlanGuards(ctx, k, tx, e, &plan, all, fresh); err != nil {
		return plan, nil, err
	}
	return plan, result, nil
}

// Only canonical references are remapped. Arbitrary imported evidence, code,
// plain text, URLs and immutable file identities are not interpreted as IDs.
func remapTemplateValue(value any, ids map[string]string, key string, inert, localIDs bool, allocate func(string) string) any {
	if inert {
		return value
	}
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "raw" {
				continue
			}
			if k == "id" && localIDs {
				if id := str(child); id != "" {
					if ids[id] == "" {
						ids[id] = allocate("local:" + id)
					}
					v[k] = ids[id]
				}
				continue
			}
			v[k] = remapTemplateValue(child, ids, k, false, localIDs, allocate)
		}
	case []any:
		for i, child := range v {
			v[i] = remapTemplateValue(child, ids, key, false, localIDs, allocate)
		}
	case string:
		switch key {
		case "targetPageId", "contentSourceId", "childBlockIds", "taskRecordId":
			if mapped := ids[v]; mapped != "" {
				return mapped
			}
		case "link":
			if strings.HasPrefix(v, "trove://page/") {
				page, block, ok := parseCanonicalPageLink(v)
				if ok {
					if mapped := ids[page]; mapped != "" {
						page = mapped
					}
					if mapped := ids[block]; mapped != "" {
						block = mapped
					}
					return canonicalPageLink(page, block)
				}
			}
		}
	}
	return value
}

func templatePlanGuards(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, plan *templatePlan, sources []recoverySnapshot, fresh map[string]string) error {
	refs := map[string]contracts.ServiceExpectedRevision{}
	add := func(kind, id string) error {
		if id == "" {
			return nil
		}
		key := guardKey(kind, id)
		if _, ok := refs[key]; ok {
			return nil
		}
		revision := "0"
		if _, ok := fresh[key]; !ok {
			table := entityTables[kind]
			if table == "" {
				return Fail("INVALID_REQUEST", "Invalid template reference")
			}
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&revision); err != nil {
				return err
			}
			if err := k.authorizeEntity(ctx, tx, e, kind, id); err != nil {
				return err
			}
		}
		refs[key] = required(kind, id, revision)
		return nil
	}
	// Existing automation guard collection includes reciprocal relation endpoints.
	guards, err := automationGuards(ctx, tx, "", plan.Actions)
	if err != nil {
		return err
	}
	for _, g := range guards {
		if err = add(g.Target.EntityType, string(g.Target.EntityId)); err != nil {
			return err
		}
	}
	for _, s := range sources {
		if err = add("page", string(s.Page.Id)); err != nil {
			return err
		}
		if s.Record != nil {
			if err = add("record", string(s.Record.Id)); err != nil {
				return err
			}
		}
	}
	var walk func(any) error
	walk = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				kind := ""
				id := str(child)
				switch key {
				case "targetPageId":
					kind = "page"
				case "contentSourceId":
					kind = "block"
				case "taskRecordId":
					kind = "record"
				case "fileId", "rawFileId", "coverFileId":
					kind = "file"
				case "link":
					if strings.HasPrefix(id, "trove://page/") {
						page, block, ok := parseCanonicalPageLink(id)
						if !ok {
							return Fail("INVALID_REQUEST", "Malformed template link")
						}
						if err := add("page", page); err != nil {
							return err
						}
						if block != "" {
							if err := add("block", block); err != nil {
								return err
							}
						}
					}
				}
				if kind != "" {
					if err := add(kind, id); err != nil {
						return err
					}
				}
				if key != "raw" {
					if err := walk(child); err != nil {
						return err
					}
				}
			}
		case []any:
			for _, child := range v {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	recordPages := map[string]string{}
	for _, a := range plan.Actions {
		if a["kind"] == "record.create" {
			recordPages[str(a["recordId"])] = str(a["pageId"])
		}
	}
	for _, a := range plan.Actions {
		if err = walk(a); err != nil {
			return err
		}
		page := str(a["pageId"])
		if a["kind"] == "block.insert" {
			b := a["block"].(map[string]any)
			page = str(b["pageId"])
			if err = add("block", str(b["blockId"])); err != nil {
				return err
			}
			if err = add("block", str(b["parentBlockId"])); err != nil {
				return err
			}
		}
		if a["kind"] == "task.promote" {
			if err = add("property", str(a["statusPropertyId"])); err != nil {
				return err
			}
		}
		if a["kind"] == "record.setValue" {
			page = recordPages[str(a["recordId"])]
		}
		files := map[string]bool{}
		collectFileIDs(a, files)
		for file := range files {
			if err = add("file", file); err != nil {
				return err
			}
			if page != "" {
				plan.Attachments[page] = append(plan.Attachments[page], file)
			}
		}
	}
	for _, g := range refs {
		plan.Guards = append(plan.Guards, g)
	}
	sort.Slice(plan.Guards, func(i, j int) bool {
		return guardKey(plan.Guards[i].Target.EntityType, string(plan.Guards[i].Target.EntityId)) < guardKey(plan.Guards[j].Target.EntityType, string(plan.Guards[j].Target.EntityId))
	})
	if len(plan.Guards) > 1000 {
		return Fail("RESOURCE_EXHAUSTED", "Template exceeds 1000 revision guards")
	}
	for _, g := range plan.Guards {
		if g.Target.EntityType == "file" {
			meta, err := readFileMetadata(ctx, tx, string(g.Target.EntityId))
			if err != nil {
				return err
			}
			if err = verifyAsset(k.path+"/"+meta.StorageKey, string(meta.ContentHash), parseSize(meta.SizeBytes)); err != nil {
				return err
			}
		}
	}
	return nil
}

func executeTemplatePlan(m *Mutation, plan templatePlan) error {
	for _, guard := range plan.Guards {
		kind, id := guard.Target.EntityType, string(guard.Target.EntityId)
		if m.GuardRevision(kind, id) != guard.Revision {
			return Fail("CONFLICT", "Template preview guards changed; preview again")
		}
		if err := m.Require(kind, id); err != nil {
			return err
		}
	}
	for _, a := range plan.Actions {
		if err := contracts.Validate("service", "Command", marshal(a)); err != nil {
			return Fail("INVALID_REQUEST", err.Error())
		}
		handler := commandHandlers[str(a["kind"])]
		if handler == nil {
			return Fail("UNSUPPORTED_SCHEMA", "Template action is unavailable")
		}
		if err := handler(m, a); err != nil {
			return err
		}
	}
	for page, files := range plan.Attachments {
		for _, file := range files {
			if _, err := m.Exec("INSERT OR IGNORE INTO attachments(page_id,file_id) VALUES(?,?)", page, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func previewTemplate(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := contracts.Validate("service", "TemplatePreviewRequest", marshal(r)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	var result map[string]any
	err := k.write(ctx, func(tx *sql.Tx) error {
		plan, summary, err := buildTemplatePlan(ctx, k, tx, e, r)
		if err != nil {
			return err
		}
		result = summary
		id, expires := NewID(), timeAfterMinutes(10)
		result["previewId"] = id
		result["expiresAt"] = expires
		result["expectedRevisions"] = plan.Guards
		if plan.Guards == nil {
			result["expectedRevisions"] = []any{}
		}
		result["command"] = map[string]any{"kind": "template.apply", "previewId": id}
		if result["canApply"] != true {
			return nil
		}
		// Validate the real canonical mutation path inside a rolled-back savepoint.
		// Preview cannot leave rows, history or task facets behind.
		if _, err = tx.ExecContext(ctx, "SAVEPOINT template_validation"); err != nil {
			return err
		}
		m := &Mutation{Tx: tx, Kernel: k, Context: ctx, Execution: e, guards: map[string]string{}, touched: map[string]contracts.ServiceRevisionChange{}, before: map[string]json.RawMessage{}}
		for _, g := range plan.Guards {
			m.guards[guardKey(g.Target.EntityType, string(g.Target.EntityId))] = g.Revision
		}
		for _, a := range plan.Actions {
			var command contracts.ServiceCommand
			if err = json.Unmarshal(marshal(a), &command); err != nil {
				break
			}
			m.Request.Commands = append(m.Request.Commands, command)
		}
		if err == nil {
			err = executeTemplatePlan(m, plan)
		}
		if err == nil {
			for _, validate := range commitValidators {
				if err = validate(m); err != nil {
					break
				}
			}
		}
		if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO template_validation"); rollbackErr != nil {
			return rollbackErr
		}
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE template_validation"); releaseErr != nil {
			return releaseErr
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM template_previews WHERE expires_at <= ?", now()); err != nil {
			return err
		}
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM template_previews WHERE actor_id=? AND consumed=0", e.Actor.ActorId).Scan(&active); err != nil {
			return err
		}
		if active >= 32 {
			return Fail("RESOURCE_EXHAUSTED", "Too many active template previews; apply a reviewed preview or wait for expiry")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO template_previews(id,actor_id,scope_grant_id,policy_revision,plan_json,expires_at) VALUES(?,?,?,?,?,?)", id, e.Actor.ActorId, e.ScopeGrantId, e.PolicyRevision, string(marshal(plan)), expires)
		return err
	})
	return result, err
}

func applyUnifiedTemplate(m *Mutation, c map[string]any) error {
	var actor, grant, policy, raw, expires string
	var consumed bool
	if err := m.Tx.QueryRowContext(m.Context, "SELECT actor_id,scope_grant_id,policy_revision,plan_json,expires_at,consumed FROM template_previews WHERE id=?", c["previewId"]).Scan(&actor, &grant, &policy, &raw, &expires, &consumed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Fail("NOT_FOUND", "Template preview is unavailable")
		}
		return err
	}
	if actor != string(m.Execution.Actor.ActorId) || grant != string(m.Execution.ScopeGrantId) || policy != string(m.Execution.PolicyRevision) {
		return Fail("SCOPE_DENIED", "Template preview belongs to another scope")
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !time.Now().Before(expiry) || consumed {
		return Fail("CONFLICT", "Template preview expired or was already applied; preview again")
	}
	var plan templatePlan
	if err = json.Unmarshal([]byte(raw), &plan); err != nil {
		return err
	}
	_, hash, err := loadUnifiedTemplate(m.Context, m.Kernel, m.Tx, m.Execution, plan.TemplateID)
	if err != nil {
		return err
	}
	if hash != plan.Hash {
		return Fail("CONFLICT", "Template changed after preview")
	}
	if err = executeTemplatePlan(m, plan); err != nil {
		return err
	}
	_, err = m.Exec("UPDATE template_previews SET consumed=1 WHERE id=?", c["previewId"])
	return err
}
