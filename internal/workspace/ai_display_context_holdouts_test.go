package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

// Independent, pre-model inputs; original semantic expectations predate the context change.
// This new-only oracle adds requested page-name evidence without changing any
// of the earlier 89 cases or their expectations.
const aiDisplayContextCorpusSHA256 = "2f729da665922dc045d912df4385d5d86a3aa843973a6921592ca4ea4d77920a"
const aiDisplayContextCorpusPath = "testdata/expanded-evaluation/display-context-transfer-holdouts-20261011-v2.json"

// V2 adds only an explicit fixture applicability declaration. Earlier case
// structures and seeding helpers remain unchanged.
type aiDisplayContextCase struct {
	aiTransferCase
	MemoryScopeSetup []aiDisplayMemorySetup `json:"memoryScopeSetup,omitempty"`
}
type aiDisplayMemorySetup struct {
	MemoryIndex int    `json:"memoryIndex"`
	Scope       string `json:"scope"`
	ScopeTarget string `json:"scopeTarget"`
}

func (setup *aiDisplayMemorySetup) UnmarshalJSON(data []byte) error {
	if err := aiDisplayContextUniqueJSON(string(data)); err != nil {
		return err
	}
	var raw struct {
		MemoryIndex *int   `json:"memoryIndex"`
		Scope       string `json:"scope"`
		ScopeTarget string `json:"scopeTarget"`
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if raw.MemoryIndex == nil || raw.Scope == "" || raw.ScopeTarget == "" {
		return fmt.Errorf("incomplete fixture memory scope setup")
	}
	*setup = aiDisplayMemorySetup{*raw.MemoryIndex, raw.Scope, raw.ScopeTarget}
	return nil
}
func aiDisplayContextValidateSetup(tc aiDisplayContextCase) error {
	if tc.ID != "display-mural-decision-under-preference-title" {
		if len(tc.MemoryScopeSetup) != 0 {
			return fmt.Errorf("scope setup on undeclared case")
		}
		return nil
	}
	if len(tc.MemoryScopeSetup) != 1 || tc.MemoryScopeSetup[0] != (aiDisplayMemorySetup{0, "page", "current-page"}) || len(tc.Memories) != 1 || tc.Memories[0].Kind != "decision" || !tc.UseMemory {
		return fmt.Errorf("declared mural decision setup differs")
	}
	return nil
}
func aiDisplayContextMemoryScopes(tc aiDisplayContextCase, workspace, page string) map[string]contracts.DomainScope {
	scopes := map[string]contracts.DomainScope{}
	for i := range tc.Memories {
		scopes[fmt.Sprintf("memory:%d", i)] = contracts.DomainScope{Value: contracts.DomainScopeVariant1{Kind: "workspace", WorkspaceId: contracts.DomainId(workspace)}}
	}
	for _, setup := range tc.MemoryScopeSetup {
		scopes[fmt.Sprintf("memory:%d", setup.MemoryIndex)] = contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(workspace), PageIds: []contracts.DomainId{contracts.DomainId(page)}}}
	}
	return scopes
}
func seedAIDisplayContext(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, tc aiDisplayContextCase) (string, string) {
	t.Helper()
	if err := aiDisplayContextValidateSetup(tc); err != nil {
		t.Fatal(err)
	}
	if len(tc.MemoryScopeSetup) == 0 {
		return seedAITransfer(t, k, e, tc.aiTransferCase)
	}
	// The independently locked v2 exception changes only applicability. The
	// separate source page, decision content, approval and display title remain
	// identical to the original canonical seed contract.
	base := tc.aiTransferCase
	base.Memories, base.MemoryTitles = nil, nil
	page, block := seedAITransfer(t, k, e, base)
	scopes := aiDisplayContextMemoryScopes(tc, k.ID(), page)
	for i, memory := range tc.Memories {
		source := seedExpandedQA(t, k, e, expandedQACase{Title: "Previously reviewed context", Source: memory.Content})
		var revision string
		if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", source).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		memoryPage := createPage(t, k, e, "Approved "+memory.Kind)
		id := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": id, "pageId": memoryPage, "scope": scopes[fmt.Sprintf("memory:%d", i)], "memoryKind": memory.Kind, "content": memory.Content, "sources": []any{map[string]any{"pageId": source, "sourceRevision": revision}}}}, "page", memoryPage, "memory", id))
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, "page", memoryPage, "memory", id))
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": memoryPage, "title": tc.MemoryTitles[i]}}, "page", memoryPage))
	}
	return page, block
}

