package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"trove.local/phase0/contracts"
)

type NotionContainerImport struct {
	ID          string   `json:"id"`
	PageID      string   `json:"pageId"`
	Title       string   `json:"title"`
	SourceID    string   `json:"sourceId"`
	Collections []string `json:"collectionIds"`
}
type NotionIdentityImport struct {
	Kind      string `json:"kind"`
	SourceID  string `json:"sourceId"`
	ID        string `json:"id"`
	RawFileID string `json:"rawFileId"`
	Hash      string `json:"hash"`
}

func notionFallback(kind string, raw any, readable string) map[string]any {
	return map[string]any{"sourceType": "notion_" + kind, "sourceVersion": 1, "raw": raw, "readableText": readable, "reason": "unsupported_behavior"}
}
func notionSortedKeys[V any](m map[string]V) []string {
	out := []string{}
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func notionFieldOutcome(p *ImportPlan, obj notionCapturedObject, path, outcome, reason string) {
	if len(p.Items) >= 10000 {
		return
	}
	p.Items = append(p.Items, ImportItem{Locator: "notion/fields/" + obj.Task.Kind + "/" + obj.Task.ID + "/" + path, Kind: "asset", Outcome: outcome, Reason: reason, RawFileID: obj.RawFileID, Hash: importDigest(obj.Raw), Size: int64(len(obj.Raw))})
}
func (k *Kernel) reconstructNotionDatabases(ctx context.Context, q querier, s *NotionCapture, p *ImportPlan) error {
	ids := map[string]string{}
	objects := s.Objects
	addIdentity := func(kind, source string, obj notionCapturedObject) string {
		id := NewID()
		ids[kind+":"+source] = id
		p.NotionIdentities = append(p.NotionIdentities, NotionIdentityImport{kind, source, id, obj.RawFileID, importDigest(obj.Raw)})
		return id
	}
	sources := map[string]int{}
	pages := map[string]int{}
	for i := range p.Items {
		if p.Items[i].PageID != "" && p.Items[i].Kind != "asset" {
			pages[p.Items[i].PageID] = i
		}
	}
	for _, key := range notionSortedKeys(objects) {
		obj := objects[key]
		switch obj.Task.Kind {
		case "database":
			id := addIdentity("container", obj.Task.ID, obj)
			item := &p.Items[pages[obj.Task.PageID]]
			item.Kind = "notion_container"
			p.NotionContainers = append(p.NotionContainers, NotionContainerImport{ID: id, PageID: obj.Task.PageID, Title: item.Title, SourceID: obj.Task.ID, Collections: []string{}})
		case "data_source":
			if !obj.QueryComplete {
				item := &p.Items[pages[obj.Task.PageID]]
				item.Kind = "notion_page"
				item.Outcome = "partial"
				item.Reason = "Record query is incomplete or denied; captured schema retained without creating a misleading empty database"
				item.Blocks = append(item.Blocks, fallbackImportBlock(item, item.Reason))
				continue
			}
			id := addIdentity("collection", obj.Task.ID, obj)
			item := &p.Items[pages[obj.Task.PageID]]
			item.Kind = "portable_collection"
			item.CollectionID = id
			sources[obj.Task.ID] = len(p.PortableCollections)
			p.PortableCollections = append(p.PortableCollections, PortableCollectionImport{CollectionID: id, PageID: item.PageID, Title: item.Title, Locator: item.Locator, Properties: []contracts.DomainPropertyDefinition{}, Records: []portableRecordSource{}, Views: []portableViewSource{}})
		}
	}
	p.NotionGraph = len(sources) > 0 || len(p.NotionContainers) > 0
	if len(sources) == 0 {
		return nil
	}
	for _, key := range notionSortedKeys(objects) {
		obj := objects[key]
		if obj.Task.Kind != "page" || obj.Task.SourceID == "" || obj.Task.Template {
			continue
		}
		if _, ok := sources[obj.Task.SourceID]; ok {
			addIdentity("record", obj.Task.ID, obj)
		}
	}
	// Allocate every property first: expressions, relation schemas and view
	// references can then resolve across all explicitly captured data sources.
	propertyIDs := map[string]map[string]string{}
	optionIDs := map[string]string{}
	for _, sourceID := range notionSortedKeys(sources) {
		obj := objects["data_source:"+sourceID]
		defs := asObject(notionObject(obj.Raw)["properties"])
		if len(defs) > 256 {
			return Fail("RESOURCE_EXHAUSTED", "Data source exceeds 256 properties")
		}
		propertyIDs[sourceID] = map[string]string{}
		for _, name := range notionSortedKeys(defs) {
			def := asObject(defs[name])
			external := str(def["id"])
			if external == "" {
				return Fail("INVALID_REQUEST", "Source property has no stable identity")
			}
			id := addIdentity("property", sourceID+":"+external, obj)
			if propertyIDs[sourceID][external] != "" {
				return Fail("INVALID_REQUEST", "Source schema repeats a property identity")
			}
			propertyIDs[sourceID][external] = id
			for _, option := range asList(asObject(def[str(def["type"])])["options"]) {
				o := asObject(option)
				optionKey := sourceID + ":" + external + ":" + str(o["id"])
				optionIDs[optionKey] = NewID()
				trackImportIdentity(p, "option", optionKey, optionIDs[optionKey])
			}
		}
	}
	for _, sourceID := range notionSortedKeys(sources) {
		obj := objects["data_source:"+sourceID]
		defs := asObject(notionObject(obj.Raw)["properties"])
		for _, name := range notionSortedKeys(defs) {
			if propertyIDs[sourceID][name] == "" {
				propertyIDs[sourceID][name] = propertyIDs[sourceID][str(asObject(defs[name])["id"])]
			}
		}
	}
	for _, sourceID := range notionSortedKeys(sources) {
		obj := objects["data_source:"+sourceID]
		defs := asObject(notionObject(obj.Raw)["properties"])
		c := &p.PortableCollections[sources[sourceID]]
		titles := 0
		for _, name := range notionSortedKeys(defs) {
			def := asObject(defs[name])
			external, kind := str(def["id"]), str(def["type"])
			id := propertyIDs[sourceID][external]
			cfg := map[string]any{}
			native := kind
			outcome, reason := "adapted", "Supplied property schema mapped to the local typed property"
			data := asObject(def[kind])
			switch kind {
			case "title":
				titles++
			case "rich_text":
				native = "text"
			case "phone_number":
				native = "phone"
			case "people":
				native = "identity"
			case "created_by", "last_edited_by":
				native = "author"
			case "number":
				cfg["format"] = str(data["format"])
				if cfg["format"] == "" {
					cfg["format"] = "number"
				}
			case "checkbox", "date", "url", "email", "files", "created_time", "last_edited_time", "unique_id":
			case "select", "multi_select", "status":
				options := []any{}
				groups := map[string]string{}
				// Only explicit standard status groups map to task semantics. Unknown
				// source group labels remain evidence and receive no invented done state.
				for _, g := range asList(data["groups"]) {
					group := asObject(g)
					semantic := ""
					switch strings.ToLower(str(group["name"])) {
					case "to-do", "not started":
						semantic = "not_started"
					case "in progress":
						semantic = "in_progress"
					case "complete", "done":
						semantic = "done"
					}
					for _, oid := range asList(group["option_ids"]) {
						groups[str(oid)] = semantic
					}
				}
				valid := true
				for _, o := range asList(data["options"]) {
					option := asObject(o)
					oid := str(option["id"])
					local := optionIDs[sourceID+":"+external+":"+oid]
					entry := map[string]any{"id": local, "label": str(option["name"])}
					if kind == "status" {
						if groups[oid] == "" {
							valid = false
						}
						entry["group"] = groups[oid]
					}
					options = append(options, entry)
				}
				cfg["options"] = options
				if !valid || kind == "status" && len(options) == 0 {
					native = "unsupported_external"
					cfg = notionFallback(kind, def, "Status groups require an explicit local mapping")
					outcome = "unsupported_but_retained"
					reason = "Status groups were not inferred from option colors or unknown labels"
				}
			case "relation":
				target := notionNormalizeID(str(data["data_source_id"]))
				if local := ids["collection:"+target]; local != "" {
					cfg = map[string]any{"targetCollectionId": local, "bidirectionalPropertyId": nil, "cardinality": "many"}
					if data["type"] == "dual_property" {
						outcome = "partial"
						reason = "Relation targets mapped; reciprocal editing semantics retained in source evidence"
					}
				} else {
					native = "unsupported_external"
					cfg = notionFallback(kind, def, "Relation target is outside the captured authorized sources")
					outcome = "unsupported_but_retained"
					reason = "No inaccessible relation target was fetched or invented"
				}
			case "formula":
				original := str(data["expression"])
				raw := map[string]any{"originalExpression": original, "sourceDefinition": def}
				if translated, err := TranslateImportedFormula(original, propertyIDs[sourceID]); err == nil {
					raw["translatedExpression"] = translated
				}
				cfg = notionFallback(kind, raw, original)
				outcome = "partial"
				reason = "Original formula preserved; only supported v1 syntax translated, per-record imported caches remain separate"
			case "rollup":
				cfg = notionFallback(kind, map[string]any{"sourceDefinition": def}, "Captured rollup definition; computed snapshots retained per record")
				outcome = "partial"
				reason = "Rollup definition and final source calculation retained without inventing local dependencies"
			default:
				native = "unsupported_external"
				cfg = notionFallback(kind, def, "Unsupported source property "+name)
				outcome = "unsupported_but_retained"
				reason = "Source property has no equivalent native contract"
			}
			prop := contracts.DomainPropertyDefinition{Id: contracts.DomainId(id), CollectionId: contracts.DomainId(c.CollectionID), Name: name, Type: native, SchemaVersion: 1, Config: marshal(cfg), Revision: "1"}
			if err := contracts.Validate("domain", "property_"+native+"_config_v1", marshal(cfg)); err != nil {
				return fmt.Errorf("Notion property %s: %w", name, err)
			}
			c.Properties = append(c.Properties, prop)
			notionFieldOutcome(p, obj, "schema/"+external, outcome, reason)
		}
		if titles != 1 {
			return Fail("INVALID_REQUEST", "Data source must supply exactly one title property")
		}
		sort.SliceStable(c.Properties, func(i, j int) bool { return c.Properties[i].Type == "title" && c.Properties[j].Type != "title" })
	}
	for _, key := range notionSortedKeys(objects) {
		obj := objects[key]
		t := obj.Task
		if t.Kind != "page" || t.SourceID == "" {
			continue
		}
		ci, ok := sources[t.SourceID]
		if !ok {
			continue
		}
		c := &p.PortableCollections[ci]
		item := &p.Items[pages[t.PageID]]
		item.Kind = "portable_record"
		if t.Template {
			item.Kind = "notion_template"
		}
		item.CollectionID = c.CollectionID
		item.RecordID = ids["record:"+t.ID]
		item.CSVSource = c.Locator
		record := portableRecordSource{ID: item.RecordID, PageID: t.PageID, Values: map[string]any{}}
		rawProps := asObject(notionObject(obj.Raw)["properties"])
		for _, name := range notionSortedKeys(rawProps) {
			supplied := asObject(rawProps[name])
			external, kind := str(supplied["id"]), str(supplied["type"])
			local := propertyIDs[t.SourceID][external]
			var prop *contracts.DomainPropertyDefinition
			for j := range c.Properties {
				if string(c.Properties[j].Id) == local {
					prop = &c.Properties[j]
					break
				}
			}
			if prop == nil {
				notionFieldOutcome(p, obj, "values/"+external, "unsupported_but_retained", "Page supplied a property absent from the captured schema")
				continue
			}
			value := supplied[kind]
			complete := true
			switch kind {
			case "title", "rich_text", "people", "relation", "rollup", "formula", "files":
				capture, exists := obj.Properties[external]
				complete = exists && capture.Complete
				if complete {
					if capture.Type != kind {
						complete = false
					} else if capture.Value != nil {
						var v any
						d := json.NewDecoder(strings.NewReader(string(capture.Value)))
						d.UseNumber()
						_ = d.Decode(&v)
						value = v
					} else {
						values := []any{}
						for _, raw := range capture.Items {
							v := notionObject(raw)
							values = append(values, v[kind])
						}
						value = values
					}
				}
			}
			if !complete {
				notionFieldOutcome(p, obj, "values/"+external, "partial", "Property pagination or calculation did not finish; the captured prefix was not activated as a complete value")
				continue
			}
			if prop.Type == "title" {
				title := plainNotionText(value)
				if title != "" {
					p.Items[pages[t.PageID]].Title = title
				}
				continue
			}
			converted, err := notionPropertyValue(obj, *prop, external, kind, value, ids, optionIDs)
			if err != nil {
				notionFieldOutcome(p, obj, "values/"+external, "partial", err.Error())
				continue
			}
			if converted == nil {
				record.KnownProperties = append(record.KnownProperties, local)
				continue
			}
			if err = contracts.Validate("domain", "property_"+prop.Type+"_v1", marshal(converted)); err != nil {
				notionFieldOutcome(p, obj, "values/"+external, "unsupported_but_retained", "Supplied value cannot satisfy the local typed contract; exact source retained")
				continue
			}
			record.KnownProperties = append(record.KnownProperties, local)
			record.Values[local] = converted
			notionFieldOutcome(p, obj, "values/"+external, "adapted", "Complete supplied value mapped; original API response remains evidence")
		}
		if t.Template {
			item := &p.Items[pages[t.PageID]]
			item.Outcome = "partial"
			item.Reason = "Template page and property defaults captured as inactive evidence; review before saving a local template. Source defaults and recurring behavior were not activated."
			supplied := map[string]any{"active": false, "collectionId": c.CollectionID, "propertyDefinitions": c.Properties, "propertyValues": record.Values, "sourcePageId": t.ID}
			item.Blocks = append(item.Blocks, fallbackImportBlock(item, "Inactive imported template defaults:\n"+string(marshal(supplied))))
			if metadata := asObject(item.Provenance); metadata != nil {
				metadata["inactiveTemplateDefaults"] = supplied
			}
		} else {
			c.Records = append(c.Records, record)
		}
	}
	for i := range p.NotionContainers {
		container := &p.NotionContainers[i]
		for _, sourceID := range notionSortedKeys(sources) {
			obj := objects["data_source:"+sourceID]
			parent := notionNormalizeID(str(asObject(notionObject(obj.Raw)["parent"])["database_id"]))
			if obj.Task.SourceID == container.SourceID || parent == container.SourceID {
				container.Collections = append(container.Collections, ids["collection:"+sourceID])
			}
		}
	}
	for _, key := range notionSortedKeys(objects) {
		obj := objects[key]
		if obj.Task.Kind != "view" {
			continue
		}
		v := notionObject(obj.Raw)
		sourceID := notionNormalizeID(str(v["data_source_id"]))
		ci, ok := sources[sourceID]
		if !ok {
			notionFieldOutcome(p, obj, "view", "unsupported_but_retained", "Linked view targets a source outside this capture; no records or permissions inferred")
			continue
		}
		c := &p.PortableCollections[ci]
		view, reason := notionView(v, propertyIDs[sourceID], c.Properties, optionIDs, sourceID)
		view.ID = addIdentity("view", obj.Task.ID, obj)
		view.OwningPageID = obj.Task.PageID
		view.Name = str(v["name"])
		if view.Name == "" {
			view.Name = "Imported view"
		}
		for _, container := range p.NotionContainers {
			if container.PageID == view.OwningPageID {
				view.ContainerID = container.ID
			}
		}
		c.Views = append(c.Views, view)
		notionFieldOutcome(p, obj, "view", "partial", reason)
		for i := range p.NotionContainers {
			container := &p.NotionContainers[i]
			if container.PageID == view.OwningPageID {
				found := false
				for _, id := range container.Collections {
					found = found || id == c.CollectionID
				}
				if !found {
					container.Collections = append(container.Collections, c.CollectionID)
				}
			}
		}
	}
	if len(p.Items) >= 10000 {
		return Fail("RESOURCE_EXHAUSTED", "Field-level import report exceeds 10,000 items; capture fewer roots")
	}
	files := map[string]bool{}
	for _, item := range p.Items {
		if item.RawFileID != "" {
			files[item.RawFileID] = true
		}
	}
	if err := validatePortableDatabaseGraph(p.PortableCollections, files); err != nil {
		return fmt.Errorf("Notion graph validation: %w", err)
	}
	// Hash source evidence without generated local IDs or local capture times. Source changes in any connected graph require one review.
	sourceHashes := []string{}
	for _, key := range notionSortedKeys(objects) {
		obj := objects[key]
		sourceHashes = append(sourceHashes, fmt.Sprintf("%s:%t:%s", key, obj.Task.Template, importDigest(obj.Raw)))
		for _, key := range notionSortedKeys(obj.Properties) {
			sourceHashes = append(sourceHashes, key+":"+importDigest(marshal(obj.Properties[key])))
		}
	}
	graphHash := importDigest(marshal(sourceHashes))
	for i := range p.Items {
		item := &p.Items[i]
		if notionGraphPage(*item) {
			item.Hash = importDigest([]byte(item.Hash + ":" + graphHash))
		}
	}
	return nil
}
func notionPropertyValue(obj notionCapturedObject, prop contracts.DomainPropertyDefinition, external, sourceType string, value any, ids, options map[string]string) (any, error) {
	if prop.Type == "unsupported_external" {
		return notionFallback(sourceType, value, string(marshal(value))), nil
	}
	if value == nil {
		return nil, nil
	}
	switch prop.Type {
	case "text":
		return notionRichText(value), nil
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("Number was not supplied as an exact JSON number")
		}
		decimal, err := notionDecimal(string(n))
		var cfg map[string]any
		_ = json.Unmarshal(prop.Config, &cfg)
		return map[string]any{"decimal": decimal, "format": str(cfg["format"])}, err
	case "select", "status":
		id := options[obj.Task.SourceID+":"+external+":"+str(asObject(value)["id"])]
		if id == "" {
			return nil, fmt.Errorf("Option was not supplied in the captured schema")
		}
		return id, nil
	case "multi_select":
		out := []any{}
		for _, v := range asList(value) {
			id := options[obj.Task.SourceID+":"+external+":"+str(asObject(v)["id"])]
			if id == "" {
				return nil, fmt.Errorf("Option was not supplied in the captured schema")
			}
			out = append(out, id)
		}
		return out, nil
	case "date":
		v := asObject(value)
		start := str(v["start"])
		zone := str(v["time_zone"])
		if zone == "" {
			zone = "UTC"
		}
		result := map[string]any{"start": start, "end": v["end"], "timezone": zone, "hasTime": strings.Contains(start, "T")}
		if err := validateDate(result); err != nil {
			return nil, err
		}
		return result, nil
	case "relation":
		out := []any{}
		seen := map[string]bool{}
		for _, v := range asList(value) {
			id := ids["record:"+notionNormalizeID(str(asObject(v)["id"]))]
			if id == "" {
				return nil, fmt.Errorf("Relation includes an uncaptured record; incomplete relations remain evidence")
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out, nil
	case "formula", "rollup":
		if kind := str(asObject(value)["type"]); kind == "incomplete" || kind == "unsupported" {
			return nil, fmt.Errorf("Source calculation is %s; no computed value invented", kind)
		}
		return notionFallback(sourceType, map[string]any{"importedCachedValue": string(marshal(value)), "importedCachedValueEncoding": "source_json"}, string(marshal(value))), nil
	case "files":
		out := []any{}
		for i, v := range asList(value) {
			id := obj.Files[fmt.Sprintf("%s:%d", external, i)]
			if id == "" {
				return nil, fmt.Errorf("File download is incomplete; source reference retained")
			}
			out = append(out, map[string]any{"fileId": id, "displayName": str(asObject(v)["name"])})
		}
		return out, nil
	case "identity":
		out := []any{}
		for _, v := range asList(value) {
			out = append(out, notionIdentity(v))
		}
		return out, nil
	case "author":
		return notionIdentity(value), nil
	case "unique_id":
		v := asObject(value)
		n, ok := v["number"].(json.Number)
		if !ok {
			return nil, fmt.Errorf("Unique ID has no source number")
		}
		prefix := str(v["prefix"])
		if prefix != "" {
			prefix += "-"
		}
		return prefix + string(n), nil
	case "url":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("Invalid URL source value")
		}
		if text != "" {
			u, err := url.Parse(text)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" && u.Scheme != "tel") {
				return nil, fmt.Errorf("Source URL has no supported local scheme")
			}
		}
		return text, nil
	case "email":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("Invalid email source value")
		}
		if text != "" {
			a, err := mail.ParseAddress(text)
			if err != nil || a.Address != text {
				return nil, fmt.Errorf("Source email needs an explicit local mapping")
			}
		}
		return text, nil
	}
	return value, nil
}
func notionIdentity(v any) map[string]any {
	obj := asObject(v)
	label := str(obj["name"])
	if label == "" {
		label = "Notion user " + str(obj["id"])
	}
	return map[string]any{"sourceSystem": "notion_api", "externalKind": "person", "externalId": str(obj["id"]), "displayName": label, "raw": v}
}
func notionDecimal(s string) (string, error) {
	if len(s) > 4096 {
		return "", fmt.Errorf("Number exceeds local precision budget")
	}
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == 'e' })
	if len(parts) == 1 {
		return s, nil
	}
	if len(parts) != 2 {
		return "", fmt.Errorf("Invalid number")
	}
	exponent, err := strconv.Atoi(parts[1])
	if err != nil || exponent > 4096 || exponent < -4096 {
		return "", fmt.Errorf("Number exponent exceeds precision budget")
	}
	mantissa := parts[0]
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign = "-"
		mantissa = mantissa[1:]
	}
	dot := strings.Index(mantissa, ".")
	if dot < 0 {
		dot = len(mantissa)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	position := dot + exponent
	if position <= 0 {
		return normalizeNotionDecimal(sign + "0." + strings.Repeat("0", -position) + digits), nil
	}
	if position >= len(digits) {
		return normalizeNotionDecimal(sign + digits + strings.Repeat("0", position-len(digits))), nil
	}
	return normalizeNotionDecimal(sign + digits[:position] + "." + digits[position:]), nil
}

