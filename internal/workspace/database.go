package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

func init() {
	for _, kind := range []string{"collection.create", "collection.addProperty", "record.create", "record.setValue", "record.clearValue", "view.save", "container.create", "container.linkCollection"} {
		RegisterCommand(kind, databaseCommand)
	}
	RegisterQuery("queryRecords", queryRecords)
	for _, name := range []string{"collections", "properties", "records", "views", "containers", "containerCollections"} {
		n := name
		RegisterSnapshotReader(n, func(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
			return databaseSnapshot(ctx, k, tx, e, n)
		})
	}
	RegisterCommitValidator(validateDatabaseCommit)
}

func databaseCommand(m *Mutation, c map[string]any) error {
	switch str(c["kind"]) {
	case "collection.create":
		id, page, title := str(c["collectionId"]), str(c["owningPageId"]), str(c["titlePropertyId"])
		if err := m.TouchPage(page); err != nil {
			return err
		}
		if err := requireNew(m, "collection", id); err != nil {
			return err
		}
		if err := requireNew(m, "property", title); err != nil {
			return err
		}
		if _, err := m.Exec("INSERT INTO collections(id,workspace_id,owning_page_id,title,title_property_id,revision) VALUES(?,?,?,?,?,1)", id, m.Kernel.id, page, c["title"], title); err != nil {
			return err
		}
		_, err := m.Exec("INSERT INTO properties(id,collection_id,name,type,schema_version,config_json,revision) VALUES(?,?,?,'title',1,'{}',1)", title, id, "Name")
		return err
	case "collection.addProperty":
		return addProperty(m, c)
	case "record.create":
		return createRecord(m, c)
	case "record.setValue":
		return setRecordValue(m, str(c["recordId"]), str(c["propertyId"]), c["value"].(map[string]any))
	case "record.clearValue":
		return clearRecordValue(m, str(c["recordId"]), str(c["propertyId"]))
	case "view.save":
		return saveView(m, c)
	case "container.create":
		id, page := str(c["containerId"]), str(c["owningPageId"])
		if err := requireNew(m, "container", id); err != nil {
			return err
		}
		if err := m.TouchPage(page); err != nil {
			return err
		}
		parent := str(c["parentProjectId"])
		if parent != "" {
			if err := m.Require("container", parent); err != nil {
				return err
			}
			var kind string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT kind FROM containers WHERE id=?", parent).Scan(&kind); err != nil {
				return err
			}
			if kind != "project" {
				return Fail("INVALID_REQUEST", "Parent must be a project")
			}
		}
		_, err := m.Exec("INSERT INTO containers VALUES(?,?,?,?,?,?,1)", id, m.Kernel.id, page, c["containerKind"], c["label"], nullable(c["parentProjectId"]))
		return err
	case "container.linkCollection":
		container, collection := str(c["containerId"]), str(c["collectionId"])
		if err := m.Touch("container", container); err != nil {
			return err
		}
		if err := m.Require("collection", collection); err != nil {
			return err
		}
		_, err := m.Exec("INSERT OR IGNORE INTO container_collections VALUES(?,?)", container, collection)
		return err
	}
	return Fail("UNSUPPORTED_SCHEMA", "Unsupported database command")
}
func requireNew(m *Mutation, kind, id string) error {
	if err := m.Touch(kind, id); err != nil {
		return err
	}
	if m.GuardRevision(kind, id) != "0" {
		return Fail("CONFLICT", kind+" already exists")
	}
	return nil
}
func addProperty(m *Mutation, c map[string]any) error {
	id, collection, kind := str(c["propertyId"]), str(c["collectionId"]), str(c["type"])
	if kind == "title" {
		return Fail("INVALID_REQUEST", "A collection has one immutable title projection")
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := requireNew(m, "property", id); err != nil {
		return err
	}
	if number(c["schemaVersion"]) != 1 {
		return Fail("UNSUPPORTED_SCHEMA", "Unsupported property version")
	}
	if err := contracts.Validate("domain", "property_"+kind+"_config_v1", marshal(c["config"])); err != nil {
		return Fail("INVALID_REQUEST", "Invalid property configuration: "+err.Error())
	}
	if _, err := m.Exec("INSERT INTO properties VALUES(?,?,?,?,?,?,1)", id, collection, c["name"], kind, 1, string(marshal(c["config"]))); err != nil {
		return err
	}
	if kind == "select" || kind == "multi_select" || kind == "status" {
		config := c["config"].(map[string]any)
		seen := map[string]string{}
		for _, raw := range config["options"].([]any) {
			o := raw.(map[string]any)
			oid := str(o["id"])
			if _, exists := seen[oid]; exists {
				return Fail("INVALID_REQUEST", "Duplicate option ID")
			}
			seen[oid] = str(o["group"])
			if _, err := m.Exec("INSERT INTO property_options VALUES(?,?,?,?)", oid, id, o["label"], nullable(o["group"])); err != nil {
				return err
			}
		}
		for _, name := range []string{"defaultOptionId", "doneOptionId", "reopenOptionId"} {
			if v := str(config[name]); v != "" {
				group, ok := seen[v]
				if !ok {
					return Fail("INVALID_REQUEST", "Status mapping references an unknown option")
				}
				if name == "doneOptionId" && group != "done" || name == "reopenOptionId" && group == "done" {
					return Fail("INVALID_REQUEST", "Status mapping has an incompatible group")
				}
			}
		}
	}
	return nil
}
func createRecord(m *Mutation, c map[string]any) error {
	id, page, collection := str(c["recordId"]), str(c["pageId"]), str(c["collectionId"])
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := requireNew(m, "record", id); err != nil {
		return err
	}
	var parent string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT owning_page_id FROM collections WHERE id=?", collection).Scan(&parent); err != nil {
		return err
	}
	// Record pages are canonical children of their owning collection page. The
	// collection guard authorizes creation; no independent duplicate title value.
	if err := requireNew(m, "page", page); err != nil {
		return err
	}
	m.before[page] = json.RawMessage(`null`)
	if _, err := m.Exec("INSERT INTO pages(id,workspace_id,parent_page_id,title,revision,created_at,updated_at) VALUES(?,?,?,?,1,?,?)", page, m.Kernel.id, parent, c["title"], now(), now()); err != nil {
		return err
	}
	var max int64
	if err := m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(MAX(CAST(sort_key AS INTEGER)),0) FROM records WHERE collection_id=?", collection).Scan(&max); err != nil {
		return err
	}
	if max >= 999999999999 {
		return Fail("RESOURCE_EXHAUSTED", "Record order exhausted")
	}
	_, err := m.Exec("INSERT INTO records VALUES(?,?,?,?,1)", id, collection, page, fmt.Sprintf("%012d", max+1))
	return err
}
func propertyDefinition(ctx context.Context, q querier, id string) (contracts.DomainPropertyDefinition, error) {
	var p contracts.DomainPropertyDefinition
	var data string
	err := q.QueryRowContext(ctx, "SELECT id,collection_id,name,type,schema_version,config_json,revision FROM properties WHERE id=?", id).Scan(&p.Id, &p.CollectionId, &p.Name, &p.Type, &p.SchemaVersion, &data, &p.Revision)
	p.Config = json.RawMessage(data)
	return p, err
}
func setRecordValue(m *Mutation, record, property string, input map[string]any) error {
	var collection, page, deleted string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT r.collection_id,r.page_id,COALESCE(p.deleted_at,'') FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&collection, &page, &deleted); err != nil {
		return err
	}
	if deleted != "" {
		return Fail("INVALID_REQUEST", "Cannot edit a trashed record")
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := m.Require("property", property); err != nil {
		return err
	}
	p, err := propertyDefinition(m.Context, m.Tx, property)
	if err != nil {
		return err
	}
	if string(p.CollectionId) != collection || p.Type != str(input["type"]) || p.SchemaVersion != number(input["schemaVersion"]) {
		return Fail("INVALID_REQUEST", "Value type, version, or collection does not match the property")
	}
	if (p.Type == "formula" || p.Type == "rollup") && m.Execution.Origin != "import" {
		return Fail("INVALID_REQUEST", "Derived properties are calculated from their definitions")
	}
	if p.Type == "title" {
		return Fail("INVALID_REQUEST", "Edit the canonical page title with page.update")
	}
	value := input["value"]
	if err = validatePropertyValue(m, p, value); err != nil {
		return err
	}
	if err = syncReciprocalRelation(m, record, p, value); err != nil {
		return err
	}
	_, err = m.Exec("INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,?,?,?) ON CONFLICT(record_id,property_id) DO UPDATE SET schema_version=excluded.schema_version,value_json=excluded.value_json", record, collection, property, p.Type, p.SchemaVersion, string(marshal(value)))
	return err
}
func validatePropertyValue(m *Mutation, p contracts.DomainPropertyDefinition, v any) error {
	if err := contracts.Validate("domain", "property_"+p.Type+"_v1", marshal(v)); err != nil {
		return Fail("INVALID_REQUEST", "Invalid "+p.Type+" value: "+err.Error())
	}
	switch p.Type {
	case "select", "status", "multi_select":
		options := []any{v}
		if p.Type == "multi_select" {
			options = v.([]any)
		}
		for _, option := range options {
			if option == nil {
				continue
			}
			var n int
			if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM property_options WHERE id=? AND property_id=?", option, p.Id).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return Fail("INVALID_REQUEST", "Selected option belongs to another property")
			}
		}
	case "date":
		return validateDate(v.(map[string]any))
	case "url":
		if s := str(v); s != "" {
			u, err := url.Parse(s)
			if err != nil || u.Scheme == "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" && u.Scheme != "tel") {
				return Fail("INVALID_REQUEST", "URL must use http, https, mailto, or tel")
			}
		}
	case "email":
		if s := str(v); s != "" {
			a, err := mail.ParseAddress(s)
			if err != nil || a.Address != s {
				return Fail("INVALID_REQUEST", "Invalid email address")
			}
		}
	case "files":
		for _, item := range v.([]any) {
			file := str(item.(map[string]any)["fileId"])
			if err := m.Kernel.authorizeEntity(m.Context, m.Tx, m.Execution, "file", file); err != nil {
				return err
			}
			var n int
			if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM files WHERE id=?", file).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return Fail("NOT_FOUND", "File does not exist")
			}
		}
	case "relation":
		var config map[string]any
		json.Unmarshal(p.Config, &config)
		if config["cardinality"] == "one" && len(v.([]any)) > 1 {
			return Fail("INVALID_REQUEST", "This relation allows one target")
		}
		for _, item := range v.([]any) {
			id := str(item)
			if err := m.Require("record", id); err != nil {
				return err
			}
			var collection string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id FROM records WHERE id=?", id).Scan(&collection); err != nil {
				return err
			}
			if collection != str(config["targetCollectionId"]) {
				return Fail("INVALID_REQUEST", "Relation target collection mismatch")
			}
		}
	}
	return nil
}
func validateDate(v map[string]any) error {
	layout := "2006-01-02"
	if v["hasTime"] == true {
		layout = time.RFC3339Nano
	}
	start, err := time.Parse(layout, str(v["start"]))
	if err != nil {
		return Fail("INVALID_REQUEST", "Invalid date start")
	}
	if end := str(v["end"]); end != "" {
		t, err := time.Parse(layout, end)
		if err != nil || t.Before(start) {
			return Fail("INVALID_REQUEST", "Date range end precedes start or is invalid")
		}
	}
	if _, err := time.LoadLocation(str(v["timezone"])); err != nil {
		return Fail("INVALID_REQUEST", "Unknown date timezone")
	}
	return nil
}
func saveView(m *Mutation, c map[string]any) error {
	id, collection, page, kind := str(c["viewId"]), str(c["collectionId"]), str(c["owningPageId"]), str(c["type"])
	if err := m.Touch("view", id); err != nil {
		return err
	}
	if err := m.Require("collection", collection); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if m.GuardRevision("view", id) != "0" {
		var oldCollection, oldPage string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id,owning_page_id FROM views WHERE id=?", id).Scan(&oldCollection, &oldPage); err != nil {
			return err
		}
		if oldCollection != collection {
			return Fail("INVALID_REQUEST", "A saved view cannot change its canonical collection")
		}
		if oldPage != page {
			if err := m.TouchPage(oldPage); err != nil {
				return err
			}
		}
	}
	if number(c["schemaVersion"]) != 1 {
		return Fail("UNSUPPORTED_SCHEMA", "Unsupported view version")
	}
	if err := contracts.Validate("domain", "view_"+kind+"_v1", marshal(c["config"])); err != nil {
		return Fail("INVALID_REQUEST", "Invalid view configuration: "+err.Error())
	}
	config := c["config"].(map[string]any)
	query := c["query"].(map[string]any)
	if err := validateViewQuery(m.Context, m.Tx, collection, query); err != nil {
		return err
	}
	if kind == "board" || kind == "calendar" || kind == "timeline" {
		name := "groupPropertyId"
		if kind == "calendar" || kind == "timeline" {
			name = "datePropertyId"
		}
		p, err := propertyDefinition(m.Context, m.Tx, str(config[name]))
		if err != nil {
			return err
		}
		if string(p.CollectionId) != collection {
			return Fail("INVALID_REQUEST", "View property belongs to another collection")
		}
		if (kind == "calendar" || kind == "timeline") && p.Type != "date" || kind == "board" && p.Type != "select" && p.Type != "status" && p.Type != "checkbox" {
			return Fail("INVALID_REQUEST", "Incompatible board/calendar property")
		}
	}
	if kind == "gallery" {
		if id := str(config["coverPropertyId"]); id != "" {
			p, err := propertyDefinition(m.Context, m.Tx, id)
			if err != nil {
				return err
			}
			if string(p.CollectionId) != collection || p.Type != "files" {
				return Fail("INVALID_REQUEST", "Gallery cover must be a files property in this collection")
			}
		}
	}
	if raw, ok := config["columnWidths"].([]any); ok {
		for _, r := range raw {
			p, err := propertyDefinition(m.Context, m.Tx, str(r.(map[string]any)["propertyId"]))
			if err != nil {
				return err
			}
			if string(p.CollectionId) != collection {
				return Fail("INVALID_REQUEST", "Column belongs to another collection")
			}
		}
	}
	if container := str(c["containerId"]); container != "" {
		if err := m.Require("container", container); err != nil {
			return err
		}
	}
	_, err := m.Exec("INSERT INTO views(id,collection_id,container_id,owning_page_id,name,type,schema_version,query_json,config_json,revision) VALUES(?,?,?,?,?,?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET container_id=excluded.container_id,owning_page_id=excluded.owning_page_id,name=excluded.name,type=excluded.type,schema_version=excluded.schema_version,query_json=excluded.query_json,config_json=excluded.config_json", id, collection, nullable(c["containerId"]), page, c["name"], kind, 1, string(marshal(query)), string(marshal(config)))
	return err
}
func validateViewQuery(ctx context.Context, q querier, collection string, v map[string]any) error {
	check := func(id string) (contracts.DomainPropertyDefinition, error) {
		p, e := propertyDefinition(ctx, q, id)
		if e == nil && string(p.CollectionId) != collection {
			e = Fail("INVALID_REQUEST", "Query property belongs to another collection")
		}
		return p, e
	}
	for _, key := range []string{"groupByPropertyId", "subgroupByPropertyId"} {
		if id := str(v[key]); id != "" {
			if _, err := check(id); err != nil {
				return err
			}
		}
	}
	for _, raw := range v["visiblePropertyIds"].([]any) {
		if _, err := check(str(raw)); err != nil {
			return err
		}
	}
	for _, raw := range v["sorts"].([]any) {
		if _, err := check(str(raw.(map[string]any)["propertyId"])); err != nil {
			return err
		}
	}
	count := 0
	var filter func(any, int) error
	filter = func(raw any, depth int) error {
		if raw == nil {
			return nil
		}
		count++
		if depth > 16 || count > 512 {
			return Fail("RESOURCE_EXHAUSTED", "Filter is too complex")
		}
		f := raw.(map[string]any)
		op := str(f["op"])
		if op == "and" || op == "or" {
			for _, t := range f["terms"].([]any) {
				if err := filter(t, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		p, err := check(str(f["propertyId"]))
		if err != nil {
			return err
		}
		if op != "is_empty" {
			value, ok := f["value"]
			if !ok {
				return Fail("INVALID_REQUEST", "Filter needs a value")
			}
			if p.Type == "title" {
				if _, ok := value.(string); !ok {
					return Fail("INVALID_REQUEST", "Title filters use display text")
				}
			} else if op == "contains" && (p.Type == "text" || p.Type == "multi_select" || p.Type == "identity") {
				if _, ok := value.(string); !ok {
					return Fail("INVALID_REQUEST", "Contains filter requires text or option ID")
				}
			} else if p.Type == "formula" || p.Type == "rollup" {
				if _, ok := value.(map[string]any); ok {
					if err := contracts.Validate("domain", "property_number_v1", marshal(value)); err != nil {
						return Fail("INVALID_REQUEST", "Derived numeric filters require decimal values")
					}
				}
			} else if err := contracts.Validate("domain", "property_"+p.Type+"_v1", marshal(value)); err != nil {
				return Fail("INVALID_REQUEST", "Filter value has wrong type")
			}
		}
		return nil
	}
	return filter(v["filter"], 0)
}
func validateDatabaseCommit(m *Mutation) error {
	for _, change := range m.touched {
		if change.Target.EntityType == "record" {
			id := string(change.Target.EntityId)
			var collection, page, rr, pr string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT r.collection_id,r.page_id,r.revision,p.revision FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", id).Scan(&collection, &page, &rr, &pr); err != nil {
				return err
			}
			if rr != pr {
				return Fail("INVALID_REQUEST", "Record and page revisions diverged")
			}
			if err := m.Touch("collection", collection); err != nil {
				return err
			}
			if _, ok := m.touched[guardKey("page", page)]; !ok {
				return Fail("INVALID_REQUEST", "Record changes require the page revision")
			}
		}
	}
	// A todo occurrence can reference only an authorized task; state remains a
	// projection of its canonical status option and never lives in block data.
	for _, change := range m.touched {
		if change.Target.EntityType == "block" && !change.Deleted {
			var task string
			err := m.Tx.QueryRowContext(m.Context, "SELECT task_record_id FROM todo_references WHERE block_id=?", change.Target.EntityId).Scan(&task)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if err = m.Kernel.authorizeEntity(m.Context, m.Tx, m.Execution, "record", task); err != nil {
				return err
			}
		}
	}
	return nil
}

func readRecords(ctx context.Context, q querier, collection string) ([]contracts.DomainRecord, error) {
	rows, err := q.QueryContext(ctx, "SELECT r.id,r.collection_id,r.page_id,r.revision,r.sort_key,c.title_property_id FROM records r JOIN collections c ON c.id=r.collection_id WHERE (?='' OR r.collection_id=?) ORDER BY r.sort_key,r.id", collection, collection)
	if err != nil {
		return nil, err
	}
	records := []contracts.DomainRecord{}
	for rows.Next() {
		var r contracts.DomainRecord
		var title contracts.DomainId
		if err = rows.Scan(&r.Id, &r.CollectionId, &r.PageId, &r.Revision, &r.SortKey, &title); err != nil {
			rows.Close()
			return nil, err
		}
		r.Values = []contracts.DomainPropertyValue{{PropertyId: title, SchemaVersion: 1, Value: marshal(map[string]any{"pageId": r.PageId})}}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range records {
		r := &records[i]
		values, err := q.QueryContext(ctx, "SELECT property_id,schema_version,value_json FROM property_values WHERE record_id=? ORDER BY property_id", r.Id)
		if err != nil {
			return nil, err
		}
		for values.Next() {
			var v contracts.DomainPropertyValue
			var raw string
			if err = values.Scan(&v.PropertyId, &v.SchemaVersion, &raw); err != nil {
				values.Close()
				return nil, err
			}
			v.Value = json.RawMessage(raw)
			r.Values = append(r.Values, v)
		}
		err = values.Err()
		values.Close()
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}
func databaseSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, name string) (any, error) {
	if name == "records" {
		rs, err := readRecords(ctx, tx, "")
		if err != nil {
			return nil, err
		}
		out := []contracts.DomainRecord{}
		for _, r := range rs {
			if k.authorizePage(ctx, tx, e, string(r.PageId)) == nil {
				out = append(out, r)
			}
		}
		return out, nil
	}
	out := []any{}
	query := map[string]string{"collections": "SELECT id,workspace_id,owning_page_id,title,title_property_id,system_role,revision FROM collections", "properties": "SELECT id,collection_id,name,type,schema_version,config_json,revision FROM properties", "views": "SELECT id,collection_id,container_id,owning_page_id,name,type,schema_version,query_json,config_json,revision FROM views", "containers": "SELECT id,workspace_id,owning_page_id,kind,label,parent_project_id,revision FROM containers", "containerCollections": "SELECT container_id,collection_id FROM container_collections"}[name]
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v any
		var kind, id string
		switch name {
		case "collections":
			var x contracts.DomainCollection
			err = rows.Scan(&x.Id, &x.WorkspaceId, &x.OwningPageId, &x.Title, &x.TitlePropertyId, &x.SystemRole, &x.Revision)
			v = x
			kind = "collection"
			id = string(x.Id)
		case "properties":
			var x contracts.DomainPropertyDefinition
			var raw string
			err = rows.Scan(&x.Id, &x.CollectionId, &x.Name, &x.Type, &x.SchemaVersion, &raw, &x.Revision)
			x.Config = json.RawMessage(raw)
			v = x
			kind = "property"
			id = string(x.Id)
		case "views":
			var x contracts.DomainView
			var container sql.NullString
			var query, config string
			err = rows.Scan(&x.Id, &x.CollectionId, &container, &x.OwningPageId, &x.Name, &x.Type, &x.SchemaVersion, &query, &config, &x.Revision)
			if container.Valid {
				c := contracts.DomainId(container.String)
				x.ContainerId = &c
			}
			if err == nil {
				err = json.Unmarshal([]byte(query), &x.Query)
			}
			x.Config = json.RawMessage(config)
			v = x
			kind = "view"
			id = string(x.Id)
		case "containers":
			var x contracts.DomainContainer
			var parent sql.NullString
			err = rows.Scan(&x.Id, &x.WorkspaceId, &x.OwningPageId, &x.Kind, &x.Label, &parent, &x.Revision)
			if parent.Valid {
				p := contracts.DomainId(parent.String)
				x.ParentProjectId = &p
			}
			v = x
			kind = "container"
			id = string(x.Id)
		case "containerCollections":
			var x contracts.DomainContainerCollection
			err = rows.Scan(&x.ContainerId, &x.CollectionId)
			if err == nil && k.authorizeEntity(ctx, tx, e, "collection", string(x.CollectionId)) != nil {
				continue
			}
			v = x
			kind = "container"
			id = string(x.ContainerId)
		}
		if err != nil {
			return nil, err
		}
		if k.authorizeEntity(ctx, tx, e, kind, id) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func textValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := []string{}
		for _, part := range x {
			parts = append(parts, textValue(part))
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"decimal", "start", "text", "displayName", "label"} {
			if val, ok := x[key]; ok {
				return str(val)
			}
		}
	}
	return string(marshal(v))
}
func compareValue(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		if av, bv := str(am["decimal"]), str(bm["decimal"]); av != "" && bv != "" {
			aa, ao := new(big.Rat).SetString(av)
			bb, bo := new(big.Rat).SetString(bv)
			if ao && bo {
				return aa.Cmp(bb)
			}
		}
		if am["hasTime"] == true && bm["hasTime"] == true {
			aa, ae := time.Parse(time.RFC3339Nano, str(am["start"]))
			bb, be := time.Parse(time.RFC3339Nano, str(bm["start"]))
			if ae == nil && be == nil {
				if aa.Before(bb) {
					return -1
				}
				if aa.After(bb) {
					return 1
				}
				return 0
			}
		}
	}
	return strings.Compare(textValue(a), textValue(b))
}
func matchesFilter(raw any, row map[string]any) bool {
	if raw == nil {
		return true
	}
	f := raw.(map[string]any)
	op := str(f["op"])
	if op == "and" || op == "or" {
		for _, term := range f["terms"].([]any) {
			value := matchesFilter(term, row)
			if op == "and" && !value {
				return false
			}
			if op == "or" && value {
				return true
			}
		}
		return op == "and"
	}
	v := row[str(f["propertyId"])]
	target := f["value"]
	cmp := compareValue(v, target)
	switch op {
	case "eq":
		return equalValue(v, target)
	case "ne":
		return !equalValue(v, target)
	case "lt":
		return v != nil && cmp < 0
	case "lte":
		return v != nil && cmp <= 0
	case "gt":
		return v != nil && cmp > 0
	case "gte":
		return v != nil && cmp >= 0
	case "is_empty":
		return v == nil || textValue(v) == ""
	case "contains":
		if items, ok := v.([]any); ok {
			richText := len(items) > 0
			for _, item := range items {
				object, objectOK := item.(map[string]any)
				if !objectOK || object["text"] == nil {
					richText = false
				}
				if equalValue(item, target) {
					return true
				}
			}
			if !richText {
				return false
			}
		}
		return strings.Contains(strings.ToLower(textValue(v)), strings.ToLower(textValue(target)))
	}
	return false
}

// Equality does not flatten boundaries in multi-select arrays or preserved
// source objects. Text marks do not affect textual filters; decimal formatting
// does not affect numeric equality.
func equalValue(a, b any) bool {
	if am, ok := a.(map[string]any); ok {
		if bm, ok := b.(map[string]any); ok && am["decimal"] != nil && bm["decimal"] != nil {
			return compareValue(a, b) == 0
		}
	}
	aa, aok := a.([]any)
	bb, bok := b.([]any)
	if aok && bok {
		rich := func(items []any) bool {
			for _, v := range items {
				m, ok := v.(map[string]any)
				if !ok || m["text"] == nil {
					return false
				}
			}
			return true
		}
		if rich(aa) && rich(bb) {
			return textValue(aa) == textValue(bb)
		}
		if len(aa) != len(bb) {
			return false
		}
		left, right := []string{}, []string{}
		for _, v := range aa {
			b, _ := canonicalJSON(v)
			left = append(left, string(b))
		}
		for _, v := range bb {
			b, _ := canonicalJSON(v)
			right = append(right, string(b))
		}
		sort.Strings(left)
		sort.Strings(right)
		for i := range left {
			if left[i] != right[i] {
				return false
			}
		}
		return true
	}
	aj, ae := canonicalJSON(a)
	bj, be := canonicalJSON(b)
	return ae == nil && be == nil && string(aj) == string(bj)
}

func clearRecordValue(m *Mutation, record, property string) error {
	var collection, page string
	var deleted sql.NullString
	if err := m.Tx.QueryRowContext(m.Context, "SELECT r.collection_id,r.page_id,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&collection, &page, &deleted); err != nil {
		return err
	}
	if deleted.Valid {
		return Fail("INVALID_REQUEST", "Cannot edit a trashed record")
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := m.Require("property", property); err != nil {
		return err
	}
	p, err := propertyDefinition(m.Context, m.Tx, property)
	if err != nil {
		return err
	}
	if string(p.CollectionId) != collection || p.Type == "title" {
		return Fail("INVALID_REQUEST", "Cannot clear this property's value")
	}
	var bound int
	if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM task_facets WHERE record_id=? AND status_property_id=?", record, property).Scan(&bound); err != nil {
		return err
	}
	if bound != 0 {
		return Fail("INVALID_REQUEST", "Task status cannot be cleared")
	}
	if err = syncReciprocalRelation(m, record, p, []any{}); err != nil {
		return err
	}
	_, err = m.Exec("DELETE FROM property_values WHERE record_id=? AND property_id=?", record, property)
	return err
}
