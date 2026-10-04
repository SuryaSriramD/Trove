package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"trove.local/phase0/contracts"
)

// Structured drafts are data only. Neither validation nor approval executes code.
var advancedAIInstructions = map[string]string{
	"database":        `Generate only JSON: {"title":"Name","properties":[{"name":"Name","type":"text","config":{}}],"view":"table"}. Do not include a title property (the service creates it). Supported view: table, list or gallery. Use canonical property configs; number config is {"format":"decimal"}. Maximum 32 properties.`,
	"schema":          `Suggest a schema as JSON {"title":"Name","properties":[{"name":"Name","type":"text","config":{}}],"view":"table"}. It is a reviewable draft, not an applied change.`,
	"mermaid":         `Return only Mermaid source, with no Markdown fence. Use flowchart, sequenceDiagram, stateDiagram-v2, classDiagram, erDiagram, gantt, pie, mindmap or timeline. Do not use links, click actions, HTML or init directives.`,
	"code_explain":    "Explain the selected code and its assumptions. Never execute it. Distinguish observed behavior from suggestions.",
	"code_refactor":   "Return only replacement code, without Markdown fences. Preserve intended behavior. Never execute code. The user will review a source diff.",
	"code_comment":    "Return only replacement code with useful comments added, without Markdown fences. Preserve behavior. Never execute code.",
	"organize":        "Suggest page organization, tags and links using only the approved source context. Each suggestion must explain why and remain unapplied. Never claim a change was made.",
	"workspace_agent": actionPlanInstructions,
}

func aiSourceOptional(mode string) bool {
	return mode == "workspace_agent" || mode == "generate" || mode == "outline" || mode == "database" || mode == "schema" || mode == "mermaid"
}
func aiLiteralOutput(mode string) bool {
	return mode == "workspace_agent" || mode == "database" || mode == "schema" || mode == "mermaid" || mode == "code_refactor" || mode == "code_comment"
}

func decodeAIDraft(body string, out any) error {
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return Fail("INVALID_REQUEST", "Draft must match the documented JSON shape: "+err.Error())
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Fail("INVALID_REQUEST", "Draft must contain one JSON value")
	}
	return nil
}
func safeMermaid(source string) error {
	source = strings.TrimSpace(source)
	if len(source) == 0 || len(source) > 32000 {
		return Fail("INVALID_REQUEST", "Mermaid source must contain 1–32000 bytes")
	}
	lower := strings.ToLower(source)
	for _, bad := range []string{"%%{", "<", ">", "javascript:", "href", "click ", "linkstyle"} {
		// Mermaid arrows use >; prohibit markup starts rather than arrows.
		if bad == ">" {
			continue
		}
		if bad == "<" {
			if strings.Contains(lower, "<script") || strings.Contains(lower, "<iframe") || strings.Contains(lower, "<img") || strings.Contains(lower, "<a ") {
				return Fail("INVALID_REQUEST", "Mermaid markup is not allowed")
			}
			continue
		}
		if strings.Contains(lower, bad) {
			return Fail("INVALID_REQUEST", "Mermaid directives and interactive links are not allowed")
		}
	}
	head := strings.Fields(source)[0]
	for _, kind := range []string{"flowchart", "graph", "sequenceDiagram", "stateDiagram-v2", "classDiagram", "erDiagram", "gantt", "pie", "mindmap", "timeline"} {
		if head == kind {
			return nil
		}
	}
	return Fail("INVALID_REQUEST", "Unsupported Mermaid diagram declaration")
}

