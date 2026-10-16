package workspace

import "strings"

// Evidence-first v2 changes only the order in which answer-mode generation
// selects a supporting span and composes its claim. It does not prove relevance,
// reconstruct an answer, or grant the model authority over source identities.
const aiAnswerCitationProtocolV2 = "evidence-first-v2"

const aiLegacyClaimFields = `Each claim has string text and quote fields: text is a complete requested factual sentence, and quote is its exact supporting source span.`
const aiEvidenceFirstClaimFields = `For evidence-first-v2, each claim has string quote then text fields: first choose the exact supporting source span, then compose its complete requested factual sentence.`

func aiCitedInstructionsForMode(mode string) string {
	if mode != "answer" {
		return aiCitedInstructions
	}
	instructions := strings.Replace(aiCitedInstructions, aiLegacyClaimFields, aiEvidenceFirstClaimFields, 1)
	// Keep the existing exclusive abstention exact, with fields in v2 order.
	return strings.Replace(instructions, `{"text":"The supplied evidence does not answer this question.","quote":""}`, `{"quote":"","text":"The supplied evidence does not answer this question."}`, 1)
}

func aiCitedGrammarForMode(mode string, sources []AIEvidence) (string, error) {
	grammar, err := aiCitedGrammarForSources(sources)
	if err != nil || mode != "answer" {
		return grammar, err
	}
	const before = `claim ::= "{" ws "\"text\"" ws ":" ws string ws "," ws "\"quote\"" ws ":" ws evidence-quote ws "}"`
	const after = `claim ::= "{" ws "\"quote\"" ws ":" ws evidence-quote ws "," ws "\"text\"" ws ":" ws string ws "}"`
	if strings.Count(grammar, before) != 1 {
		return "", Fail("WORKER_FAILED", "Cited answer protocol declaration is unavailable")
	}
	// All original span choices, including the empty quote, stay unchanged.
	grammar = "# Trove cited-answer protocol: " + aiAnswerCitationProtocolV2 + "\n" + strings.Replace(grammar, before, after, 1)
	if len(grammar) > 64<<10 {
		return "", Fail("RESOURCE_EXHAUSTED", "Exact evidence quote choices exceed 64 KiB; narrow the source context")
	}
	return grammar, nil
}
