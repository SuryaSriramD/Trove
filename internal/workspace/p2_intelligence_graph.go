package workspace

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

type GraphNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Revision string `json:"revision"`
}
type GraphEdge struct {
	Source   string                   `json:"source"`
	Target   string                   `json:"target"`
	Kind     string                   `json:"kind"`
	Location contracts.DomainLocation `json:"location"`
	Evidence string                   `json:"evidence"`
	Score    float64                  `json:"score,omitempty"`
}
type KnowledgeGraph struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Truncated bool        `json:"truncated"`
	Warning   string      `json:"warning,omitempty"`
}

func init() { RegisterQuery("graph.query", knowledgeGraph) }

// The graph is a projection: explicit edges always retain the originating block
// and revision, and retrieval associations never become asserted canonical links.
func knowledgeGraph(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	limit := int(number(r["limit"]))
	if limit == 0 {
		limit = 60
	}
	if limit < 1 || limit > 100 {
		return nil, Fail("INVALID_REQUEST", "Graph limit must be 1–100")
	}
	focus := str(r["pageId"])
	if focus != "" {
		if err := k.authorizePage(ctx, k.db, e, focus); err != nil {
			return nil, err
		}
	}
	out := KnowledgeGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nodes := map[string]GraphNode{}
	add := func(id string) bool {
		if _, ok := nodes[id]; ok {
			return true
		}
		if k.authorizePage(ctx, tx, e, id) != nil {
			return false
		}
		if len(nodes) >= limit {
			out.Truncated = true
			return false
		}
		var n GraphNode
		if tx.QueryRowContext(ctx, "SELECT id,title,revision FROM pages WHERE id=? AND deleted_at IS NULL", id).Scan(&n.ID, &n.Title, &n.Revision) != nil {
			return false
		}
		nodes[id] = n
		return true
	}
	if focus != "" && !add(focus) {
		return nil, Fail("NOT_FOUND", "Graph page is not available")
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.source_page_id,l.source_block_id,l.target_page_id,p.revision FROM page_links l JOIN pages p ON p.id=l.source_page_id AND p.deleted_at IS NULL JOIN pages t ON t.id=l.target_page_id AND t.deleted_at IS NULL WHERE (?='' OR l.source_page_id=? OR l.target_page_id=?) ORDER BY l.source_page_id,l.source_block_id,l.target_page_id`, focus, focus, focus)
	if err != nil {
		return nil, err
	}
	type link struct{ from, block, to, rev string }
	links := []link{}
	for rows.Next() {
		var l link
		if err = rows.Scan(&l.from, &l.block, &l.to, &l.rev); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, l)
		if len(links) >= 10000 {
			out.Truncated = true
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if k.authorizePage(ctx, tx, e, l.from) != nil || k.authorizePage(ctx, tx, e, l.to) != nil {
			continue
		}
		if add(l.from) && add(l.to) {
			bid := contracts.DomainId(l.block)
			out.Edges = append(out.Edges, GraphEdge{Source: l.from, Target: l.to, Kind: "explicit", Location: contracts.DomainLocation{PageId: contracts.DomainId(l.from), SourceRevision: contracts.DomainRevision(l.rev), BlockId: &bid}, Evidence: "Canonical link in source block"})
		}
		if len(out.Edges) >= 500 {
			out.Truncated = true
			break
		}
	}
	if focus == "" && len(nodes) == 0 {
		rows, err = tx.QueryContext(ctx, "SELECT id FROM pages WHERE deleted_at IS NULL ORDER BY title,id LIMIT 1000")
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			add(id)
		}
	}
	// Release the read snapshot before retrieval flushes its durable projection.
	tx.Rollback()
	if r["includeSemantic"] == true && focus != "" {
		query := strings.TrimSpace(str(r["query"]))
		if query == "" {
			query = nodes[focus].Title
		}
		if live, err := relatedPageCurrent(ctx, k, e, focus, nodes[focus].Revision); err != nil {
			return nil, err
		} else if !live {
			return nil, Fail("CONFLICT", "Graph source changed or became unavailable; refresh the graph")
		}
		hits, searchErr := configuredRelatedSearch(ctx, k, e, query, focus, limit)
		if searchErr != nil {
			return nil, searchErr
		}
		out.Warning = hits.Warning
		for _, h := range hits.Hits {
			id := string(h.Location.PageId)
			if id == focus {
				continue
			}
			if _, ok := nodes[id]; !ok {
				if len(nodes) >= limit {
					out.Truncated = true
					continue
				}
				nodes[id] = GraphNode{ID: id, Title: h.Title, Revision: string(h.Location.SourceRevision)}
			}
			kind := "semantic"
			if hits.Mode == "keyword" {
				kind = "keyword"
			}
			out.Edges = append(out.Edges, GraphEdge{Source: focus, Target: id, Kind: kind, Location: h.Location, Evidence: h.Excerpt, Score: h.Score})
		}
	}
	// Retrieval can outlive the explicit-link snapshot. Never publish stale
	// titles, evidence, forgotten memories or nodes outside the current grant.
	if err := k.authorize(e); err != nil {
		return nil, err
	}
	removed := false
	for id, n := range nodes {
		live, err := relatedPageCurrent(ctx, k, e, id, n.Revision)
		if err != nil {
			return nil, err
		}
		if !live {
			if id == focus {
				return nil, Fail("CONFLICT", "Graph source changed or became unavailable; refresh the graph")
			}
			delete(nodes, id)
			removed = true
			continue
		}
		out.Nodes = append(out.Nodes, n)
	}
	if removed {
		edges := []GraphEdge{}
		for _, edge := range out.Edges {
			if _, ok := nodes[edge.Source]; !ok {
				continue
			}
			if _, ok := nodes[edge.Target]; ok {
				edges = append(edges, edge)
			}
		}
		out.Edges = edges
		out.Truncated = true
		out.Warning = strings.TrimSpace(out.Warning + " Some graph sources changed or became unavailable and were omitted.")
	}
	if err := k.authorize(e); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		return out.Nodes[i].Title < out.Nodes[j].Title || out.Nodes[i].Title == out.Nodes[j].Title && out.Nodes[i].ID < out.Nodes[j].ID
	})
	return out, nil
}
