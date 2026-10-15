package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"trove.local/phase0/contracts"
)

type AIRequest struct {
	Skill          *SkillRunReference `json:"skill,omitempty"`
	ConversationID string             `json:"conversationId,omitempty"`
	PageID         string             `json:"pageId,omitempty"`
	BlockID        string             `json:"blockId,omitempty"`
	ProjectID      string             `json:"projectId,omitempty"`
	Mode           string             `json:"mode"`
	Prompt         string             `json:"prompt"`
	Selection      string             `json:"selection,omitempty"`
	UseMemory      bool               `json:"useMemory"`
	ModelPackID    string             `json:"modelPackId"`
}
type AIEvidence struct {
	Freshness      *contracts.DomainPageFreshness `json:"freshness,omitempty"`
	Number         int                            `json:"number"`
	Title          string                         `json:"title"`
	Text           string                         `json:"text"`
	Location       contracts.DomainLocation       `json:"location"`
	MemoryID       string                         `json:"memoryId,omitempty"`
	MemoryRevision string                         `json:"memoryRevision,omitempty"`
	MemoryKind     string                         `json:"memoryKind,omitempty"`
	State          string                         `json:"state,omitempty"`
}
type AICitation struct {
	Number   int                      `json:"number"`
	Title    string                   `json:"title"`
	Location contracts.DomainLocation `json:"location"`
	State    string                   `json:"state"`
}
type AIRun struct {
	ID             string       `json:"id"`
	ConversationID string       `json:"conversationId"`
	State          string       `json:"state"`
	Request        AIRequest    `json:"request"`
	Text           string       `json:"text"`
	Citations      []AICitation `json:"citations"`
	Sources        []AIEvidence `json:"sources"`
	Warning        string       `json:"warning,omitempty"`
	Error          string       `json:"error,omitempty"`
	CreatedAt      string       `json:"createdAt"`
}
type AIPrepared struct {
	Run          AIRun  `json:"run"`
	System       string `json:"-"`
	Prompt       string `json:"-"`
	OutputTokens int    `json:"-"`
	PromptTokens int    `json:"-"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ai_runs(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL,actor_id TEXT NOT NULL,state TEXT NOT NULL,request_json TEXT NOT NULL,text TEXT NOT NULL DEFAULT '',sources_json TEXT NOT NULL,citations_json TEXT NOT NULL DEFAULT '[]',warning TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS ai_runs_conversation ON ai_runs(conversation_id,created_at); CREATE TABLE IF NOT EXISTS ai_proposals(id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES ai_runs(id),state TEXT NOT NULL,body_json TEXT NOT NULL,created_at TEXT NOT NULL); UPDATE ai_runs SET state='cancelled',error='Application closed during generation; ask again to resume.' WHERE state='running';`)
			return err
		})
	})
	RegisterQuery("ai.runs", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.AIRuns(ctx, e, str(r["conversationId"]))
	})
	RegisterQuery("ai.run", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.AIRun(ctx, e, str(r["runId"]))
	})
	RegisterQuery("ai.prepareAction", prepareAIAction)
	RegisterQuery("ai.applyAction", applyAIAction)
}

func (k *Kernel) PrepareAI(ctx context.Context, e contracts.ServiceExecutionContext, r AIRequest) (AIPrepared, error) {
	return k.PrepareAIWithBudget(ctx, e, r, AIBudget{ContextTokens: 8192, OutputTokens: 512})
}

