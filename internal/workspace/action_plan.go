package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"trove.local/phase0/contracts"
)

const actionPlanInstructions = `Return only a JSON plan: {"version":1,"actions":[{"kind":"create_page","destination":"Exact existing page title","title":"New page title","body":"Markdown text"}]}. Use human-readable exact existing page, database, record and property names; never output IDs, revisions, grants, permissions or raw commands. Supported actions: create_page(destination,title,optional body); update_page(target,optional title,optional body to APPEND); create_task(destination=canonical Tasks page name,title,optional body,status,due YYYY-MM-DD); update_task(target,optional status,due); create_database(destination,title,properties:[{name,type}]); create_record(destination=database page title,title,optional body); update_record(target=record page title,property,value). Database property types: text,number,checkbox,date,url,email,phone. Values use text or decimal strings, boolean, date YYYY-MM-DD, or an existing status/select label. At most 32 actions. Use destination @workspace only for a root page/database specifically requested in workspace scope. If a reference is unclear, use its requested name; the application will ask for a unique title instead of guessing. Do not claim any action has run. All writes require explicit review and approval.`

// ActionPlan contains user-facing references and values, never authoritative
// IDs, permissions or revision guards. Both local AI and paired clients use it.
type ActionPlan struct {
	Version int             `json:"version"`
	Actions []PlannedAction `json:"actions"`
}
type PlannedAction struct {
	Kind        string            `json:"kind"`
	Target      string            `json:"target,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Title       string            `json:"title,omitempty"`
	Body        string            `json:"body,omitempty"`
	Property    string            `json:"property,omitempty"`
	Value       any               `json:"value,omitempty"`
	Status      string            `json:"status,omitempty"`
	Due         string            `json:"due,omitempty"`
	Properties  []PlannedProperty `json:"properties,omitempty"`
}
type PlannedProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type ActionReview struct {
	Label       string `json:"label"`
	Destination string `json:"destination"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after"`
}
type ResolvedActionPlan struct {
	Commands []any                               `json:"commands"`
	Guards   []contracts.ServiceExpectedRevision `json:"expectedRevisions"`
	Changes  []ActionReview                      `json:"changes"`
	PageIDs  []string                            `json:"pageIds"`
}
type actionResolver struct {
	ctx    context.Context
	k      *Kernel
	tx     *sql.Tx
	e      contracts.ServiceExecutionContext
	out    ResolvedActionPlan
	guards map[string]string
}