var aiDisplayContextIDs = []string{
	"display-chess-venue-body-over-title", "display-actual-manuscript-page-name",
	"display-bell-page-name-and-location", "display-dye-summary-no-heading",
	"display-moss-explicit-requested-heading", "display-sourdough-explicit-no-heading",
	"display-seed-direct-preference-question", "display-mural-decision-under-preference-title",
	"display-cello-preference-under-decision-title", "display-same-title-fossil-source-identity",
	"display-bead-subject-attribution", "display-title-only-aperture-not-body-evidence",
}

func parseAIDisplayContextCorpus(data []byte) ([]aiDisplayContextCase, string, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash != aiDisplayContextCorpusSHA256 {
		return nil, hash, fmt.Errorf("locked display-context corpus bytes changed")
	}
	var corpus struct {
		SchemaVersion int    `json:"schemaVersion"`
		Family        string `json:"family"`
		Model         struct {
			Pack               string `json:"pack"`
			ContextTokens      int    `json:"contextTokens"`
			OutputTokens       int    `json:"outputTokens"`
			GenerationAttempts int    `json:"generationAttempts"`
		} `json:"model"`
		Cases []aiDisplayContextCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		return nil, hash, err
	}
	if corpus.SchemaVersion != 1 || corpus.Family != "p0_display_context_transfer_holdouts_20261011" || len(corpus.Cases) != len(aiDisplayContextIDs) || corpus.Model.Pack != "01990000-0000-7000-8000-000000000106" || corpus.Model.ContextTokens != 8192 || corpus.Model.OutputTokens != 512 || corpus.Model.GenerationAttempts != 1 {
		return nil, hash, fmt.Errorf("locked display-context controls or inventory differ")
	}
	for i, tc := range corpus.Cases {
		if err := aiDisplayContextValidateSetup(tc); err != nil {
			return nil, hash, err
		}
		if tc.ID != aiDisplayContextIDs[i] {
			return nil, hash, fmt.Errorf("locked display-context identity/order differs")
		}
	}
	return corpus.Cases, hash, nil
}
func aiDisplayContextCorpus(t *testing.T) ([]aiDisplayContextCase, string) {
	t.Helper()
	data, err := os.ReadFile(aiDisplayContextCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	cases, hash, err := parseAIDisplayContextCorpus(data)
	if err != nil {
		t.Fatal(err)
	}
	return cases, hash
}

// encoding/json normally accepts repeated object keys. Reject them before
// applying this new oracle so an earlier contradictory claim cannot disappear
// during decoding; escaped spellings of the same key are duplicates too.
func aiDisplayContextUniqueJSON(raw string) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	var value func() error
	value = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || keys[key] {
					return fmt.Errorf("duplicate or invalid JSON object key")
				}
				keys[key] = true
				if err := value(); err != nil {
					return err
				}
			}
			close, err := dec.Token()
			if err != nil {
				return err
			}
			if close != json.Delim('}') {
				return fmt.Errorf("invalid JSON object close")
			}
		case '[':
			for dec.More() {
				if err := value(); err != nil {
					return err
				}
			}
			close, err := dec.Token()
			if err != nil {
				return err
			}
			if close != json.Delim(']') {
				return fmt.Errorf("invalid JSON array close")
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("additional or invalid JSON value")
	}
	return nil
}

