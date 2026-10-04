package workspace

import (
	"encoding/json"
	"fmt"
	"trove.local/phase0/contracts"
)

func notionView(source map[string]any, ids map[string]string, properties []contracts.DomainPropertyDefinition, options map[string]string, sourceID string) (portableViewSource, string) {
	query := map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": []any{}, "groupByPropertyId": nil, "subgroupByPropertyId": nil}
	out := portableViewSource{Type: str(source["type"]), SchemaVersion: 1, Query: query, Config: map[string]any{}}
	inert := func(reason string) (portableViewSource, string) {
		out.Type = "unsupported_external"
		out.Config = notionFallback("view", source, reason)
		out.Query = map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": []any{}, "groupByPropertyId": nil, "subgroupByPropertyId": nil}
		return out, reason + "; complete source view retained without broadening its results"
	}
	props := map[string]contracts.DomainPropertyDefinition{}
	for _, p := range properties {
		props[string(p.Id)] = p
	}
	cfg := asObject(source["configuration"])
	visible := []any{}
	widths := []any{}
	if entries, ok := cfg["properties"].([]any); ok {
		for _, v := range entries {
			entry := asObject(v)
			id := ids[str(entry["property_id"])]
			if id == "" {
				return inert("View refers to an unknown property")
			}
			if entry["visible"] != false {
				visible = append(visible, id)
			}
			if width, ok := entry["width"].(json.Number); ok {
				n, err := width.Int64()
				if err == nil && n >= 40 && n <= 2000 {
					widths = append(widths, map[string]any{"propertyId": id, "pixels": n})
				}
			}
		}
	} else {
		for _, p := range properties {
			visible = append(visible, string(p.Id))
		}
	}
	query["visiblePropertyIds"] = visible
	if source["filter"] != nil {
		filter, err := notionFilter(source["filter"], ids, props, 0)
		if err != nil {
			return inert(err.Error())
		}
		query["filter"] = filter
	}
	for _, raw := range asList(source["sorts"]) {
		s := asObject(raw)
		id := ids[str(s["property"])]
		direction := str(s["direction"])
		if id == "" || (direction != "ascending" && direction != "descending") {
			return inert("Unsupported view sort")
		}
		local := "asc"
		if direction == "descending" {
			local = "desc"
		}
		query["sorts"] = append(asList(query["sorts"]), map[string]any{"propertyId": id, "direction": local})
	}
	for sourceKey, targetKey := range map[string]string{"group_by": "groupByPropertyId", "sub_group_by": "subgroupByPropertyId"} {
		if g := asObject(cfg[sourceKey]); g != nil {
			id := ids[str(g["property_id"])]
			if id == "" {
				return inert("View grouping refers to an unknown property")
			}
			switch str(g["type"]) {
			case "select", "multi_select", "checkbox", "relation":
			case "status":
				if g["group_by"] != "option" {
					return inert("Status group aggregation is not equivalent to option grouping")
				}
			case "text", "title", "url", "email", "phone_number":
				if g["group_by"] != "exact" {
					return inert("Non-exact text grouping requires a local mapping")
				}
			default:
				return inert("Unsupported source grouping semantics")
			}
			query[targetKey] = id
		}
	}
	// Quick filters can change result semantics; unknown or dynamic criteria are
	// never silently dropped to create an apparently equivalent unrestricted view.
	if len(asObject(source["quick_filters"])) > 0 {
		return inert("Quick filter behavior needs an explicit local mapping")
	}
	if subtasks := asObject(cfg["subtasks"]); subtasks != nil && subtasks["display_mode"] != "disabled" {
		return inert("Source sub-item filter scope needs an explicit local mapping")
	}
	switch out.Type {
	case "table":
		out.Config = map[string]any{"columnWidths": widths}
	case "list":
		out.Config = map[string]any{"showPageIcon": true}
	case "board":
		g := asObject(cfg["group_by"])
		id := ids[str(g["property_id"])]
		if id == "" || (props[id].Type != "select" && props[id].Type != "status" && props[id].Type != "checkbox") {
			return inert("Board has no supported captured grouping property")
		}
		out.Config = map[string]any{"groupPropertyId": id, "hideEmptyGroups": g["hide_empty_groups"] == true}
	case "calendar", "timeline":
		id := ids[str(cfg["date_property_id"])]
		if id == "" || props[id].Type != "date" {
			return inert("Date view has no supported captured date property")
		}
		if out.Type == "calendar" {
			out.Config = map[string]any{"datePropertyId": id, "weekStartsOn": 0}
		} else {
			if end := str(cfg["end_date_property_id"]); end != "" && ids[end] != id {
				return inert("Timeline uses a separate end-date property requiring a local mapping")
			}
			scale := str(asObject(cfg["preference"])["zoom_level"])
			switch scale {
			case "day", "week", "month", "quarter", "year":
			case "":
				scale = "month"
			default:
				return inert("Timeline zoom has no equivalent local scale")
			}
			out.Config = map[string]any{"datePropertyId": id, "scale": scale}
		}
	case "gallery":
		var cover any
		sourceCover := asObject(cfg["cover"])
		if sourceCover["type"] == "property" {
			id := ids[str(sourceCover["property_id"])]
			if id == "" || props[id].Type != "files" {
				return inert("Gallery cover property is unavailable")
			}
			cover = id
		}
		size := str(cfg["cover_size"])
		if size == "" {
			size = "medium"
		}
		out.Config = map[string]any{"coverPropertyId": cover, "cardSize": size}
	default:
		return inert("Source view type has no supported local rendering")
	}
	if err := contracts.Validate("domain", "view_"+out.Type+"_v1", marshal(out.Config)); err != nil {
		return inert("Source view configuration cannot satisfy the local view contract")
	}
	return out, "Supported view type, property references, filters, sorts and layout reconstructed; source-only cosmetic defaults and settings remain in evidence"
}
func notionFilter(raw any, ids map[string]string, props map[string]contracts.DomainPropertyDefinition, depth int) (any, error) {
	if depth > 8 {
		return nil, fmt.Errorf("Source filter exceeds supported nesting")
	}
	f := asObject(raw)
	for _, op := range []string{"and", "or"} {
		if terms, ok := f[op].([]any); ok {
			if len(terms) > 64 {
				return nil, fmt.Errorf("Source filter exceeds term limit")
			}
			out := []any{}
			for _, v := range terms {
				term, err := notionFilter(v, ids, props, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, term)
			}
			return map[string]any{"op": op, "terms": out}, nil
		}
	}
	id := ids[str(f["property"])]
	prop, ok := props[id]
	if !ok {
		return nil, fmt.Errorf("Source filter property is unavailable")
	}
	sourceType := prop.Type
	if sourceType == "text" {
		sourceType = "rich_text"
	}
	if sourceType == "phone" {
		sourceType = "phone_number"
	}
	condition := asObject(f[sourceType])
	if len(condition) != 1 {
		return nil, fmt.Errorf("Unsupported source filter condition")
	}
	for sourceOp, value := range condition {
		op := map[string]string{"equals": "eq", "does_not_equal": "ne", "greater_than": "gt", "greater_than_or_equal_to": "gte", "less_than": "lt", "less_than_or_equal_to": "lte", "contains": "contains", "is_empty": "is_empty"}[sourceOp]
		if op == "" {
			return nil, fmt.Errorf("Unsupported source filter operator")
		}
		out := map[string]any{"op": op, "propertyId": id}
		if op == "is_empty" {
			if value != true {
				return nil, fmt.Errorf("Negated empty source filter requires local mapping")
			}
			return out, nil
		}
		switch prop.Type {
		case "title", "url", "email", "phone", "checkbox":
		case "text":
			if op != "contains" {
				value = []any{map[string]any{"text": str(value)}}
			}
		case "number":
			n, ok := value.(json.Number)
			if !ok {
				return nil, fmt.Errorf("Numeric source filter has no exact value")
			}
			decimal, err := notionDecimal(string(n))
			if err != nil {
				return nil, err
			}
			var cfg map[string]any
			_ = json.Unmarshal(prop.Config, &cfg)
			value = map[string]any{"decimal": decimal, "format": str(cfg["format"])}
		case "select", "status", "multi_select":
			var cfg map[string]any
			_ = json.Unmarshal(prop.Config, &cfg)
			local := ""
			for _, v := range asList(cfg["options"]) {
				option := asObject(v)
				if option["label"] == value {
					if local != "" {
						return nil, fmt.Errorf("Ambiguous source option label")
					}
					local = str(option["id"])
				}
			}
			if local == "" {
				return nil, fmt.Errorf("Source filter option unavailable")
			}
			value = local
			if prop.Type == "multi_select" && op != "contains" {
				return nil, fmt.Errorf("Unsupported multi-select source filter")
			}
		default:
			return nil, fmt.Errorf("Source filter value requires an explicit local mapping")
		}
		if prop.Type == "title" || op == "contains" && (prop.Type == "text" || prop.Type == "multi_select") {
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("Source filter needs a text value")
			}
		} else if err := contracts.Validate("domain", "property_"+prop.Type+"_v1", marshal(value)); err != nil {
			return nil, fmt.Errorf("Source filter value cannot satisfy the local property contract")
		}
		out["value"] = value
		return out, nil
	}
	return nil, fmt.Errorf("Empty source filter")
}
