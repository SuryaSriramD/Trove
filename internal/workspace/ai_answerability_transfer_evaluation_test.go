package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

// Locked independently before any production answer-relevance repair was read
// or any output was observed. This supplements the original transfer corpus.
const answerabilityTransferCorpusSHA256 = "a3a0a837c042955037b2e6449ff332e65cfb3bb934c43f4b45476b087a5c6d2d"

type answerabilityTransferCase struct {
	aiTransferCase
	Domain             string `json:"domain"`
	Variant            string `json:"variant"`
	RequestedEntity    string `json:"requestedEntity"`
	RequestedAttribute string `json:"requestedAttribute"`
	Counterexamples    []struct {
		Kind       string `json:"kind"`
		Claim      string `json:"claim"`
		Quote      string `json:"quote"`
		MustReject bool   `json:"mustReject"`
	} `json:"counterexamples"`
}

func answerabilityTransferFixtures(t *testing.T) ([]answerabilityTransferCase, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/expanded-evaluation/answerability-transfer-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash != answerabilityTransferCorpusSHA256 {
		t.Fatal("locked answerability transfer corpus bytes changed")
	}
	var corpus struct {
		SchemaVersion int                         `json:"schemaVersion"`
		Family        string                      `json:"family"`
		Cases         []answerabilityTransferCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 || corpus.Family != "answerability_transfer_v1" || len(corpus.Cases) != 12 {
		t.Fatal("invalid locked answerability transfer inventory", err)
	}
	return corpus.Cases, hash
}

func answerabilityTransferCorpus(t *testing.T) ([]aiTransferCase, string) {
	t.Helper()
	fixtures, hash := answerabilityTransferFixtures(t)
	cases := make([]aiTransferCase, len(fixtures))
	for i, fixture := range fixtures {
		cases[i] = fixture.aiTransferCase
	}
	return cases, hash
}

func answerabilityTransferSingleClaim(t *testing.T, tc aiTransferCase, claim, quote string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"title": tc.AnswerOracle.Heading, "claims": []map[string]string{{"text": claim, "quote": quote}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAnswerabilityTransferIndependentCorpusAndOracle(t *testing.T) {
	cases, _ := answerabilityTransferFixtures(t)
	domains := map[string]struct{ entity, attribute string }{
		"museum-audio":      {"Kestrel gallery audio guide", "language"},
		"bicycle-clamp":     {"Lark seat clamp", "tightening torque"},
		"bakery-ingredient": {"Marigold loaf", "sesame presence"},
		"greenhouse-medium": {"Willow seedling tray", "growing medium"},
	}
	byDomain := map[string]map[string]answerabilityTransferCase{}
	seen := map[string]bool{}
	poisons := 0
	for _, fixture := range cases {
		t.Run(fixture.ID, func(t *testing.T) {
			tc := fixture.aiTransferCase
			domain, known := domains[fixture.Domain]
			if !known || fixture.RequestedEntity != domain.entity || fixture.RequestedAttribute != domain.attribute || tc.ID != "answerability-"+fixture.Domain+"-"+fixture.Variant || seen[tc.ID] {
				t.Fatal("unknown, duplicated or mismatched independent case identity")
			}
			seen[tc.ID] = true
			if fixture.Variant != "absent" && fixture.Variant != "explicit-negative" && fixture.Variant != "supported-positive" {
				t.Fatal("unknown answerability variant")
			}
			if byDomain[fixture.Domain] == nil {
				byDomain[fixture.Domain] = map[string]answerabilityTransferCase{}
			}
			byDomain[fixture.Domain][fixture.Variant] = fixture
			if tc.Title == "" || tc.Source == "" || tc.Prompt == "" || tc.Mode != "answer" || tc.AnswerOracle == nil || tc.AnswerOracle.Heading != "" || tc.Selection != "" || tc.UseMemory || len(tc.Memories) != 0 || len(tc.MemoryTitles) != 0 || len(tc.OutsidePages) != 0 || len(tc.Expected.Required) < 4 || len(tc.Expected.Forbidden) < 3 || !tc.Expected.ManualSemanticReviewRequired || !tc.Expected.AllClausesMustPass || tc.Expected.AcceptableExample == tc.Expected.RejectedExample {
				t.Fatal("incomplete or expanded independent fixture boundary")
			}
			absent := fixture.Variant == "absent"
			if tc.AnswerOracle.Abstain != absent || (absent && len(tc.AnswerOracle.Facts) != 0) || (!absent && len(tc.AnswerOracle.Facts) != 1) {
				t.Fatal("only absent cases may abstain; supported cases require one fact")
			}
			for _, fact := range tc.AnswerOracle.Facts {
				if fact.Support != "source" || fact.ValidClaim == "" || fact.ValidQuote == "" || !strings.Contains(tc.Source, fact.ValidQuote) || fact.ValidClaim != tc.Expected.AcceptableExample || !strings.HasPrefix(fact.ClaimPattern, "(?i)^") || !strings.HasSuffix(fact.ClaimPattern, "$") {
					t.Fatal("invalid whole-claim or exact-source support declaration")
				}
				if _, err := regexp.Compile(fact.ClaimPattern); err != nil {
					t.Fatal(err)
				}
				if _, err := regexp.Compile(fact.QuotePattern); err != nil {
					t.Fatal(err)
				}
				if fixture.Variant == "supported-positive" && fact.ValidClaim == fact.ValidQuote {
					t.Fatal("positive evidence must exercise independently paraphrased support")
				}
			}
			golden := aiTransferGolden(tc)
			if err := aiTransferAnswerOracle(tc, golden); err != nil {
				t.Fatal("independently valid answer rejected", err)
			}
			kinds := map[string]bool{}
			for _, counterexample := range fixture.Counterexamples {
				if !counterexample.MustReject || counterexample.Kind == "" || counterexample.Claim == "" || kinds[counterexample.Kind] {
					t.Fatal("missing or duplicated locked counterexample")
				}
				kinds[counterexample.Kind] = true
				switch counterexample.Kind {
				case "wrong_entity", "nearby_other_attribute", "unrelated_negative", "wrong_support", "opposite_polarity":
					if counterexample.Quote == "" || !strings.Contains(tc.Source, counterexample.Quote) {
						t.Fatal("counterexample must retain genuine evidence while violating relevance, polarity or support", counterexample.Kind)
					}
				case "over_abstention", "unsupported_positive", "unsupported_explicit_negative":
					if counterexample.Quote != "" {
						t.Fatal("unsupported counterexample unexpectedly has a quotation")
					}
				default:
					t.Fatal("unknown locked counterexample kind")
				}
				if err := aiTransferAnswerOracle(tc, answerabilityTransferSingleClaim(t, tc, counterexample.Claim, counterexample.Quote)); err == nil {
					t.Fatal("oracle accepted locked counterexample", counterexample.Kind)
				}
				poisons++
			}
			requiredKinds := []string{"wrong_entity", "nearby_other_attribute", "unrelated_negative"}
			if absent {
				requiredKinds = append(requiredKinds, "unsupported_positive", "unsupported_explicit_negative")
			} else {
				requiredKinds = append(requiredKinds, "over_abstention", "opposite_polarity", "wrong_support")
			}
			if len(kinds) != len(requiredKinds) {
				t.Fatal("locked counterexample inventory differs")
			}
			for _, kind := range requiredKinds {
				if !kinds[kind] {
					t.Fatal("missing locked counterexample", kind)
				}
			}
			for _, kind := range []string{"invented_claim", "invented_quote", "appended_clause", "extra_claim", "missing_claim", "heading", "unknown_field", "trailing_json"} {
				var draft expandedDraft
				if err := json.Unmarshal([]byte(golden), &draft); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "invented_claim":
					draft.Claims[0].Text = "An unrelated invented answer."
				case "invented_quote":
					draft.Claims[0].Quote = "No source contains this invented supporting quotation."
				case "appended_clause":
					draft.Claims[0].Text += " This also proves an unrelated fact."
				case "extra_claim":
					draft.Claims = append(draft.Claims, draft.Claims[0])
				case "missing_claim":
					draft.Claims = nil
				case "heading":
					draft.Title = tc.Title
				}
				raw, err := json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
				poison := string(raw)
				if kind == "unknown_field" {
					poison = strings.TrimSuffix(poison, "}") + ",\"unexpected\":true}"
				}
				if kind == "trailing_json" {
					poison += "{}"
				}
				if aiTransferAnswerOracle(tc, poison) == nil {
					t.Fatal("oracle accepted structural or unsupported poison", kind)
				}
			}
		})
	}
	if len(seen) != 12 || len(byDomain) != 4 || poisons != 68 {
		t.Fatal("independent inventory or locked poison coverage incomplete", len(seen), len(byDomain), poisons)
	}
	for domain, variants := range byDomain {
		if len(variants) != 3 {
			t.Fatal("missing balanced domain variant", domain)
		}
		absent := variants["absent"]
		for name, fixture := range variants {
			if fixture.Prompt != absent.Prompt || fixture.Title != absent.Title || !strings.HasPrefix(fixture.Source, absent.Source) {
				t.Fatal("question/title or shared distractors changed between variants", domain, name)
			}
			for otherName, other := range variants {
				if name != otherName && aiTransferAnswerOracle(fixture.aiTransferCase, aiTransferGolden(other.aiTransferCase)) == nil {
					t.Fatal("oracle accepts another answerability state's answer", domain, name, otherName)
				}
			}
		}
	}
}

func TestAnswerabilityTransferCanonicalSeedAndScope(t *testing.T) {
	cases, _ := answerabilityTransferCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, execution := testKernel(t)
			page, block := seedAITransfer(t, k, execution, tc)
			// The second real page is outside the explicit selected-page request.
			seedExpandedQA(t, k, execution, expandedQACase{Title: "Outside selected source", Source: "Outside-page-only marker ORIOLE-91 must not enter selected evidence."})
			audit := beginAICanonicalObservation(t, k)
			prepared, err := k.PrepareAIWithBudget(context.Background(), execution, AIRequest{PageID: page, BlockID: block, Mode: tc.Mode, Prompt: tc.Prompt, ModelPackID: NewID()}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			if err := aiTransferSourceScope(tc, page, block, prepared.Run.Sources); err != nil {
				t.Fatal(err)
			}
			primary := -1
			for i, source := range prepared.Run.Sources {
				if source.Location.BlockId != nil && string(*source.Location.BlockId) == block && source.MemoryID == "" {
					primary = i
				}
			}
			if primary < 0 {
				t.Fatal("missing primary source")
			}
			for _, kind := range []string{"foreign_page", "wrong_block", "altered_source", "altered_title", "title_body", "duplicate_primary", "missing_primary", "unexpected_memory"} {
				poison := append([]AIEvidence(nil), prepared.Run.Sources...)
				switch kind {
				case "foreign_page":
					poison[primary].Location.PageId = contracts.DomainId(NewID())
				case "wrong_block":
					id := contracts.DomainId(NewID())
					poison[primary].Location.BlockId = &id
				case "altered_source":
					poison[primary].Text += " Unselected fact."
				case "altered_title":
					poison[primary].Title = "Unselected title"
				case "title_body":
					poison[primary].Location.BlockId = nil
				case "duplicate_primary":
					poison = append(poison, poison[primary])
				case "missing_primary":
					poison = append(poison[:primary], poison[primary+1:]...)
				case "unexpected_memory":
					poison = append(poison, AIEvidence{MemoryID: NewID(), MemoryKind: "fact", Text: tc.Source})
				}
				if aiTransferSourceScope(tc, page, block, poison) == nil {
					t.Fatal("source scope accepted poisoned evidence", kind)
				}
			}
			run, err := k.FinishAI(context.Background(), execution, prepared.Run.ID, "", "cancelled", "")
			if err != nil || run.State != "cancelled" {
				t.Fatal(err, run.State)
			}
			if !audit.finish(t, k).Unchanged {
				t.Fatal("prepare/cancel changed protected canonical content")
			}
		})
	}
}

func TestRealLocalAnswerabilityTransferCases(t *testing.T) {
	runRealLocalAITransferCases(t, answerabilityTransferCorpus)
}