func (k *Kernel) PrepareAIWithBudget(ctx context.Context, e contracts.ServiceExecutionContext, r AIRequest, budget AIBudget) (out AIPrepared, resultErr error) {
	started := time.Now()
	defer func() {
		outcome, code := diagnosticOutcome(resultErr)
		k.diagnostics.Record("model", "prepare", outcome, code, time.Since(started), int64(budget.ContextTokens))
	}()
	if err := k.authorize(e); err != nil {
		return out, err
	}
	if r.Skill != nil {
		skill, err := k.ResolveSkillRun(ctx, e, *r.Skill, "local")
		if err != nil {
			return out, err
		}
		r.Prompt, r.Mode = skill.Prompt, skill.Skill.Definition.Mode
		r.PageID, r.BlockID, r.ProjectID, r.ConversationID, r.Selection, r.UseMemory = "", "", "", "", "", false
		if len(r.Skill.SourcePageIDs) == 1 {
			r.PageID = r.Skill.SourcePageIDs[0]
		}
	}
	switch r.Mode {
	case "answer", "rewrite", "summarize", "explain", "shorten", "expand", "outline", "generate":
	default:
		if _, ok := advancedAIInstructions[r.Mode]; !ok {
			return out, Fail("INVALID_REQUEST", "Unknown AI operation")
		}
	}
	if len(r.Prompt) > 8192 || len(r.Selection) > 16384 {
		return out, Fail("RESOURCE_EXHAUSTED", "Use a prompt up to 8 KiB and an exact selection up to 16 KiB; the model context must also fit")
	}
	if strings.TrimSpace(r.Prompt) == "" && r.Mode == "answer" {
		return out, Fail("INVALID_REQUEST", "Enter a question")
	}
	if r.PageID != "" {
		if err := k.authorizePage(ctx, k.db, e, r.PageID); err != nil {
			return out, err
		}
	}
	var prior *AIRun
	if r.ConversationID == "" {
		r.ConversationID = NewID()
	} else {
		runs, err := k.AIRuns(ctx, e, r.ConversationID)
		if err != nil {
			return out, err
		}
		if len(runs) == 0 {
			return out, Fail("NOT_FOUND", "Conversation is unavailable")
		}
		candidate := runs[0]
		if candidate.State == "completed" && candidate.Request.PageID == r.PageID && candidate.Request.ProjectID == r.ProjectID && (r.UseMemory || !candidate.Request.UseMemory) {
			live := true
			for _, s := range candidate.Sources {
				if k.aiEvidenceValid(ctx, e, s) != nil {
					live = false
				}
			}
			if live {
				prior = &candidate
			}
		}
	}
	if r.ProjectID != "" {
		if err := k.authorizeEntity(ctx, k.db, e, "container", r.ProjectID); err != nil {
			return out, err
		}
	}
	retrieval := r
	if prior != nil && r.PageID == "" {
		retrieval.Prompt = r.Prompt + " " + prior.Request.Prompt
	}
	sources, retrievalWarning, err := k.aiSources(ctx, e, retrieval)
	if err != nil {
		return out, err
	}
	if r.Selection != "" {
		matched := false
		for _, s := range sources {
			if (r.BlockID == "" || (s.Location.BlockId != nil && string(*s.Location.BlockId) == r.BlockID)) && strings.Contains(s.Text, r.Selection) {
				matched = true
			}
		}
		if !matched {
			return out, Fail("CONFLICT", "Selected text is no longer present in the current source; refresh and select again")
		}
	}
	if !aiSourceOptional(r.Mode) && len(sources) == 0 {
		return out, Fail("NOT_FOUND", "No current sources match. Open a page or use specific keywords.")
	}
	out, err = prepareAIContext(ctx, r, sources, prior, retrievalWarning, budget)
	if err != nil {
		return AIPrepared{}, err
	}
	run, kept := out.Run, out.Run.Sources
	// Tokenization is asynchronous native work; revalidate current evidence and
	// permissions after it, before persisting a runnable prompt.
	if err := k.authorize(e); err != nil {
		return AIPrepared{}, err
	}
	if err := ctx.Err(); err != nil {
		return AIPrepared{}, err
	}
	for _, source := range kept {
		if err := k.aiEvidenceValid(ctx, e, source); err != nil {
			return AIPrepared{}, err
		}
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if r.Skill != nil {
			preview, err := k.previewSkillTx(ctx, tx, e, *r.Skill, "local")
			if err != nil {
				return err
			}
			if preview.Hash != r.Skill.PreviewHash {
				return Fail("CONFLICT", "Reviewed skill sources changed while preparing the run")
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO ai_runs(id,conversation_id,actor_id,state,request_json,sources_json,warning,created_at) VALUES(?,?,?,'running',?,?,?,?)", run.ID, run.ConversationID, e.Actor.ActorId, string(marshal(r)), string(marshal(kept)), run.Warning, run.CreatedAt)
		return err
	})
	out.Run = run
	return out, err
}
func truncateAI(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xc0) == 0x80 {
		n--
	}
	return s[:n] + "…"
}
func (k *Kernel) aiSources(ctx context.Context, e contracts.ServiceExecutionContext, r AIRequest) ([]AIEvidence, string, error) {
	if r.Skill != nil {
		sources := []AIEvidence{}
		warnings := []string{}
		for _, page := range r.Skill.SourcePageIDs {
			if err := k.authorizePage(ctx, k.db, e, page); err != nil {
				return nil, "", err
			}
			part := r
			part.Skill = nil
			part.PageID = page
			part.UseMemory = false
			current, warning, err := k.aiSources(ctx, e, part)
			if err != nil {
				return nil, "", err
			}
			sources = append(sources, current...)
			if warning != "" {
				warnings = append(warnings, warning)
			}
		}
		return sources, strings.Join(warnings, " "), nil
	}

	if err := k.FlushSearch(ctx); err != nil {
		return nil, "", err
	}
	out := []AIEvidence{}
	warning := ""
	seen := map[string]bool{}
	add := func(title, text string, loc contracts.DomainLocation) {
		var n int
		k.db.QueryRowContext(ctx, "SELECT count(*) FROM memory_entries WHERE page_id=?", loc.PageId).Scan(&n)
		if n > 0 {
			return
		}
		// Distinct indexed excerpts of one block share a navigation location.
		// Deduplicate exact evidence, not the location: otherwise later chunks
		// silently disappear before the measured context can report omissions.
		key := string(marshal(loc)) + ":" + searchDigest(text)
		if !seen[key] && len(strings.TrimSpace(text)) > 0 {
			seen[key] = true
			out = append(out, AIEvidence{Title: title, Text: text, Location: loc})
		}
	}
	if r.PageID != "" {
		if r.BlockID != "" {
			var title, raw, rev, kind string
			if err := k.db.QueryRowContext(ctx, `SELECT p.title,b.data_json,p.revision,b.type FROM blocks b JOIN pages p ON p.id=b.page_id WHERE b.id=? AND b.page_id=? AND p.deleted_at IS NULL`, r.BlockID, r.PageID).Scan(&title, &raw, &rev, &kind); err != nil {
				return nil, "", Fail("NOT_FOUND", "Selected source block is unavailable")
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				return nil, "", err
			}
			text := searchReadable(data)
			if kind == "code" {
				text = str(data["source"])
			}
			block := contracts.DomainId(r.BlockID)
			add(title, text, contracts.DomainLocation{PageId: contracts.DomainId(r.PageID), BlockId: &block, SourceRevision: contracts.DomainRevision(rev)})
		}
		rows, err := k.db.QueryContext(ctx, `SELECT c.title,c.body,c.location_json FROM search_chunks c JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision WHERE c.page_id=? AND p.deleted_at IS NULL ORDER BY CASE WHEN c.block_id=? THEN 0 WHEN c.block_id IS NULL THEN 1 ELSE 2 END,c.rowid LIMIT 101`, r.PageID, r.BlockID)
		if err != nil {
			return nil, "", err
		}
		indexedExcerpts := 0
		for rows.Next() {
			indexedExcerpts++
			if indexedExcerpts > 100 {
				warning = "Page retrieval is limited to 100 indexed excerpts; additional page content was omitted."
				break
			}
			var title, text, raw string
			if err = rows.Scan(&title, &text, &raw); err != nil {
				rows.Close()
				return nil, "", err
			}
			var loc contracts.DomainLocation
			json.Unmarshal([]byte(raw), &loc)
			// The selected block above was loaded in full. Its indexed fragments
			// would duplicate the literal source and cannot replace that full copy.
			if r.BlockID != "" && loc.BlockId != nil && string(*loc.BlockId) == r.BlockID {
				continue
			}
			add(title, text, loc)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", err
		}
	} else {
		query := r.Prompt
		if len(query) > 4000 {
			query = truncateAI(query, 4000)
			warning = "Retrieval used the first 4,000 bytes of the request; the complete request is included in the answer context. "
		}
		request := map[string]any{"query": query, "limit": float64(12), "mode": "keyword"}
		// Use only the model enabled by the workspace owner, never an arbitrary
		// retained index or the chat model. This read does not widen caller scope.
		var model string
		var compatible int
		if err := k.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM search_meta WHERE key='semantic_model'),''), EXISTS(SELECT 1 FROM search_embeddings v JOIN search_chunks c ON c.id=v.chunk_id AND c.content_hash=v.content_hash JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision AND p.deleted_at IS NULL WHERE v.model_pack_id=(SELECT value FROM search_meta WHERE key='semantic_model') AND v.dimension=CAST((SELECT value FROM search_meta WHERE key='semantic_dimension') AS INTEGER))`).Scan(&model, &compatible); err != nil {
			return nil, "", err
		}
		if model != "" && compatible != 0 {
			request["mode"], request["modelPackId"] = "hybrid", model
		} else if model == "" {
			warning += "Semantic retrieval is not enabled. Using keyword sources."
		} else {
			warning += "No compatible semantic index is ready. Using keyword sources."
		}
		if r.ProjectID != "" {
			request["scope"] = map[string]any{"kind": "project", "workspaceId": k.id, "projectId": r.ProjectID}
		}
		result, err := searchQuery(ctx, k, e, request)
		if err != nil {
			return nil, "", err
		}
		searched := result.(SearchResults)
		hits := searched.Hits
		if searched.Warning != "" {
			warning = strings.TrimSpace(warning + " " + searched.Warning)
		}
		// Natural questions rarely share every token with a source. Union single-term
		// results only after strict search found none; live filters remain identical.
		if len(hits) == 0 {
			// Query embeddings already covered the complete question. Do not start
			// another model inference for each token during lexical fallback.
			request["mode"] = "keyword"
			for _, term := range strings.Fields(r.Prompt) {
				if len(term) < 4 {
					continue
				}
				request["query"] = term
				res, err := searchQuery(ctx, k, e, request)
				if err != nil {
					return nil, "", err
				}
				hits = append(hits, res.(SearchResults).Hits...)
				if len(hits) >= 12 {
					break
				}
			}
		}
		for _, h := range hits {
			add(h.Title, h.Excerpt, h.Location)
		}
	}
	if r.UseMemory {
		memories, err := readMemories(ctx, k, k.db, e, false)
		if err != nil {
			return nil, "", err
		}
		var scope contracts.DomainScope
		if r.ProjectID != "" {
			scope.Value = contracts.DomainScopeVariant2{Kind: "project", WorkspaceId: contracts.DomainId(k.id), ProjectId: contracts.DomainId(r.ProjectID)}
		} else if r.PageID != "" {
			scope.Value = contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.id), PageIds: []contracts.DomainId{contracts.DomainId(r.PageID)}}
		} else {
			scope = e.Scope
		}
		prefix := []AIEvidence{}
		for _, m := range memories {
			if !memoryScopeMatches(ctx, k, e, m, scope) {
				continue
			}
			if len(prefix) >= 2 {
				warning = strings.TrimSpace(warning + " Additional approved memories were omitted by the two-entry memory limit.")
				break
			}
			var text, title, rev string
			if err = k.db.QueryRowContext(ctx, "SELECT title,revision FROM pages WHERE id=? AND deleted_at IS NULL", m.PageId).Scan(&title, &rev); err != nil {
				continue
			}
			loc := contracts.DomainLocation{PageId: m.PageId, SourceRevision: contracts.DomainRevision(rev)}
			if m.Content != nil {
				text = *m.Content
				if len(m.Sources) > 0 {
					loc = m.Sources[0]
				}
			} else if m.PageRef != nil {
				if k.authorizePage(ctx, k.db, e, string(*m.PageRef)) != nil {
					continue
				}
				rows, err := k.db.QueryContext(ctx, "SELECT title,body,location_json FROM search_chunks WHERE page_id=? ORDER BY rowid LIMIT 5", *m.PageRef)
				if err != nil {
					return nil, "", err
				}
				for rows.Next() {
					var t, b, l string
					rows.Scan(&t, &b, &l)
					text += b + "\n"
					title = t
					json.Unmarshal([]byte(l), &loc)
				}
				rows.Close()
			}
			prefix = append(prefix, AIEvidence{Title: "Approved " + m.Kind + ": " + title, Text: text, Location: loc, MemoryID: string(m.Id), MemoryRevision: string(m.Revision), MemoryKind: m.Kind})
		}
		out = append(out, prefix...)
	}
	for i := range out {
		if out[i].MemoryID != "" {
			continue
		}
		freshness, err := pageFreshness(ctx, k.db, string(out[i].Location.PageId), time.Now())
		if err != nil {
			return nil, "", err
		}
		out[i].Freshness = &freshness
	}
	return out, warning, nil
}

func (k *Kernel) CheckpointAI(ctx context.Context, e contracts.ServiceExecutionContext, id, text string) error {
	if len(text) > 64000 {
		return Fail("RESOURCE_EXHAUSTED", "Answer exceeds output limit")
	}
	if err := k.authorize(e); err != nil {
		return err
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE ai_runs SET text=? WHERE id=? AND actor_id=? AND state='running'", text, id, e.Actor.ActorId)
		return err
	})
}

var aiCitationPattern = regexp.MustCompile(`\[(\d+)\]`)

func (k *Kernel) FinishAI(ctx context.Context, e contracts.ServiceExecutionContext, id, text, state, failure string) (final AIRun, resultErr error) {
	started := time.Now()
	defer func() {
		outcome, code := diagnosticOutcome(resultErr)
		if resultErr == nil && final.State != "" {
			outcome = final.State
		}
		k.diagnostics.Record("model", "finish", outcome, code, time.Since(started), 0)
	}()
	run, err := k.AIRun(ctx, e, id)
	if err != nil {
		return run, err
	}
	if run.State != "running" {
		return run, nil
	}

	if state != "completed" && state != "cancelled" && state != "failed" {
		return run, Fail("INVALID_REQUEST", "Invalid completion state")
	}
	if len(text) > 64000 {
		text = truncateAI(text, 64000)
	}
	citations := []AICitation{}
	valid := map[int]AIEvidence{}
	seen := map[int]bool{}
	for _, s := range run.Sources {
		if k.aiEvidenceValid(ctx, e, s) == nil {
			valid[s.Number] = s
		}
	}
	invalid := false
	if !aiLiteralOutput(run.Request.Mode) {
		text = aiCitationPattern.ReplaceAllStringFunc(text, func(marker string) string {
			n, _ := strconv.Atoi(strings.Trim(marker, "[]"))
			s, ok := valid[n]
			if !ok {
				invalid = true
				return "[source unavailable]"
			}
			if !seen[n] {
				seen[n] = true
				citations = append(citations, AICitation{Number: n, Title: s.Title, Location: s.Location, State: "current"})
			}
			return marker
		})
	}
	if len(citations) == 0 && !invalid && (run.Request.Mode == "answer" || run.Request.Mode == "summarize" || run.Request.Mode == "explain") {
		quote := strings.TrimRight(strings.TrimSpace(text), ".!?")
		if len(quote) >= 16 {
			for _, s := range run.Sources {
				if _, ok := valid[s.Number]; ok && strings.Contains(strings.ToLower(s.Text), strings.ToLower(quote)) {
					text = strings.TrimSpace(text) + fmt.Sprintf(" [%d]", s.Number)
					citations = append(citations, AICitation{Number: s.Number, Title: s.Title, Location: s.Location, State: "current"})
					break
				}
			}
		}
	}
	warning := run.Warning
	if len(valid) != len(run.Sources) {
		state = "failed"
		failure = "Source evidence or approved memory changed during generation. Ask again using current sources."
		text = ""
		citations = []AICitation{}
	}
	if invalid {
		warning += " Unrecognized citations were removed. Review the evidence before using this answer."
	}
	if state == "completed" && len(citations) == 0 && len(run.Sources) > 0 && !aiLiteralOutput(run.Request.Mode) {
		warning += " The model did not cite its answer. Sources below were provided as context; they do not establish that every generated claim is supported."
	}
	if state == "completed" && strings.TrimSpace(text) == "" {
		state = "failed"
		failure = "The local model returned no text."
	}
	sort.Slice(citations, func(i, j int) bool { return citations[i].Number < citations[j].Number })
	err = k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE ai_runs SET text=?,state=?,citations_json=?,warning=?,error=? WHERE id=? AND actor_id=? AND state='running'", text, state, string(marshal(citations)), warning, failure, id, e.Actor.ActorId)
		return err
	})
	if err != nil {
		return run, err
	}
	return k.AIRun(ctx, e, id)
}
func (k *Kernel) aiEvidenceValid(ctx context.Context, e contracts.ServiceExecutionContext, s AIEvidence) error {
	if err := k.authorizePage(ctx, k.db, e, string(s.Location.PageId)); err != nil {
		return err
	}
	var loc map[string]any
	json.Unmarshal(marshal(s.Location), &loc)
	if err := validateMemoryLocation(ctx, k.db, loc); err != nil {
		return err
	}
	if s.MemoryID != "" {
		var status, revision, kind string
		if err := k.db.QueryRowContext(ctx, "SELECT status,revision,kind FROM memory_entries WHERE id=?", s.MemoryID).Scan(&status, &revision, &kind); err != nil {
			return err
		}
		// Older saved runs have no kind. Their identity/revision checks remain
		// authoritative; never infer a missing kind from a display title.
		if status != "active" || revision != s.MemoryRevision || (s.MemoryKind != "" && kind != s.MemoryKind) {
			return Fail("STALE_CITATION", "Memory is no longer approved and current")
		}
		return memoryEvidenceCurrent(ctx, k.db, s.MemoryID)
	}
	return nil
}
func (k *Kernel) AIRun(ctx context.Context, e contracts.ServiceExecutionContext, id string) (AIRun, error) {
	var run AIRun
	var request, sources, citations, actor string
	if err := k.authorize(e); err != nil {
		return run, err
	}
	if err := k.db.QueryRowContext(ctx, "SELECT id,conversation_id,actor_id,state,request_json,text,sources_json,citations_json,warning,error,created_at FROM ai_runs WHERE id=?", id).Scan(&run.ID, &run.ConversationID, &actor, &run.State, &request, &run.Text, &sources, &citations, &run.Warning, &run.Error, &run.CreatedAt); err != nil {
		return run, err
	}
	if actor != string(e.Actor.ActorId) {
		return AIRun{}, Fail("SCOPE_DENIED", "Conversation belongs to another actor")
	}
	if err := json.Unmarshal([]byte(request), &run.Request); err != nil {
		return run, err
	}
	json.Unmarshal([]byte(sources), &run.Sources)
	json.Unmarshal([]byte(citations), &run.Citations)
	for _, s := range run.Sources {
		if k.authorizePage(ctx, k.db, e, string(s.Location.PageId)) != nil {
			return AIRun{}, Fail("SCOPE_DENIED", "Conversation evidence is outside the current scope")
		}
	}
	states := map[int]string{}
	for i := range run.Sources {
		run.Sources[i].State = "current"
		if run.Sources[i].MemoryID == "" {
			freshness, err := pageFreshness(ctx, k.db, string(run.Sources[i].Location.PageId), time.Now())
			if err != nil {
				return AIRun{}, err
			}
			run.Sources[i].Freshness = &freshness
		}
		if k.aiEvidenceValid(ctx, e, run.Sources[i]) != nil {
			run.Sources[i].State = "stale"
		}
		states[run.Sources[i].Number] = run.Sources[i].State
	}
	for i := range run.Citations {
		run.Citations[i].State = states[run.Citations[i].Number]
		if run.Citations[i].State == "" {
			run.Citations[i].State = "unavailable"
		}
	}
	return run, nil
}
func (k *Kernel) AIRuns(ctx context.Context, e contracts.ServiceExecutionContext, conversation string) ([]AIRun, error) {
	if err := k.authorize(e); err != nil {
		return nil, err
	}
	query := "SELECT id FROM ai_runs WHERE actor_id=?"
	args := []any{e.Actor.ActorId}
	if conversation != "" {
		query += " AND conversation_id=?"
		args = append(args, conversation)
	}
	query += " ORDER BY created_at DESC LIMIT 100"
	rows, err := k.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []AIRun{}
	for _, id := range ids {
		run, err := k.AIRun(ctx, e, id)
		if err == nil {
			out = append(out, run)
		}
	}
	return out, nil
}