// Text guards deliberately do not claim complete semantic or format review.
// Support keys are locked corpus facts, never inferred from display titles.
func aiDisplayContextAnswerOracle(tc aiDisplayContextCase, raw string) error {
	if tc.AnswerOracle == nil {
		return nil
	}
	if err := aiDisplayContextUniqueJSON(raw); err != nil {
		return err
	}
	var d expandedDraft
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("additional JSON value")
	}
	o := tc.AnswerOracle
	if d.Title != o.Heading {
		return fmt.Errorf("unexpected output heading")
	}
	if o.Abstain {
		if len(d.Claims) != 1 || d.Title != "" || d.Claims[0].Text != "The supplied evidence does not answer this question." || d.Claims[0].Quote != "" {
			return fmt.Errorf("missing exclusive evidence-absent abstention")
		}
		return nil
	}
	if len(d.Claims) != len(o.Facts) {
		return fmt.Errorf("wrong claim count")
	}
	for i, f := range o.Facts {
		var support string
		switch f.Support {
		case "source":
			support = tc.Source
		case "page-title":
			support = tc.Title
		case "memory:0":
			if !tc.UseMemory || len(tc.Memories) != 1 {
				return fmt.Errorf("missing independently declared memory support")
			}
			support = tc.Memories[0].Content
		default:
			return fmt.Errorf("unknown independently declared support")
		}
		claim := d.Claims[i]
		claimPattern, err := regexp.Compile(f.ClaimPattern)
		if err != nil {
			return err
		}
		quotePattern, err := regexp.Compile(f.QuotePattern)
		if err != nil {
			return err
		}
		if !claimPattern.MatchString(claim.Text) || claim.Quote == "" || !strings.Contains(support, claim.Quote) || !quotePattern.MatchString(claim.Quote) {
			return fmt.Errorf("claim or exact supporting quotation differs from independent expectation")
		}
		if f.Support == "page-title" && claim.Quote != tc.Title {
			return fmt.Errorf("requested page-name support is not the complete exact title")
		}
	}
	return nil
}

// Read expected IDs/revisions from canonical records before preparation. This
// must not derive the expected source identity from the captured AI evidence.
func aiDisplayContextExpectedSources(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, tc aiDisplayContextCase, page, block string) map[string]AIEvidence {
	t.Helper()
	var title, revision string
	if err := k.db.QueryRow("SELECT title,revision FROM pages WHERE id=? AND deleted_at IS NULL", page).Scan(&title, &revision); err != nil || title != tc.Title {
		t.Fatal("canonical current page differs", err)
	}
	loc := contracts.DomainLocation{PageId: contracts.DomainId(page), SourceRevision: contracts.DomainRevision(revision)}
	expected := map[string]AIEvidence{"page-title": {Title: tc.Title, Text: tc.Title, Location: loc}}
	b := contracts.DomainId(block)
	loc.BlockId = &b
	expected["source"] = AIEvidence{Title: tc.Title, Text: tc.Source, Location: loc}
	memories, err := readMemories(context.Background(), k, k.db, e, false)
	if err != nil || len(memories) != len(tc.Memories) {
		t.Fatal("canonical memory inventory differs", err)
	}
	declaredScopes := aiDisplayContextMemoryScopes(tc, k.ID(), page)
	for i, want := range tc.Memories {
		matched := 0
		for _, memory := range memories {
			if memory.Content == nil || *memory.Content != want.Content || memory.Kind != want.Kind {
				continue
			}
			if string(marshal(memory.Scope)) != string(marshal(declaredScopes[fmt.Sprintf("memory:%d", i)])) {
				t.Fatal("canonical memory scope differs from independently declared setup")
			}
			if memory.Status != "active" || len(memory.Sources) != 1 || memory.PageRef != nil {
				t.Fatal("canonical approved memory support differs")
			}
			var memoryTitle string
			if err := k.db.QueryRow("SELECT title FROM pages WHERE id=? AND deleted_at IS NULL", memory.PageId).Scan(&memoryTitle); err != nil || memoryTitle != tc.MemoryTitles[i] {
				t.Fatal("canonical memory display title differs", err)
			}
			expected[fmt.Sprintf("memory:%d", i)] = AIEvidence{Title: "Approved " + memory.Kind + ": " + memoryTitle, Text: want.Content, Location: memory.Sources[0], MemoryID: string(memory.Id), MemoryRevision: string(memory.Revision), MemoryKind: memory.Kind}
			matched++
		}
		if matched != 1 {
			t.Fatal("canonical memory match is not unique")
		}
	}
	return expected
}
func aiDisplayContextSameEvidence(a, b AIEvidence) bool {
	return a.Title == b.Title && a.Text == b.Text && reflect.DeepEqual(a.Location, b.Location) && a.MemoryID == b.MemoryID && a.MemoryRevision == b.MemoryRevision && a.MemoryKind == b.MemoryKind
}
func aiDisplayContextSourceScope(expected map[string]AIEvidence, sources []AIEvidence) (map[string]AIEvidence, error) {
	found := map[string]AIEvidence{}
	numbers := map[int]bool{}
	for _, source := range sources {
		if source.Number <= 0 || numbers[source.Number] || (source.State != "" && source.State != "current") {
			return nil, fmt.Errorf("invalid, duplicated or non-current source identity")
		}
		numbers[source.Number] = true
		role := ""
		for key, want := range expected {
			if aiDisplayContextSameEvidence(source, want) {
				if role != "" {
					return nil, fmt.Errorf("ambiguous canonical support role")
				}
				role = key
			}
		}
		if role == "" {
			return nil, fmt.Errorf("source differs from exact canonical title/body/memory identity")
		}
		if _, duplicate := found[role]; duplicate {
			return nil, fmt.Errorf("duplicated canonical support role")
		}
		found[role] = source
	}
	if len(found) != len(expected) {
		return nil, fmt.Errorf("missing canonical title/body/memory source")
	}
	return found, nil
}