func notionGraphPage(item ImportItem) bool {
	return item.Kind == "notion_template" || item.Kind == "notion_container" || item.Kind == "portable_collection" || item.Kind == "portable_record"
}
func setNotionGraphConflicts(p *ImportPlan) {
	if p.TypedDatabaseMerge {
		return
	}
	if !p.NotionGraph {
		return
	}
	existing, changed := false, false
	for _, item := range p.Items {
		if notionGraphPage(item) && item.MappingID != "" {
			existing = true
			changed = changed || item.Choice != "keep_local"
		}
	}
	if !existing {
		return
	}
	for i := range p.Items {
		item := &p.Items[i]
		if !notionGraphPage(*item) {
			continue
		}
		if changed {
			item.Choice = ""
			item.Conflict = "Connected Notion database graph exists: keep its local schema/values or import the whole graph as a separate copy"
		} else {
			item.Choice = "keep_local"
		}
	}
}
func prepareNotionGraphChoice(p *ImportPlan) error {
	if p.TypedDatabaseMerge {
		copy := false
		for _, item := range p.Items {
			if notionGraphPage(item) && item.Choice == "duplicate" {
				copy = true
			}
		}
		if copy {
			for _, item := range p.Items {
				if notionGraphPage(item) && item.MappingID != "" && item.Choice != "duplicate" {
					return Fail("CONFLICT", "Copy the connected source graph consistently")
				}
			}
			pages := map[string]bool{}
			for _, collection := range p.PortableCollections {
				pages[collection.PageID] = true
				for _, record := range collection.Records {
					pages[record.PageID] = true
				}
				for _, view := range collection.Views {
					pages[view.OwningPageID] = true
				}
			}
			for i := range p.Items {
				if pages[p.Items[i].PageID] || notionGraphPage(p.Items[i]) {
					p.Items[i].Choice = "duplicate"
				}
			}
			p.NotionCopyGraph = true
			copyImportDatabaseIdentities(p)
		}
		return nil
	}
	if !p.NotionGraph {
		return nil
	}
	choice := ""
	existing := false
	for _, item := range p.Items {
		if !notionGraphPage(item) {
			continue
		}
		existing = existing || item.MappingID != ""
		if item.Choice != "" {
			if item.Choice != "keep_local" && item.Choice != "duplicate" {
				return Fail("CONFLICT", "Notion database schema replacement requires a separate reviewed migration")
			}
			if choice != "" && choice != item.Choice {
				return Fail("CONFLICT", "Choose keep or copy consistently for the connected Notion graph")
			}
			choice = item.Choice
		}
	}
	if !existing {
		return nil
	}
	if choice == "" {
		return Fail("CONFLICT", "Resolve the connected Notion graph before import")
	}
	p.NotionKeepGraph = choice == "keep_local"
	p.NotionCopyGraph = choice == "duplicate"
	for i := range p.Items {
		if notionGraphPage(p.Items[i]) {
			p.Items[i].Choice = choice
			if choice == "keep_local" && p.Items[i].MappingID == "" {
				p.Items[i].PageID = ""
				p.Items[i].Kind = "asset"
				p.Items[i].Outcome = "partial"
				p.Items[i].Reason += "; incoming graph retained while keeping the existing local graph"
			}
		}
	}
	return nil
}
func commitNotionContainers(m *Mutation, p *ImportPlan) error {
	if !p.NotionGraph || p.NotionKeepGraph {
		return nil
	}
	for _, container := range p.NotionContainers {
		if err := guardImport(m, "container", container.ID); err != nil {
			return err
		}
		if err := importGuardPage(m, container.PageID); err != nil {
			return err
		}
		if m.GuardRevision("container", container.ID) == "0" {
			if err := databaseCommand(m, map[string]any{"kind": "container.create", "containerId": container.ID, "owningPageId": container.PageID, "label": container.Title, "containerKind": "database", "parentProjectId": nil}); err != nil {
				return err
			}
		} else {
			// Existing presentation and placement remain local. The field merge
			// does not yet review container membership changes.
			continue
		}
		for _, id := range container.Collections {
			if err := guardImport(m, "collection", id); err != nil {
				return err
			}
			if err := databaseCommand(m, map[string]any{"kind": "container.linkCollection", "containerId": container.ID, "collectionId": id}); err != nil {
				return err
			}
		}
	}
	if !p.NotionCopyGraph {
		for _, identity := range p.NotionIdentities {
			var existing string
			err := m.Tx.QueryRowContext(m.Context, "SELECT local_id FROM import_mappings WHERE workspace_id=? AND source_system='notion_api' AND source_namespace=? AND external_kind=? AND identity_kind='external_id' AND identity_value=?", m.Kernel.id, p.Namespace, identity.Kind, identity.SourceID).Scan(&existing)
			if err == nil {
				if p.TypedDatabaseMerge && existing == identity.ID {
					continue
				}
				return Fail("CONFLICT", "Notion source identity was imported after this preview; capture and review again")
			}
			if err != sql.ErrNoRows {
				return err
			}
			if _, err = m.Exec("INSERT INTO import_mappings VALUES(?,?,?,?,?,'external_id',?,?,?,?,?,?,?)", NewID(), m.Kernel.id, "notion_api", p.Namespace, identity.Kind, identity.SourceID, identity.Kind, identity.ID, identity.Hash, identity.Hash, identity.RawFileID, p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateNotionIdentityPreview(m *Mutation, p *ImportPlan) error {
	if !p.NotionGraph || p.NotionKeepGraph || p.NotionCopyGraph {
		return nil
	}
	for _, identity := range p.NotionIdentities {
		var found int
		if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM import_mappings WHERE workspace_id=? AND source_system='notion_api' AND source_namespace=? AND external_kind=? AND identity_kind='external_id' AND identity_value=?", m.Kernel.id, p.Namespace, identity.Kind, identity.SourceID).Scan(&found); err != nil {
			return err
		}
		if found != 0 {
			var existing string
			if p.TypedDatabaseMerge {
				if err := m.Tx.QueryRowContext(m.Context, "SELECT local_id FROM import_mappings WHERE workspace_id=? AND source_system='notion_api' AND source_namespace=? AND external_kind=? AND identity_kind='external_id' AND identity_value=?", m.Kernel.id, p.Namespace, identity.Kind, identity.SourceID).Scan(&existing); err != nil {
					return err
				}
				if existing == identity.ID {
					continue
				}
			}
			return Fail("CONFLICT", "A Notion source was imported after this preview; capture and review again")
		}
	}
	return nil
}

func normalizeNotionDecimal(value string) string {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign = "-"
		value = value[1:]
	}
	parts := strings.SplitN(value, ".", 2)
	whole := strings.TrimLeft(parts[0], "0")
	if whole == "" {
		whole = "0"
	}
	if len(parts) > 1 {
		return sign + whole + "." + parts[1]
	}
	return sign + whole
}
