package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"trove.local/phase0/contracts"
)

type portableDatabaseSource struct {
	Title          string                               `json:"title,omitempty"`
	ID             string                               `json:"id"`
	Path           string                               `json:"path"`
	OwningPagePath string                               `json:"owningPagePath"`
	Properties     []contracts.DomainPropertyDefinition `json:"properties"`
	Records        []portableRecordSource               `json:"records"`
	Views          []portableViewSource                 `json:"views"`
}
type portableRecordSource struct {
	KnownProperties []string       `json:"knownProperties,omitempty"`
	ID              string         `json:"id"`
	PageID          string         `json:"pageId"`
	PagePath        string         `json:"pagePath"`
	Values          map[string]any `json:"values"`
}
type portableViewSource struct {
	ContainerID    string         `json:"containerId,omitempty"`
	OwningPagePath string         `json:"owningPagePath,omitempty"`
	OwningPageID   string         `json:"owningPageId,omitempty"`
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	SchemaVersion  int64          `json:"schemaVersion"`
	Query          map[string]any `json:"query"`
	Config         map[string]any `json:"config"`
}
type PortableCollectionImport struct {
	SchemaComplete bool                                 `json:"schemaComplete,omitempty"`
	CollectionID   string                               `json:"collectionId"`
	PageID         string                               `json:"pageId"`
	Title          string                               `json:"title"`
	Locator        string                               `json:"sourceLocator"`
	Properties     []contracts.DomainPropertyDefinition `json:"properties"`
	Records        []portableRecordSource               `json:"records"`
	Views          []portableViewSource                 `json:"views"`
}