// Bind each bounded claim to the actual captured source number and the saved
// citation's full location. Merely finding the same words elsewhere cannot pass.
func aiDisplayContextCitationOracle(tc aiDisplayContextCase, raw string, expected map[string]AIEvidence, captured []AIEvidence, run AIRun) error {
	if tc.AnswerOracle == nil {
		return nil
	}
	if err := aiDisplayContextAnswerOracle(tc, raw); err != nil {
		return err
	}
	sources, err := aiDisplayContextSourceScope(expected, captured)
	if err != nil {
		return err
	}
	saved, err := aiDisplayContextSourceScope(expected, run.Sources)
	if err != nil {
		return err
	}
	for key, source := range sources {
		if saved[key].Number != source.Number {
			return fmt.Errorf("saved captured source number changed")
		}
	}
	if run.State != "completed" {
		return fmt.Errorf("answer not completed")
	}
	var d expandedDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return err
	}
	if tc.AnswerOracle.Abstain {
		if run.Text != d.Claims[0].Text || len(run.Citations) != 0 {
			return fmt.Errorf("abstention acquired text or attribution")
		}
		return nil
	}
	lines := strings.Split(run.Text, "\n")
	offset := 0
	used := map[int]AIEvidence{}
	if d.Title != "" {
		offset = 1
		if len(lines) == 0 {
			return fmt.Errorf("missing requested heading")
		}
		if lines[0] != d.Title {
			matched := false
			for _, source := range sources {
				if strings.Contains(source.Text, d.Title) && lines[0] == d.Title+fmt.Sprintf(" [%d]", source.Number) {
					used[source.Number] = source
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("heading contains unexpected text or attribution")
			}
		}
	}
	if len(lines) != len(d.Claims)+offset {
		return fmt.Errorf("displayed claim inventory differs")
	}
	for i, fact := range tc.AnswerOracle.Facts {
		source, ok := sources[fact.Support]
		if !ok {
			return fmt.Errorf("required captured source missing")
		}
		if lines[i+offset] != strings.TrimSpace(d.Claims[i].Text)+" ["+strconv.Itoa(source.Number)+"]" {
			return fmt.Errorf("claim citation is not its exact captured support identity")
		}
		used[source.Number] = source
	}
	if len(run.Citations) != len(used) {
		return fmt.Errorf("saved citation inventory differs")
	}
	seen := map[int]bool{}
	for _, citation := range run.Citations {
		source, ok := used[citation.Number]
		if !ok || seen[citation.Number] || citation.Title != source.Title || citation.State != "current" || !reflect.DeepEqual(citation.Location, source.Location) {
			return fmt.Errorf("saved citation differs from captured canonical support")
		}
		seen[citation.Number] = true
	}
	return nil
}