func (k *Kernel) ResolveActionPlan(ctx context.Context, e contracts.ServiceExecutionContext, draft ActionPlan, sources []contracts.ServiceExpectedRevision) (ResolvedActionPlan, error) {
	if err := k.authorize(e); err != nil {
		return ResolvedActionPlan{}, err
	}
	if err := contracts.Validate("service", "ActionPlan", marshal(draft)); err != nil {
		return ResolvedActionPlan{}, Fail("INVALID_REQUEST", "Invalid proposed actions: "+err.Error())
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ResolvedActionPlan{}, err
	}
	defer tx.Rollback()
	b := &actionResolver{ctx: ctx, k: k, tx: tx, e: e, guards: map[string]string{}, out: ResolvedActionPlan{Commands: []any{}, Guards: []contracts.ServiceExpectedRevision{}, Changes: []ActionReview{}, PageIDs: []string{}}}
	for _, g := range sources {
		if err = b.guard(g.Target.EntityType, string(g.Target.EntityId), g.Revision); err != nil {
			return b.out, err
		}
	}
	for i, a := range draft.Actions {
		if err = ctx.Err(); err != nil {
			return b.out, err
		}
		if err = b.action(a); err != nil {
			return b.out, fmt.Errorf("Action %d: %w", i+1, err)
		}
		if len(b.out.Commands) > 200 {
			return b.out, Fail("RESOURCE_EXHAUSTED", "The proposed plan exceeds 200 changes; split it into smaller reviews")
		}
	}
	req := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": "plan-validation", "expectedRevisions": b.out.Guards}, "commands": b.out.Commands}
	if _, err = contracts.DecodeBatch(marshal(req)); err != nil {
		return b.out, Fail("INVALID_REQUEST", err.Error())
	}
	return b.out, k.authorize(e)
}
func (b *actionResolver) guard(kind, id, revision string) error {
	key := guardKey(kind, id)
	if old, ok := b.guards[key]; ok {
		if old != revision {
			return Fail("CONFLICT", "A source changed while preparing the proposal")
		}
		return nil
	}
	if revision != "0" {
		if err := b.k.authorizeEntity(b.ctx, b.tx, b.e, kind, id); err != nil {
			return err
		}
		var live string
		table, ok := entityTables[kind]
		if !ok {
			return Fail("INVALID_REQUEST", "Unsupported proposal source")
		}
		if err := b.tx.QueryRowContext(b.ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&live); err != nil {
			return err
		}
		if live != revision {
			return Fail("CONFLICT", "A source changed; generate a fresh proposal")
		}
	}
	b.guards[key] = revision
	b.out.Guards = append(b.out.Guards, required(kind, id, revision))
	return nil
}
func (b *actionResolver) current(kind, id string) error {
	table, ok := entityTables[kind]
	if !ok {
		return Fail("INVALID_REQUEST", "Unsupported proposed entity")
	}
	var rev string
	if err := b.tx.QueryRowContext(b.ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&rev); err != nil {
		return err
	}
	return b.guard(kind, id, rev)
}
func (b *actionResolver) newID(kind string) string {
	id := NewID()
	_ = b.guard(kind, id, "0")
	return id
}
func (b *actionResolver) page(name string) (string, string, error) {
	rows, err := b.tx.QueryContext(b.ctx, "SELECT id,title FROM pages WHERE title=? AND deleted_at IS NULL ORDER BY id LIMIT 101", strings.TrimSpace(name))
	if err != nil {
		return "", "", err
	}
	type match struct{ id, title string }
	matches := []match{}
	for rows.Next() {
		var v match
		if err = rows.Scan(&v.id, &v.title); err != nil {
			rows.Close()
			return "", "", err
		}
		matches = append(matches, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", err
	}
	allowed := []match{}
	for _, v := range matches {
		if b.k.authorizePage(b.ctx, b.tx, b.e, v.id) == nil {
			allowed = append(allowed, v)
		}
	}
	if len(allowed) != 1 || len(matches) == 101 {
		return "", "", Fail("INVALID_REQUEST", "Choose a unique available page title for “"+truncateAI(name, 120)+"”; unavailable or duplicate titles are not guessed")
	}
	v := allowed[0]
	if err = b.current("page", v.id); err != nil {
		return "", "", err
	}
	var record, collection string
	err = b.tx.QueryRowContext(b.ctx, "SELECT id,collection_id FROM records WHERE page_id=?", v.id).Scan(&record, &collection)
	if err == nil {
		if err = b.current("record", record); err != nil {
			return "", "", err
		}
		if err = b.current("collection", collection); err != nil {
			return "", "", err
		}
	} else if err != sql.ErrNoRows {
		return "", "", err
	}
	return v.id, v.title, nil
}
func (b *actionResolver) destination(name string) (string, string, error) {
	if name == "@workspace" {
		if _, ok := b.e.Scope.Value.(contracts.DomainScopeVariant1); !ok {
			return "", "", Fail("SCOPE_DENIED", "Root creation requires an explicitly selected workspace scope")
		}
		return "", "Workspace", nil
	}
	return b.page(name)
}
func (b *actionResolver) body(page, body string) {
	for _, block := range aiDraftBlocks(body) {
		block["pageId"] = page
		block["blockId"] = b.newID("block")
		b.out.Commands = append(b.out.Commands, map[string]any{"kind": "block.insert", "block": block})
	}
}
func (b *actionResolver) collection(page string) (string, error) {
	var id string
	if err := b.tx.QueryRowContext(b.ctx, "SELECT id FROM collections WHERE owning_page_id=?", page).Scan(&id); err != nil {
		return "", Fail("INVALID_REQUEST", "The selected destination is not a database")
	}
	return id, b.current("collection", id)
}
func (b *actionResolver) property(collection, name string) (contracts.DomainPropertyDefinition, error) {
	rows, err := b.tx.QueryContext(b.ctx, "SELECT id FROM properties WHERE collection_id=? AND name=? ORDER BY id LIMIT 2", collection, name)
	if err != nil {
		return contracts.DomainPropertyDefinition{}, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return contracts.DomainPropertyDefinition{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return contracts.DomainPropertyDefinition{}, err
	}
	if len(ids) != 1 {
		return contracts.DomainPropertyDefinition{}, Fail("INVALID_REQUEST", "Choose a unique property name in the selected database")
	}
	p, err := propertyDefinition(b.ctx, b.tx, ids[0])
	if err != nil {
		return p, err
	}
	return p, b.current("property", ids[0])
}

// Existing tasks use their persisted facet bindings. New canonical tasks follow
// the same stable property selection as task.create/promoteTask, never names.
func (b *actionResolver) taskProperty(collection, record, kind string) (contracts.DomainPropertyDefinition, error) {
	var id sql.NullString
	var err error
	if record != "" {
		column := "status_property_id"
		if kind == "date" {
			column = "due_property_id"
		}
		err = b.tx.QueryRowContext(b.ctx, "SELECT "+column+" FROM task_facets WHERE record_id=? AND collection_id=?", record, collection).Scan(&id)
	} else {
		err = b.tx.QueryRowContext(b.ctx, "SELECT id FROM properties WHERE collection_id=? AND type=? ORDER BY id LIMIT 1", collection, kind).Scan(&id)
	}
	if err != nil || !id.Valid {
		return contracts.DomainPropertyDefinition{}, Fail("INVALID_REQUEST", "This task has no configured "+kind+" property")
	}
	p, err := propertyDefinition(b.ctx, b.tx, id.String)
	if err != nil {
		return p, err
	}
	if p.Type != kind || string(p.CollectionId) != collection {
		return p, Fail("INVALID_REQUEST", "Task property binding is invalid")
	}
	return p, b.current("property", id.String)
}
func (b *actionResolver) reviewValue(p contracts.DomainPropertyDefinition, raw string) string {
	if raw == "" {
		return "Not set"
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	if value == nil {
		return "Not set"
	}
	if p.Type == "status" || p.Type == "select" {
		if id, ok := value.(string); ok {
			var label string
			if b.tx.QueryRowContext(b.ctx, "SELECT label FROM property_options WHERE property_id=? AND id=?", p.Id, id).Scan(&label) == nil {
				return label
			}
		}
	}
	if p.Type == "text" {
		if runs, ok := value.([]any); ok {
			var text strings.Builder
			for _, run := range runs {
				if r, ok := run.(map[string]any); ok {
					text.WriteString(str(r["text"]))
				}
			}
			return text.String()
		}
	}
	if v, ok := value.(string); ok {
		return v
	}
	return raw
}
func (b *actionResolver) value(p contracts.DomainPropertyDefinition, value any) (any, error) {
	var config map[string]any
	json.Unmarshal(p.Config, &config)
	switch p.Type {
	case "text":
		if v, ok := value.(string); ok {
			value = []any{map[string]any{"text": v}}
		}
	case "number":
		if v, ok := value.(string); ok {
			value = map[string]any{"decimal": v, "format": config["format"]}
		}
	case "date":
		if v, ok := value.(string); ok {
			value = map[string]any{"start": v, "end": nil, "hasTime": false, "timezone": "UTC"}
		}
	case "select", "status":
		label, ok := value.(string)
		if !ok {
			return nil, Fail("INVALID_REQUEST", "Use an existing option label")
		}
		var id string
		rows, err := b.tx.QueryContext(b.ctx, "SELECT id FROM property_options WHERE property_id=? AND label=? ORDER BY id LIMIT 2", p.Id, label)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(ids) != 1 {
			return nil, Fail("INVALID_REQUEST", "Use a unique existing option label")
		}
		value = ids[0]
	case "checkbox", "url", "email", "phone":
	default:
		return nil, Fail("INVALID_REQUEST", "This proposal supports text, number, checkbox, date, status, select, URL, email and phone values; other property types require the regular editor")
	}
	m := &Mutation{Context: b.ctx, Kernel: b.k, Tx: b.tx, Execution: b.e}
	if err := validatePropertyValue(m, p, value); err != nil {
		return nil, err
	}
	return map[string]any{"type": p.Type, "schemaVersion": p.SchemaVersion, "value": value}, nil
}
func (b *actionResolver) setValue(record string, p contracts.DomainPropertyDefinition, value any) (string, error) {
	input, err := b.value(p, value)
	if err != nil {
		return "", err
	}
	var old sql.NullString
	err = b.tx.QueryRowContext(b.ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, p.Id).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	b.out.Commands = append(b.out.Commands, map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": p.Id, "value": input})
	return b.reviewValue(p, old.String), nil
}
func (b *actionResolver) action(a PlannedAction) error {
	if strings.HasPrefix(a.Kind, "create_") && strings.TrimSpace(a.Title) == "" {
		return Fail("INVALID_REQUEST", "A proposed title must contain text")
	}
	if a.Kind == "update_page" && strings.TrimSpace(a.Title) == "" && strings.TrimSpace(a.Body) == "" {
		return Fail("INVALID_REQUEST", "A page update must contain a title or appended text")
	}
	switch a.Kind {
	case "create_page", "create_database":
		parent, label, err := b.destination(a.Destination)
		if err != nil {
			return err
		}
		page := b.newID("page")
		command := map[string]any{"kind": "page.create", "pageId": page, "title": a.Title}
		if parent != "" {
			command["parentPageId"] = parent
		}
		b.out.Commands = append(b.out.Commands, command)
		b.out.PageIDs = append(b.out.PageIDs, page)
		b.body(page, a.Body)
		if a.Kind == "create_database" {
			collection, title, view := b.newID("collection"), b.newID("property"), b.newID("view")
			b.out.Commands = append(b.out.Commands, map[string]any{"kind": "collection.create", "collectionId": collection, "owningPageId": page, "titlePropertyId": title, "title": a.Title})
			visible := []string{title}
			seen := map[string]bool{"name": true}
			for _, p := range a.Properties {
				key := strings.ToLower(strings.TrimSpace(p.Name))
				if seen[key] || key == "" {
					return Fail("INVALID_REQUEST", "Property names must be unique; Name is reserved")
				}
				seen[key] = true
				id := b.newID("property")
				config := map[string]any{}
				if p.Type == "number" {
					config["format"] = "decimal"
				}
				if err = contracts.Validate("domain", "property_"+p.Type+"_config_v1", marshal(config)); err != nil {
					return Fail("INVALID_REQUEST", "Unsupported generated property configuration")
				}
				b.out.Commands = append(b.out.Commands, map[string]any{"kind": "collection.addProperty", "collectionId": collection, "propertyId": id, "name": p.Name, "type": p.Type, "schemaVersion": 1, "config": config})
				visible = append(visible, id)
			}
			b.out.Commands = append(b.out.Commands, map[string]any{"kind": "view.save", "viewId": view, "collectionId": collection, "owningPageId": page, "type": "table", "name": "All records", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": visible, "groupByPropertyId": nil, "subgroupByPropertyId": nil}})
		}
		b.out.Changes = append(b.out.Changes, ActionReview{Label: strings.ReplaceAll(a.Kind, "_", " ") + ": " + a.Title, Destination: label, After: actionCreationReview(a)})
		return nil
	case "update_page":
		page, title, err := b.page(a.Target)
		if err != nil {
			return err
		}
		if a.Title != "" {
			b.out.Commands = append(b.out.Commands, map[string]any{"kind": "page.update", "pageId": page, "title": a.Title})
		}
		b.body(page, a.Body)
		b.out.PageIDs = append(b.out.PageIDs, page)
		b.out.Changes = append(b.out.Changes, ActionReview{Label: "Update page", Destination: title, Before: title, After: actionPageReview(a)})
		return nil
	case "create_task", "create_record":
		destination, label, err := b.page(a.Destination)
		if err != nil {
			return err
		}
		collection, err := b.collection(destination)
		if err != nil {
			return err
		}
		page, record := b.newID("page"), b.newID("record")
		kind := "record.create"
		command := map[string]any{"kind": kind, "recordId": record, "collectionId": collection, "pageId": page, "title": a.Title}
		if a.Kind == "create_task" {
			var role string
			if err = b.tx.QueryRowContext(b.ctx, "SELECT COALESCE(system_role,'') FROM collections WHERE id=?", collection).Scan(&role); err != nil {
				return err
			}
			if role != "tasks" {
				return Fail("INVALID_REQUEST", "Choose the canonical Tasks database for task creation")
			}
			status, err := b.taskProperty(collection, "", "status")
			if err != nil {
				return err
			}
			command["kind"] = "task.create"
			command["statusPropertyId"] = status.Id
		}
		b.out.Commands = append(b.out.Commands, command)
		b.out.PageIDs = append(b.out.PageIDs, page)
		b.body(page, a.Body)
		if a.Status != "" {
			p, err := b.taskProperty(collection, "", "status")
			if err != nil {
				return err
			}
			if _, err = b.setValue(record, p, a.Status); err != nil {
				return err
			}
		}
		if a.Due != "" {
			p, err := b.taskProperty(collection, "", "date")
			if err != nil {
				return err
			}
			if _, err = b.setValue(record, p, a.Due); err != nil {
				return err
			}
		}
		b.out.Changes = append(b.out.Changes, ActionReview{Label: strings.ReplaceAll(a.Kind, "_", " ") + ": " + a.Title, Destination: label, After: actionCreationReview(a)})
		return nil
	case "update_record", "update_task":
		page, title, err := b.page(a.Target)
		if err != nil {
			return err
		}
		var record, collection string
		if err = b.tx.QueryRowContext(b.ctx, "SELECT id,collection_id FROM records WHERE page_id=?", page).Scan(&record, &collection); err != nil {
			return Fail("INVALID_REQUEST", "Choose an existing record or task")
		}
		changes := []struct {
			name  string
			value any
		}{}
		if a.Kind == "update_record" {
			changes = append(changes, struct {
				name  string
				value any
			}{a.Property, a.Value})
		} else {
			var count int
			if err = b.tx.QueryRowContext(b.ctx, "SELECT count(*) FROM task_facets WHERE record_id=?", record).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return Fail("INVALID_REQUEST", "This record is not a canonical task")
			}
			if a.Status != "" {
				changes = append(changes, struct {
					name  string
					value any
				}{"Status", a.Status})
			}
			if a.Due != "" {
				changes = append(changes, struct {
					name  string
					value any
				}{"Due", a.Due})
			}
		}
		for _, change := range changes {
			var p contracts.DomainPropertyDefinition
			if a.Kind == "update_task" {
				kind := "status"
				if change.name == "Due" {
					kind = "date"
				}
				p, err = b.taskProperty(collection, record, kind)
			} else {
				p, err = b.property(collection, change.name)
			}
			if err != nil {
				return err
			}
			before, err := b.setValue(record, p, change.value)
			if err != nil {
				return err
			}
			b.out.Changes = append(b.out.Changes, ActionReview{Label: "Set " + p.Name, Destination: title, Before: before, After: b.reviewValue(p, string(marshal(change.value)))})
		}
		b.out.PageIDs = append(b.out.PageIDs, page)
		return nil
	}
	return Fail("INVALID_REQUEST", "Unsupported proposed action")
}

func actionPageReview(a PlannedAction) string {
	parts := []string{}
	if a.Title != "" {
		parts = append(parts, "Title: "+a.Title)
	}
	if a.Body != "" {
		parts = append(parts, "Append: "+a.Body)
	}
	return strings.Join(parts, "\n")
}
func actionCreationReview(a PlannedAction) string {
	parts := []string{}
	if a.Body != "" {
		parts = append(parts, a.Body)
	}
	if a.Kind == "create_database" {
		parts = append(parts, "Table view with Name (title)")
		for _, p := range a.Properties {
			parts = append(parts, "Property: "+p.Name+" ("+p.Type+")")
		}
	}
	if a.Kind == "create_task" {
		if a.Status != "" {
			parts = append(parts, "Status: "+a.Status)
		} else {
			parts = append(parts, "Status: configured default")
		}
		if a.Due != "" {
			parts = append(parts, "Due: "+a.Due)
		}
	}
	if len(parts) == 0 {
		return "Empty page body"
	}
	return strings.Join(parts, "\n")
}