// restorePortableDatabase freezes only supplied, validated canonical definitions.
// It never infers executable formula definitions from CSV display values.
func (k *Kernel) restorePortableDatabase(p *ImportPlan, manifest portableHierarchy) {
	if len(manifest.Collections) == 0 {
		return
	}
	sources := 0
	for _, c := range manifest.Collections {
		if len(c.Properties) > 0 {
			sources++
		}
	}
	if sources == 0 {
		return
	}
	fail := func(reason string) {
		for i := range p.Items {
			item := &p.Items[i]
			for _, c := range manifest.Collections {
				if item.Locator == c.Path {
					item.Outcome = "partial"
					item.Reason += "; canonical database evidence retained without reconstruction: " + reason
				}
			}
		}
		p.SourceOmissions = append(p.SourceOmissions, "Supplied canonical database graph retained; live reconstruction was unavailable: "+reason)
	}
	ids := map[string]string{}
	pages := map[string]string{}
	items := map[string]*ImportItem{}
	for i := range p.Items {
		item := &p.Items[i]
		items[item.Locator] = item
		if item.PageID != "" {
			pages[item.Locator] = item.PageID
		}
	}
	for _, page := range manifest.Pages {
		if local := pages[page.Path]; local != "" && page.CanonicalPage.Id != "" {
			ids[string(page.CanonicalPage.Id)] = local
		}
	}
	used := map[string]bool{}
	for id := range ids {
		used[id] = true
	}
	total := 0
	for _, c := range manifest.Collections {
		total += len(c.Properties) + len(c.Records) + len(c.Views) + 1
		if len(c.Properties) > 256 {
			fail("collection exceeds 256 properties")
			return
		}
	}
	if total > 5000 {
		fail("canonical graph exceeds 5000 entities")
		return
	}
	fresh := func(kind, old string) (string, bool) {
		if contracts.Validate("domain", "Id", marshal(old)) != nil || used[old] {
			return "", false
		}
		used[old] = true
		id := NewID()
		ids[old] = id
		trackImportIdentity(p, kind, old, id)
		return id, true
	}
	// Allocate the graph before rewriting any cross-collection references.
	for _, c := range manifest.Collections {
		if c.ID == "" || len(c.Properties) == 0 || items[c.Path] == nil || pages[c.OwningPagePath] == "" {
			fail("collection identity, schemas or owning page are missing")
			return
		}
		if _, ok := fresh("collection", c.ID); !ok {
			fail("duplicate or invalid collection identity")
			return
		}
		for _, prop := range c.Properties {
			if string(prop.CollectionId) != c.ID {
				fail("property belongs to another source collection")
				return
			}
			if _, ok := fresh("property", string(prop.Id)); !ok {
				fail("duplicate or invalid property identity")
				return
			}
			var cfg map[string]any
			json.Unmarshal(prop.Config, &cfg)
			if options, ok := cfg["options"].([]any); ok {
				for _, raw := range options {
					if option, ok := raw.(map[string]any); ok {
						if _, ok = fresh("option", str(option["id"])); !ok {
							fail("invalid option identity")
							return
						}
					}
				}
			}
		}
		for _, r := range c.Records {
			if pages[r.PagePath] == "" {
				fail("record body page is absent")
				return
			}
			if _, ok := fresh("record", r.ID); !ok {
				fail("invalid record identity")
				return
			}
			ids[r.PageID] = pages[r.PagePath]
		}
		for _, v := range c.Views {
			if _, ok := fresh("view", v.ID); !ok {
				fail("invalid view identity")
				return
			}
		}
	}
	for _, a := range manifest.Attachments {
		if item := items[a.Path]; item != nil && item.Kind != "rejected" {
			if ids[a.ID] != "" {
				fail("attachment identity collides with a canonical entity")
				return
			}
			ids[a.ID] = item.RawFileID
		}
	}
	plans := []PortableCollectionImport{}
	unsupportedViews := map[string]int{}
	for _, source := range manifest.Collections {
		csv := items[source.Path]
		if csv.Kind != "csv" {
			fail("readable CSV source is unavailable")
			return
		}
		plan := PortableCollectionImport{SchemaComplete: true, CollectionID: ids[source.ID], PageID: pages[source.OwningPagePath], Title: csv.Title, Locator: source.Path, Properties: []contracts.DomainPropertyDefinition{}, Records: []portableRecordSource{}, Views: []portableViewSource{}}
		if source.Title != "" {
			plan.Title = source.Title
		}
		titles := 0
		for _, prop := range source.Properties {
			var config any
			if json.Unmarshal(prop.Config, &config) != nil {
				fail("invalid property configuration")
				return
			}
			config = rewritePortableDatabaseValue(config, ids, "")
			prop.Id = contracts.DomainId(ids[string(prop.Id)])
			prop.CollectionId = contracts.DomainId(plan.CollectionID)
			prop.Revision = "1"
			prop.Config = marshal(config)
			if prop.Type == "title" {
				titles++
			}
			if prop.SchemaVersion != 1 || contracts.Validate("domain", "property_"+prop.Type+"_config_v1", prop.Config) != nil {
				fail("unsupported property schema " + prop.Type)
				return
			}
			if prop.Type == "relation" {
				cfg := config.(map[string]any)
				target := str(cfg["targetCollectionId"])
				mapped := false
				for _, s := range manifest.Collections {
					if target == ids[s.ID] {
						mapped = true
					}
				}
				if !mapped {
					fail("relation target was not supplied")
					return
				}
			}
			plan.Properties = append(plan.Properties, prop)
		}
		if titles != 1 {
			fail("collection needs exactly one title property")
			return
		}
		sort.SliceStable(plan.Properties, func(i, j int) bool { return plan.Properties[i].Type == "title" && plan.Properties[j].Type != "title" })
		for _, r := range source.Records {
			local := portableRecordSource{ID: ids[r.ID], PageID: pages[r.PagePath], PagePath: r.PagePath, Values: map[string]any{}}
			for old, value := range r.Values {
				property := ids[old]
				var prop *contracts.DomainPropertyDefinition
				for i := range plan.Properties {
					if string(plan.Properties[i].Id) == property {
						prop = &plan.Properties[i]
						break
					}
				}
				if prop == nil || prop.Type == "title" {
					fail("record contains an unknown property")
					return
				}
				value = rewritePortablePropertyValue(value, prop.Type, ids)
				if contracts.Validate("domain", "property_"+prop.Type+"_v1", marshal(value)) != nil {
					fail("record value does not match supplied schema")
					return
				}
				local.Values[property] = value
			}
			for _, prop := range plan.Properties {
				if prop.Type != "title" {
					local.KnownProperties = append(local.KnownProperties, string(prop.Id))
				}
			}
			plan.Records = append(plan.Records, local)
		}
		for _, view := range source.Views {
			if view.Type != "table" && view.Type != "list" && view.Type != "board" && view.Type != "calendar" && view.Type != "gallery" && view.Type != "timeline" {
				unsupportedViews[source.Path]++
				continue
			}
			view.ID = ids[view.ID]
			if view.OwningPagePath != "" {
				view.OwningPageID = pages[view.OwningPagePath]
				if view.OwningPageID == "" {
					fail("linked view destination page is absent")
					return
				}
			} else {
				view.OwningPageID = plan.PageID
			}
			view.Config = rewritePortableDatabaseValue(view.Config, ids, "").(map[string]any)
			view.Query = rewritePortableDatabaseQuery(view.Query, source.Properties, ids)
			if view.SchemaVersion != 1 || contracts.Validate("domain", "view_"+view.Type+"_v1", marshal(view.Config)) != nil || contracts.Validate("domain", "ViewQuery", marshal(view.Query)) != nil {
				fail("unsupported view configuration")
				return
			}
			plan.Views = append(plan.Views, view)
		}
		plans = append(plans, plan)
	}
	// Check all supported dependency definitions before changing the CSV fallback.
	graph := map[string][]string{}
	known := map[string]bool{}
	for _, c := range plans {
		for _, prop := range c.Properties {
			known[string(prop.Id)] = true
		}
	}
	for _, c := range plans {
		for _, prop := range c.Properties {
			var cfg map[string]any
			json.Unmarshal(prop.Config, &cfg)
			raw, _ := cfg["raw"].(map[string]any)
			if prop.Type == "formula" {
				deps, err := formulaDependencies(str(raw["translatedExpression"]))
				if err == nil {
					for _, dep := range deps {
						if !known[dep] {
							fail("formula target property is missing")
							return
						}
					}
					graph[string(prop.Id)] = deps
				}
			}
			if prop.Type == "rollup" {
				if relation, target := str(raw["relationPropertyId"]), str(raw["targetPropertyId"]); relation != "" || target != "" {
					if !known[relation] || !known[target] {
						fail("rollup dependencies are missing")
						return
					}
					graph[string(prop.Id)] = []string{target}
				}
			}
		}
	}
	if graphCycle(graph) {
		fail("derived definitions contain a dependency cycle")
		return
	}
	files := map[string]bool{}
	for _, a := range manifest.Attachments {
		if local := ids[a.ID]; local != "" {
			files[local] = true
		}
	}
	if err := validatePortableDatabaseGraph(plans, files); err != nil {
		fail(err.Error())
		return
	}
	for _, plan := range plans {
		csv := items[plan.Locator]
		csv.Kind = "portable_collection"
		csv.CollectionID = plan.CollectionID
		csv.Outcome = "preserved"
		csv.Reason = "Supplied canonical property schemas, values, record identities and supported views reconstructed with new local IDs; original evidence retained"
		if n := unsupportedViews[plan.Locator]; n > 0 {
			csv.Outcome = "partial"
			csv.Reason += fmt.Sprintf("; %d unsupported views retained in manifest only", n)
		}
		rowIndex := 0
		for i := range p.Items {
			row := &p.Items[i]
			if row.CSVSource != plan.Locator || row.Kind != "row" {
				continue
			}
			row.Kind = "portable_record"
			if rowIndex < len(plan.Records) {
				r := plan.Records[rowIndex]
				row.PageID = r.PageID
				row.RecordID = r.ID
				row.CollectionID = plan.CollectionID
				row.ParentID = plan.PageID
				row.Outcome = "preserved"
				row.Reason = "Supplied typed canonical record values and page body reconstructed; CSV bytes retained"
			}
			rowIndex++
		}
		if rowIndex != len(plan.Records) { // Extra canonical pages still have their own inventory; no values are discarded.
			csv.Outcome = "partial"
			csv.Reason += "; CSV row count differs from canonical record inventory; canonical records take precedence"
		}
	}
	p.PortableCollections = plans
	unknown := []string{}
	for _, notice := range p.UnknownCompleteness {
		remove := false
		for _, plan := range plans {
			if strings.HasPrefix(notice, "CSV "+plan.Locator+":") {
				remove = true
			}
		}
		if !remove {
			unknown = append(unknown, notice)
		}
	}
	p.UnknownCompleteness = unknown
}
func rewritePortableDatabaseValue(v any, ids map[string]string, key string) any {
	if key == "originalExpression" || key == "importedCachedValue" {
		return v
	}
	switch x := v.(type) {
	case string:
		if key == "translatedExpression" {
			if translated, err := TranslateImportedFormula(x, ids); err == nil {
				return translated
			}
			return x
		}
		references := map[string]bool{"id": true, "collectionId": true, "targetCollectionId": true, "bidirectionalPropertyId": true, "relationPropertyId": true, "targetPropertyId": true, "propertyId": true, "defaultOptionId": true, "doneOptionId": true, "reopenOptionId": true, "groupPropertyId": true, "datePropertyId": true, "coverPropertyId": true, "groupByPropertyId": true, "subgroupByPropertyId": true, "visiblePropertyIds": true, "fileId": true, "pageId": true, "recordId": true, "optionId": true}
		if references[key] {
			if id := ids[x]; id != "" {
				return id
			}
		}
		if key == "link" && strings.HasPrefix(x, "trove://page/") {
			page, block, ok := parseCanonicalPageLink(x)
			if id := ids[page]; ok && id != "" {
				if mapped := ids[block]; mapped != "" {
					block = mapped
				}
				return canonicalPageLink(id, block)
			}
		}
		return x
	case []any:
		out := []any{}
		for _, v := range x {
			out = append(out, rewritePortableDatabaseValue(v, ids, key))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[k] = rewritePortableDatabaseValue(v, ids, k)
		}
		// Version-two AST/hash metadata is derived from the translated
		// expression. Remapping property IDs must regenerate those references.
		// Original source text and captured results remain retained evidence.
		if expression := str(out["translatedExpression"]); expression != "" && number(out["evaluatorVersion"]) == 2 {
			if ast, _, err := formulaCompileV2(expression); err == nil {
				hash := sha256.Sum256([]byte(expression))
				out["translatedAST"], out["expressionHash"] = ast, hex.EncodeToString(hash[:])
			}
		}
		return out
	}
	return v
}
func remapPortableDatabasePages(p *ImportPlan, ids map[string]string) {
	for i := range p.NotionContainers {
		if id := ids[p.NotionContainers[i].PageID]; id != "" {
			p.NotionContainers[i].PageID = id
		}
	}
	for i := range p.PortableCollections {
		c := &p.PortableCollections[i]
		if id := ids[c.PageID]; id != "" {
			c.PageID = id
		}
		for j := range c.Views {
			if id := ids[c.Views[j].OwningPageID]; id != "" {
				c.Views[j].OwningPageID = id
			}
		}
		for j := range c.Records {
			if id := ids[c.Records[j].PageID]; id != "" {
				c.Records[j].PageID = id
			}
		}
	}
}