func TestAIDisplayContextLockedCorpusAndOracle(t *testing.T) {
	cases, _ := aiDisplayContextCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			if tc.Source == "" || tc.Title == "" || tc.Prompt == "" || tc.UseMemory != (len(tc.Memories) > 0) || len(tc.MemoryTitles) != len(tc.Memories) || len(tc.Expected.Required) < 2 || len(tc.Expected.Forbidden) < 1 || !tc.Expected.AllClausesMustPass || !tc.Expected.ManualSemanticReviewRequired || tc.Expected.AcceptableExample == tc.Expected.RejectedExample {
				t.Fatal("incomplete independently locked fixture")
			}
			if tc.Selection != "" && !strings.Contains(tc.Source, tc.Selection) {
				t.Fatal("selection outside source")
			}
			if (tc.Mode == "answer") != (tc.AnswerOracle != nil) {
				t.Fatal("answer grammar inventory mismatch")
			}
			if tc.AnswerOracle == nil {
				return
			}
			golden := aiTransferGolden(tc.aiTransferCase)
			if err := aiDisplayContextAnswerOracle(tc, golden); err != nil {
				t.Fatal(err)
			}
			for _, f := range tc.AnswerOracle.Facts {
				if f.Support == "page-title" && aiTransferAnswerOracle(tc.aiTransferCase, golden) == nil {
					t.Fatal("earlier oracle unexpectedly accepts new page-title support")
				}
			}
			for _, poison := range []string{"claim", "quote", "extra", "missing", "heading", "trailing", "unknown", "duplicate-title", "duplicate-escaped-title", "duplicate-text"} {
				var d expandedDraft
				if err := json.Unmarshal([]byte(golden), &d); err != nil {
					t.Fatal(err)
				}
				switch poison {
				case "claim":
					d.Claims[0].Text = "An unsupported factual claim."
				case "quote":
					d.Claims[0].Quote = "Invented quotation."
				case "extra":
					d.Claims = append(d.Claims, d.Claims[0])
				case "missing":
					d.Claims = nil
				case "heading":
					d.Title = "Unrequested heading"
				}
				bytes, _ := json.Marshal(d)
				raw := string(bytes)
				if poison == "trailing" {
					raw += "{}"
				}
				if poison == "unknown" {
					raw = strings.TrimSuffix(raw, "}") + `,"unexpected":true}`
				}
				if poison == "duplicate-title" {
					raw = `{"title":"Contradictory discarded title",` + raw[1:]
				}
				if poison == "duplicate-escaped-title" {
					raw = `{"\u0074itle":"Contradictory discarded title",` + raw[1:]
				}
				if poison == "duplicate-text" {
					raw = strings.Replace(raw, `{"text":`, `{"text":"Contradictory discarded claim","text":`, 1)
				}
				if aiDisplayContextAnswerOracle(tc, raw) == nil {
					t.Fatal("oracle accepted mutation", poison)
				}
			}
		})
	}
	// Metadata validation remains strict independently of the corpus hash.
	mural := cases[7]
	for _, poison := range []string{"missing", "duplicate", "index", "scope", "target", "other-case"} {
		changed := mural
		changed.MemoryScopeSetup = append([]aiDisplayMemorySetup(nil), mural.MemoryScopeSetup...)
		switch poison {
		case "missing":
			changed.MemoryScopeSetup = nil
		case "duplicate":
			changed.MemoryScopeSetup = append(changed.MemoryScopeSetup, changed.MemoryScopeSetup[0])
		case "index":
			changed.MemoryScopeSetup[0].MemoryIndex = 1
		case "scope":
			changed.MemoryScopeSetup[0].Scope = "workspace"
		case "target":
			changed.MemoryScopeSetup[0].ScopeTarget = "memory-page"
		case "other-case":
			changed.ID = cases[0].ID
		}
		if aiDisplayContextValidateSetup(changed) == nil {
			t.Fatal("invalid scope setup accepted", poison)
		}
	}
	for _, raw := range []string{`{"memoryIndex":0,"scope":"page","scopeTarget":"current-page","unknown":true}`, `{"memoryIndex":0,"memoryIndex":0,"scope":"page","scopeTarget":"current-page"}`, `{"scope":"page","scopeTarget":"current-page"}`} {
		var setup aiDisplayMemorySetup
		if json.Unmarshal([]byte(raw), &setup) == nil {
			t.Fatal("ambiguous/incomplete setup decoded", raw)
		}
	}
	data, err := os.ReadFile(aiDisplayContextCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseAIDisplayContextCorpus(append(data, '\n')); err == nil {
		t.Fatal("modified locked bytes accepted")
	}
}