func prepareAdvancedAI(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, run AIRun, p *AIProposal, guard func(string, string, string), current func(string, string) error) error {
	guardPage := func() error {
		if p.PageID == "" {
			return Fail("INVALID_REQUEST", "Select a page before reviewing this change")
		}
		if err := k.authorizePage(ctx, k.db, e, p.PageID); err != nil {
			return err
		}
		if err := current("page", p.PageID); err != nil {
			return err
		}
		var record, collection string
		if k.db.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", p.PageID).Scan(&record, &collection) == nil {
			if err := current("record", record); err != nil {
				return err
			}
			return current("collection", collection)
		}
		return nil
	}
	switch p.Kind {
	case "mermaid", "replace_code":
		if err := guardPage(); err != nil {
			return err
		}
		typ := "mermaid"
		data := map[string]any{"source": p.Body, "renderMode": "inert_svg"}
		if p.Kind == "mermaid" {
			if err := safeMermaid(p.Body); err != nil {
				return err
			}
		}
		if p.Kind == "replace_code" || run.Request.BlockID != "" {
			var raw, actual, rev string
			if err := k.db.QueryRowContext(ctx, "SELECT data_json,type,revision FROM blocks WHERE id=? AND page_id=?", run.Request.BlockID, p.PageID).Scan(&raw, &actual, &rev); err != nil {
				return err
			}
			if p.Kind == "replace_code" {
				typ = "code"
			}
			if actual != typ {
				return Fail("INVALID_REQUEST", "Select a matching code or Mermaid block")
			}
			data = map[string]any{}
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				return err
			}
			p.Before = str(data["source"])
			after := p.Body
			if p.Kind == "replace_code" {
				if selected := run.Request.Selection; selected != "" {
					if strings.Count(p.Before, selected) != 1 {
						return Fail("CONFLICT", "Selected code must occur exactly once in the current source block")
					}
					// Replace precisely the reviewed selection. Source outside the
					// selection never depends on a model reproducing it correctly.
					after = strings.Replace(p.Before, selected, p.Body, 1)
				} else {
					complete := strings.TrimSpace(p.Before) == ""
					for _, evidence := range run.Sources {
						if evidence.Location.BlockId != nil && string(*evidence.Location.BlockId) == run.Request.BlockID && strings.Contains(evidence.Text, strings.TrimSpace(p.Before)) {
							complete = true
							break
						}
					}
					if !complete {
						return Fail("INVALID_REQUEST", "The model received only an excerpt of this code block; select the exact code to replace and generate a new draft")
					}
				}
			}
			p.After = after
			data["source"] = after
			guard("block", run.Request.BlockID, rev)
			p.Commands = append(p.Commands, map[string]any{"kind": "block.update", "blockId": run.Request.BlockID, "type": typ, "schemaVersion": 1, "data": data})
		} else {
			id := NewID()
			guard("block", id, "0")
			p.Commands = append(p.Commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": p.PageID, "type": typ, "schemaVersion": 1, "data": data}})
		}
		return validateBlock(typ, 1, data)
	case "create_database":
		var draft struct {
			Title      string `json:"title"`
			Properties []struct {
				Name   string         `json:"name"`
				Type   string         `json:"type"`
				Config map[string]any `json:"config"`
			} `json:"properties"`
			View string `json:"view"`
		}
		if err := decodeAIDraft(p.Body, &draft); err != nil {
			return err
		}
		if p.Title != "" {
			draft.Title = p.Title
		}
		if strings.TrimSpace(draft.Title) == "" || len(draft.Title) > 512 || len(draft.Properties) > 32 {
			return Fail("INVALID_REQUEST", "Database needs a title and at most 32 properties")
		}
		p.Title = draft.Title
		p.PageID = NewID()
		collection, titleProp, view := NewID(), NewID(), NewID()
		for _, v := range [][2]string{{"page", p.PageID}, {"collection", collection}, {"property", titleProp}, {"view", view}} {
			guard(v[0], v[1], "0")
		}
		p.Commands = append(p.Commands, map[string]any{"kind": "page.create", "pageId": p.PageID, "title": p.Title}, map[string]any{"kind": "collection.create", "collectionId": collection, "owningPageId": p.PageID, "titlePropertyId": titleProp, "title": p.Title})
		visible := []string{titleProp}
		names := map[string]bool{"name": true}
		for _, prop := range draft.Properties {
			name := strings.TrimSpace(prop.Name)
			if name == "" || len(name) > 128 || names[strings.ToLower(name)] || prop.Type == "title" {
				return Fail("INVALID_REQUEST", "Property names must be unique; Name is reserved")
			}
			names[strings.ToLower(name)] = true
			if prop.Config == nil {
				prop.Config = map[string]any{}
			}
			if err := contracts.Validate("domain", "property_"+prop.Type+"_config_v1", marshal(prop.Config)); err != nil {
				return Fail("INVALID_REQUEST", "Invalid "+prop.Type+" property config: "+err.Error())
			}
			id := NewID()
			guard("property", id, "0")
			visible = append(visible, id)
			p.Commands = append(p.Commands, map[string]any{"kind": "collection.addProperty", "collectionId": collection, "propertyId": id, "name": name, "type": prop.Type, "schemaVersion": 1, "config": prop.Config})
		}
		config := map[string]any{"columnWidths": []any{}}
		if draft.View == "" {
			draft.View = "table"
		}
		switch draft.View {
		case "table":
		case "list":
			config = map[string]any{"showPageIcon": true}
		case "gallery":
			config = map[string]any{"coverPropertyId": nil, "cardSize": "medium"}
		default:
			return Fail("INVALID_REQUEST", "Generated database view must be table, list or gallery")
		}
		if err := contracts.Validate("domain", "view_"+draft.View+"_v1", marshal(config)); err != nil {
			return err
		}
		p.Commands = append(p.Commands, map[string]any{"kind": "view.save", "viewId": view, "collectionId": collection, "owningPageId": p.PageID, "type": draft.View, "name": "All records", "schemaVersion": 1, "config": config, "query": map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": visible, "groupByPropertyId": nil, "subgroupByPropertyId": nil}})
		p.After = string(marshal(p.Commands))
		return nil
	case "workspace_patch":
		if err := contracts.Validate("service", "ActionPlan", []byte(p.Body)); err != nil {
			return Fail("INVALID_REQUEST", "The plan must contain supported named actions: "+err.Error())
		}
		var draft ActionPlan
		if err := decodeAIDraft(p.Body, &draft); err != nil {
			return err
		}
		resolved, err := k.ResolveActionPlan(ctx, e, draft, p.Guards)
		if err != nil {
			return err
		}
		p.Commands, p.Guards, p.Changes = resolved.Commands, resolved.Guards, resolved.Changes
		if len(resolved.PageIDs) > 0 {
			p.PageID = resolved.PageIDs[0]
		}
		lines := []string{}
		for _, change := range resolved.Changes {
			lines = append(lines, change.Label+" → "+change.Destination+"\n"+change.After)
		}
		p.After = strings.Join(lines, "\n\n")
		return nil
	}
	return Fail("INVALID_REQUEST", "Unknown advanced AI action")
}

func init() { RegisterQuery("ai.suggestions", aiSuggestions) }
func aiSuggestions(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	if err := k.FlushSearch(ctx); err != nil {
		return nil, err
	}
	query := `SELECT p.id,p.title,p.revision,COALESCE(group_concat(c.body,' '),'') FROM pages p LEFT JOIN search_chunks c ON c.page_id=p.id AND c.source_revision=p.revision WHERE p.deleted_at IS NULL GROUP BY p.id ORDER BY p.id`
	if _, unrestricted := e.Scope.Value.(contracts.DomainScopeVariant1); unrestricted {
		query += " LIMIT 1001"
	}
	// For restricted grants, denied candidates must not consume the visible
	// candidate budget. Stream until 1000 authorized pages plus a sentinel.
	rows, err := k.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	type doc struct {
		id, title, revision, body string
		shortened                 bool
	}
	docs := []doc{}
	var source doc
	for rows.Next() {
		var d doc
		if err = rows.Scan(&d.id, &d.title, &d.revision, &d.body); err != nil {
			rows.Close()
			return nil, err
		}
		if k.authorizePage(ctx, k.db, e, d.id) != nil {
			continue
		}
		d.shortened = len(d.body) > 16000
		d.body = truncateAI(d.body, 16000)
		docs = append(docs, d)
		if d.id == page {
			source = d
		}
		if len(docs) > 1000 {
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if source.id == "" {
		err = k.db.QueryRowContext(ctx, `SELECT p.id,p.title,p.revision,COALESCE(group_concat(c.body,' '),'') FROM pages p LEFT JOIN search_chunks c ON c.page_id=p.id AND c.source_revision=p.revision WHERE p.id=? AND p.deleted_at IS NULL GROUP BY p.id`, page).Scan(&source.id, &source.title, &source.revision, &source.body)
		if err != nil {
			return nil, err
		}
		source.shortened = len(source.body) > 16000
		source.body = truncateAI(source.body, 16000)
	}
	tokens := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if len(t) > 2 {
				out[t] = true
			}
		}
		return out
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	a := tokens(source.body)
	suggestions := []map[string]any{}
	partial := len(docs) > 1000
	if partial {
		docs = docs[:1000]
	}
	for _, d := range docs {
		if d.id == page {
			continue
		}
		b := tokens(d.body)
		inter := 0
		for t := range a {
			if b[t] {
				inter++
			}
		}
		score := 0.0
		if len(a)+len(b) > inter {
			score = float64(inter) / float64(len(a)+len(b)-inter)
		}
		kind := "related"
		if !source.shortened && !d.shortened && normalize(source.body) != "" && bytes.Equal([]byte(normalize(source.body)), []byte(normalize(d.body))) {
			kind = "duplicate"
			score = 1
		}
		if score < 0.2 && !strings.Contains(strings.ToLower(source.body), strings.ToLower(d.title)) {
			continue
		}
		suggestions = append(suggestions, map[string]any{"kind": kind, "pageId": d.id, "title": d.title, "sourceRevision": d.revision, "score": score, "reason": fmt.Sprintf("%.0f%% shared vocabulary; review before linking", 100*score)})
	}
	sort.SliceStable(suggestions, func(i, j int) bool { return suggestions[i]["score"].(float64) > suggestions[j]["score"].(float64) })
	if len(suggestions) > 20 {
		suggestions = suggestions[:20]
	}
	result := map[string]any{"suggestions": suggestions, "method": "deterministic vocabulary and exact duplicate comparison", "partial": partial, "applied": false}
	if r["semantic"] == true {
		search, err := searchQuery(ctx, k, e, map[string]any{"query": truncateAI(source.title, 200), "mode": "semantic", "limit": 20})
		if err != nil {
			result["semanticDiagnostic"] = err.Error()
		} else {
			result["semanticBacklinks"] = search
		}
	}
	return result, nil
}