// commitPortableDatabase is one atomic final import unit. Canonical commands run
// through their normal handlers and the frozen import plan's guarded IDs.
func commitPortableDatabase(m *Mutation, p *ImportPlan) error {
	if p.TypedDatabaseMerge {
		return commitMergedImportDatabases(m, p)
	}
	if p.NotionKeepGraph {
		return nil
	}
	if len(p.PortableCollections) == 0 {
		return commitNotionContainers(m, p)
	}
	for _, item := range p.Items {
		if item.Kind == "portable_collection" && item.Choice == "keep_local" {
			for i := range p.Items {
				if p.Items[i].Kind == "portable_collection" || p.Items[i].Kind == "portable_record" {
					p.Items[i].Outcome = "partial"
					p.Items[i].Reason += "; canonical graph retained without activation because a destination database was kept local"
				}
			}
			return nil
		}
	}
	for _, c := range p.PortableCollections {
		for _, ref := range [][2]string{{"collection", c.CollectionID}, {"page", c.PageID}} {
			if err := guardImport(m, ref[0], ref[1]); err != nil {
				return err
			}
		}
		for _, prop := range c.Properties {
			if err := guardImport(m, "property", string(prop.Id)); err != nil {
				return err
			}
		}
		for _, r := range c.Records {
			if err := guardImport(m, "record", r.ID); err != nil {
				return err
			}
			if err := importGuardPage(m, r.PageID); err != nil {
				return err
			}
		}
		for _, v := range c.Views {
			if err := importGuardPage(m, v.OwningPageID); err != nil {
				return err
			}
			if err := guardImport(m, "view", v.ID); err != nil {
				return err
			}
		}
	}
	for _, c := range p.PortableCollections {
		if err := databaseCommand(m, map[string]any{"kind": "collection.create", "collectionId": c.CollectionID, "owningPageId": c.PageID, "title": c.Title, "titlePropertyId": string(c.Properties[0].Id)}); err != nil {
			return err
		}
	}
	for _, c := range p.PortableCollections {
		for _, prop := range c.Properties[1:] {
			var config map[string]any
			json.Unmarshal(prop.Config, &config)
			if err := databaseCommand(m, map[string]any{"kind": "collection.addProperty", "collectionId": c.CollectionID, "propertyId": string(prop.Id), "name": prop.Name, "type": prop.Type, "schemaVersion": float64(prop.SchemaVersion), "config": config}); err != nil {
				return err
			}
		}
	}
	for _, c := range p.PortableCollections {
		for i, r := range c.Records {
			if err := m.TouchPage(r.PageID); err != nil {
				return err
			}
			if err := requireNew(m, "record", r.ID); err != nil {
				return err
			}
			var rev string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM pages WHERE id=?", r.PageID).Scan(&rev); err != nil {
				return err
			}
			if _, err := m.Exec("INSERT INTO records VALUES(?,?,?,?,?)", r.ID, c.CollectionID, r.PageID, fmt.Sprintf("%012d", i+1), rev); err != nil {
				return err
			}
			change := m.touched[guardKey("record", r.ID)]
			change.After = m.touched[guardKey("page", r.PageID)].After
			m.touched[guardKey("record", r.ID)] = change
		}
	}
	for _, c := range p.PortableCollections {
		for _, r := range c.Records {
			keys := []string{}
			for id := range r.Values {
				keys = append(keys, id)
			}
			sort.Strings(keys)
			for _, id := range keys {
				prop, err := propertyDefinition(m.Context, m.Tx, id)
				if err != nil {
					return err
				}
				value := r.Values[id]
				if prop.Type == "formula" || prop.Type == "rollup" { // Captured source values are inert evidence, never native calculated writes.
					if err = validatePropertyValue(m, prop, value); err != nil {
						return err
					}
					if _, err = m.Exec("INSERT INTO property_values VALUES(?,?,?,?,?,?)", r.ID, c.CollectionID, id, prop.Type, prop.SchemaVersion, string(marshal(value))); err != nil {
						return err
					}
					continue
				}
				if err = setRecordValue(m, r.ID, id, map[string]any{"type": prop.Type, "schemaVersion": float64(prop.SchemaVersion), "value": value}); err != nil {
					return err
				}
			}
		}
	}
	if err := commitNotionContainers(m, p); err != nil {
		return err
	}
	for _, c := range p.PortableCollections {
		for _, v := range c.Views {
			if err := saveView(m, map[string]any{"kind": "view.save", "viewId": v.ID, "containerId": v.ContainerID, "collectionId": c.CollectionID, "owningPageId": v.OwningPageID, "name": v.Name, "type": v.Type, "schemaVersion": float64(v.SchemaVersion), "config": v.Config, "query": v.Query}); err != nil {
				return err
			}
		}
	}
	for _, item := range p.Items {
		if item.MappingID != "" {
			if change, ok := m.touched[guardKey("page", item.PageID)]; ok {
				if _, err := m.Exec("INSERT INTO import_mapping_state VALUES(?,?) ON CONFLICT(mapping_id) DO UPDATE SET local_revision=excluded.local_revision", item.MappingID, change.After); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func rewritePortablePropertyValue(v any, kind string, ids map[string]string) any {
	key := ""
	switch kind {
	case "relation":
		key = "recordId"
	case "select", "status", "multi_select":
		key = "optionId"
	case "formula", "rollup", "unsupported_external", "identity", "author", "place":
		return v
	}
	return rewritePortableDatabaseValue(v, ids, key)
}
func rewritePortableDatabaseQuery(query map[string]any, properties []contracts.DomainPropertyDefinition, ids map[string]string) map[string]any {
	types := map[string]string{}
	for _, p := range properties {
		types[string(p.Id)] = p.Type
	}
	var filter func(any)
	filter = func(raw any) {
		f, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if terms, ok := f["terms"].([]any); ok {
			for _, t := range terms {
				filter(t)
			}
		}
		if value, ok := f["value"]; ok {
			f["value"] = rewritePortablePropertyValue(value, types[str(f["propertyId"])], ids)
		}
	}
	var copy map[string]any
	json.Unmarshal(marshal(query), &copy)
	filter(copy["filter"])
	return rewritePortableDatabaseValue(copy, ids, "").(map[string]any)
}

func validatePortableDatabaseGraph(plans []PortableCollectionImport, files map[string]bool) error {
	props := map[string]contracts.DomainPropertyDefinition{}
	records := map[string]string{}
	pages := map[string]bool{}
	for _, c := range plans {
		for _, p := range c.Properties {
			props[string(p.Id)] = p
		}
		for _, r := range c.Records {
			if pages[r.PageID] {
				return fmt.Errorf("multiple records reference one canonical page")
			}
			pages[r.PageID] = true
			records[r.ID] = c.CollectionID
		}
	}
	for _, c := range plans {
		for _, p := range c.Properties {
			var cfg map[string]any
			json.Unmarshal(p.Config, &cfg)
			raw, _ := cfg["raw"].(map[string]any)
			switch p.Type {
			case "relation":
				if back := str(cfg["bidirectionalPropertyId"]); back != "" {
					b, ok := props[back]
					if !ok || b.Type != "relation" || string(b.CollectionId) != str(cfg["targetCollectionId"]) {
						return fmt.Errorf("reciprocal relation definition is missing")
					}
					var bc map[string]any
					json.Unmarshal(b.Config, &bc)
					if str(bc["bidirectionalPropertyId"]) != string(p.Id) || str(bc["targetCollectionId"]) != c.CollectionID {
						return fmt.Errorf("reciprocal relation does not point back")
					}
				}
			case "formula":
				if deps, err := formulaDependencies(str(raw["translatedExpression"])); err == nil {
					for _, id := range deps {
						if string(props[id].CollectionId) != c.CollectionID {
							return fmt.Errorf("formula dependency is not in its collection")
						}
					}
				}
			case "rollup":
				relation, target := str(raw["relationPropertyId"]), str(raw["targetPropertyId"])
				if relation != "" || target != "" {
					rp := props[relation]
					var rc map[string]any
					json.Unmarshal(rp.Config, &rc)
					if rp.Type != "relation" || string(rp.CollectionId) != c.CollectionID || str(rc["targetCollectionId"]) != string(props[target].CollectionId) {
						return fmt.Errorf("rollup dependencies do not match their relation")
					}
				}
			}
		}
		for _, r := range c.Records {
			for id, value := range r.Values {
				p := props[id]
				var cfg map[string]any
				json.Unmarshal(p.Config, &cfg)
				switch p.Type {
				case "relation":
					values, _ := value.([]any)
					if cfg["cardinality"] == "one" && len(values) > 1 {
						return fmt.Errorf("relation cardinality exceeds one")
					}
					for _, target := range values {
						if records[str(target)] != str(cfg["targetCollectionId"]) {
							return fmt.Errorf("relation record target is absent from supplied graph")
						}
					}
				case "files":
					values, _ := value.([]any)
					for _, v := range values {
						f, _ := v.(map[string]any)
						if !files[str(f["fileId"])] {
							return fmt.Errorf("record file value references an unavailable attachment")
						}
					}
				case "select", "status", "multi_select":
					valid := map[string]bool{}
					options, _ := cfg["options"].([]any)
					for _, o := range options {
						option, _ := o.(map[string]any)
						valid[str(option["id"])] = true
					}
					values := []any{value}
					if p.Type == "multi_select" {
						values, _ = value.([]any)
					}
					for _, v := range values {
						if v != nil && !valid[str(v)] {
							return fmt.Errorf("record option is absent from its supplied schema")
						}
					}
				}
			}
		}
		for _, view := range c.Views {
			check := func(id string) error {
				if id != "" && string(props[id].CollectionId) != c.CollectionID {
					return fmt.Errorf("view references a property outside its collection")
				}
				return nil
			}
			for _, key := range []string{"groupPropertyId", "datePropertyId", "coverPropertyId"} {
				if err := check(str(view.Config[key])); err != nil {
					return err
				}
			}
			for _, key := range []string{"groupByPropertyId", "subgroupByPropertyId"} {
				if err := check(str(view.Query[key])); err != nil {
					return err
				}
			}
			if ids, ok := view.Query["visiblePropertyIds"].([]any); ok {
				for _, id := range ids {
					if err := check(str(id)); err != nil {
						return err
					}
				}
			}
			if sorts, ok := view.Query["sorts"].([]any); ok {
				for _, v := range sorts {
					s, _ := v.(map[string]any)
					if err := check(str(s["propertyId"])); err != nil {
						return err
					}
				}
			}
			var filter func(any) error
			filter = func(raw any) error {
				f, ok := raw.(map[string]any)
				if !ok {
					return nil
				}
				if err := check(str(f["propertyId"])); err != nil {
					return err
				}
				if terms, ok := f["terms"].([]any); ok {
					for _, t := range terms {
						if err := filter(t); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := filter(view.Query["filter"]); err != nil {
				return err
			}
			if view.Type == "calendar" || view.Type == "timeline" {
				if props[str(view.Config["datePropertyId"])].Type != "date" {
					return fmt.Errorf("calendar/timeline needs a date property")
				}
			}
			if view.Type == "gallery" && str(view.Config["coverPropertyId"]) != "" && props[str(view.Config["coverPropertyId"])].Type != "files" {
				return fmt.Errorf("gallery cover is not a files property")
			}
			if view.Type == "board" {
				kind := props[str(view.Config["groupPropertyId"])].Type
				if kind != "checkbox" && kind != "select" && kind != "status" {
					return fmt.Errorf("board grouping property is incompatible")
				}
			}
		}
	}
	return nil
}