func cloneAIDisplayContextSources(t *testing.T, sources []AIEvidence) []AIEvidence {
	t.Helper()
	data, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	var copy []AIEvidence
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}
func TestAIDisplayContextCanonicalSeedScopeAndCapturedCitations(t *testing.T) {
	cases, _ := aiDisplayContextCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page, block := seedAIDisplayContext(t, k, e, tc)
			expected := aiDisplayContextExpectedSources(t, k, e, tc, page, block)
			audit := beginAICanonicalObservation(t, k)
			request := AIRequest{PageID: page, BlockID: block, Mode: tc.Mode, Prompt: tc.Prompt, Selection: tc.Selection, UseMemory: tc.UseMemory, ModelPackID: NewID()}
			p, err := k.PrepareAIWithBudget(context.Background(), e, request, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = aiDisplayContextSourceScope(expected, p.Run.Sources); err != nil {
				t.Fatal(err)
			}
			for index := range p.Run.Sources {
				for _, poison := range []string{"page", "block", "revision", "title", "text", "number", "memory-id", "memory-kind", "memory-revision", "state"} {
					changed := cloneAIDisplayContextSources(t, p.Run.Sources)
					s := &changed[index]
					switch poison {
					case "page":
						s.Location.PageId = contracts.DomainId(NewID())
					case "block":
						b := contracts.DomainId(NewID())
						s.Location.BlockId = &b
					case "revision":
						s.Location.SourceRevision = "999"
					case "title":
						s.Title = "Wrong metadata"
					case "text":
						s.Text += " Extra"
					case "number":
						s.Number = 0
					case "memory-id":
						s.MemoryID = NewID()
					case "memory-kind":
						s.MemoryKind = "wrong"
					case "memory-revision":
						s.MemoryRevision = "999"
					case "state":
						s.State = "stale"
					}
					if _, err := aiDisplayContextSourceScope(expected, changed); err == nil {
						t.Fatal("scope mutation accepted", index, poison)
					}
				}
			}
			for _, changed := range [][]AIEvidence{p.Run.Sources[:len(p.Run.Sources)-1], append(cloneAIDisplayContextSources(t, p.Run.Sources), p.Run.Sources[0])} {
				if _, err := aiDisplayContextSourceScope(expected, changed); err == nil {
					t.Fatal("missing/duplicate captured source accepted")
				}
			}
			if tc.AnswerOracle == nil {
				run, err := k.FinishAI(context.Background(), e, p.Run.ID, "", "cancelled", "")
				if err != nil || run.State != "cancelled" {
					t.Fatal(err, run.State)
				}
			} else {
				raw := aiTransferGolden(tc.aiTransferCase)
				attempts := 0
				completion, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
					attempts++
					return raw, nil
				}, nil)
				if err != nil || attempts != 1 {
					t.Fatal("golden generation", err, attempts)
				}
				run, err := k.FinishAI(context.Background(), e, p.Run.ID, completion.Text, "completed", "")
				if err != nil {
					t.Fatal(err)
				}
				if err := aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, run); err != nil {
					t.Fatal(err)
				}
				if tc.AnswerOracle.Abstain {
					changed := run
					changed.Text += " Additional claim."
					if aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, changed) == nil {
						t.Fatal("nonexclusive abstention accepted")
					}
				} else {
					for i := range run.Citations {
						for _, poison := range []string{"number", "page", "block", "revision", "title", "state"} {
							changed := run
							changed.Citations = append([]AICitation(nil), run.Citations...)
							c := &changed.Citations[i]
							switch poison {
							case "number":
								c.Number = 999
							case "page":
								c.Location.PageId = contracts.DomainId(NewID())
							case "block":
								b := contracts.DomainId(NewID())
								c.Location.BlockId = &b
							case "revision":
								c.Location.SourceRevision = "999"
							case "title":
								c.Title = "Wrong metadata"
							case "state":
								c.State = "stale"
							}
							if aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, changed) == nil {
								t.Fatal("citation mutation accepted", i, poison)
							}
						}
					}
					changed := run
					changed.Citations = nil
					if aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, changed) == nil {
						t.Fatal("missing citations accepted")
					}
					changed = run
					changed.Text = strings.Replace(run.Text, " [", " [999", 1)
					if aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, changed) == nil {
						t.Fatal("wrong displayed source number accepted")
					}
					changed = run
					changed.Sources = cloneAIDisplayContextSources(t, run.Sources)
					changed.Sources[0].Number = 999
					if aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, changed) == nil {
						t.Fatal("saved source number substitution accepted")
					}
				}
			}
			if !audit.finish(t, k).Unchanged {
				t.Fatal("prepare/publication changed protected canonical content")
			}
		})
	}
}

func TestRealLocalAIDisplayContextHoldouts(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires explicit native integration")
	}
	if os.Getenv("TROVE_AI_CANONICAL_EVIDENCE_DIR") == "" {
		t.Fatal("retained canonical evidence destination required")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack != "01990000-0000-7000-8000-000000000106" {
		t.Fatal("fixed pack106 required")
	}
	cases, hash := aiDisplayContextCorpus(t)
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page, block := seedAIDisplayContext(t, k, e, tc)
			expected := aiDisplayContextExpectedSources(t, k, e, tc, page, block)
			audit := beginAICanonicalObservation(t, k)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			request := AIRequest{Mode: tc.Mode, Prompt: tc.Prompt, PageID: page, BlockID: block, Selection: tc.Selection, ModelPackID: pack, UseMemory: tc.UseMemory}
			budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				texts := make([]string, len(prompts))
				for i, p := range prompts {
					texts[i] = native.ChatPrompt(p.System, p.User)
				}
				return supervisor.CountTokens(ctx, pack, texts)
			}}
			started := time.Now()
			p, prepareErr := k.PrepareAIWithBudget(ctx, e, request, budget)
			raw, system, prompt, grammar, decoded := "", "", "", "", ""
			tokens, attempts := 0, 0
			var generationErr, finishErr, oracleErr, sourceErr, citationErr error
			var run AIRun
			if prepareErr == nil {
				_, sourceErr = aiDisplayContextSourceScope(expected, p.Run.Sources)
				if sourceErr == nil {
					c, err := GeneratePreparedAI(ctx, p, func(ctx context.Context, s, u string, n int, g string, stream func(string)) (string, error) {
						attempts++
						if attempts != 1 {
							return "", fmt.Errorf("single attempt required")
						}
						system, prompt, grammar, tokens = s, u, g, n
						out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(s, u), n, g, &native.RunMetrics{}, stream)
						raw = out
						return out, err
					}, nil)
					decoded, generationErr = c.Text, err
					state, failure := "completed", ""
					if err != nil {
						state, failure = "failed", err.Error()
					}
					run, finishErr = k.FinishAI(context.Background(), e, p.Run.ID, c.Text, state, failure)
					oracleErr = aiDisplayContextAnswerOracle(tc, raw)
					citationErr = aiDisplayContextCitationOracle(tc, raw, expected, p.Run.Sources, run)
				}
			}
			latency := time.Since(started).Milliseconds()
			canonical := audit.finish(t, k)
			executionPassed := prepareErr == nil && sourceErr == nil && generationErr == nil && finishErr == nil && attempts == 1 && tokens == 512 && p.OutputTokens == 512 && system == p.System && prompt == p.Prompt && run.State == "completed" && canonical.Unchanged
			automatedPassed := executionPassed && oracleErr == nil && citationErr == nil
			report, _ := json.Marshal(aiEvaluationRequestReport(request, attempts, map[string]any{"fixture": tc.ID, "mode": request.Mode, "useMemory": request.UseMemory, "corpusSHA256": hash, "request": request, "sourceTitle": tc.Title, "source": tc.Source, "selection": tc.Selection, "prompt": tc.Prompt, "modelPackId": pack, "contextTokens": 8192, "outputCap": p.OutputTokens, "promptTokens": p.PromptTokens, "generatorSystem": system, "generatorPrompt": prompt, "generatorOutputCap": tokens, "generatorGrammarSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(grammar))), "preparedSystem": p.System, "preparedPrompt": p.Prompt, "sources": p.Run.Sources, "savedSources": run.Sources, "runState": run.State, "expectedSourceBindings": expected, "expectedMemoryScopes": aiDisplayContextMemoryScopes(tc, k.ID(), page), "memoryScopeSetup": tc.MemoryScopeSetup, "sourceIdentityOracleApplied": true, "rawDraft": raw, "decodedBody": decoded, "displayed": run.Text, "citations": run.Citations, "generationAttempts": attempts, "prepareError": fmt.Sprint(prepareErr), "sourceScopeError": fmt.Sprint(sourceErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "citationBindingError": fmt.Sprint(citationErr), "citedIdentityOracleApplied": tc.AnswerOracle != nil, "boundedAnswerOracleApplied": tc.AnswerOracle != nil, "latencyMs": latency, "canonicalInventory": canonical, "canonicalUnchanged": canonical.Unchanged, "mutationHistoryCountUnchanged": canonical.MutationHistoryCountUnchanged, "executionPassed": executionPassed, "passed": automatedPassed, "manualSemanticReviewPending": true, "releaseQualified": false}))
			t.Log(string(report))
			if !automatedPassed {
				t.Error("display-context holdout execution, source/citation identity, bounded answer or canonical preservation failed; every case still needs independent manual review")
			}
		})
	}
}
