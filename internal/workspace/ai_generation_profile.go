package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"trove.local/phase0/contracts"
)

// Generation metadata identifies the code-owned protocol actually prepared for
// a run. It records no prompt, source text or grammar text. Legacy NULL values
// deliberately stay unlabelled; reading a run never infers a current version.
func initAIGenerationProfiles(ctx context.Context, tx *sql.Tx) error {
	var installed, columns int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM service_runtime_migrations WHERE version=21").Scan(&installed); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('ai_runs') WHERE name='generation_profile_json'").Scan(&columns); err != nil {
		return err
	}
	if installed != 0 {
		if columns != 1 {
			return Fail("INTEGRITY_ERROR", "Generation profile migration is incomplete")
		}
		return nil
	}
	if columns != 0 {
		return Fail("INTEGRITY_ERROR", "Unversioned generation profile storage")
	}
	_, err := tx.ExecContext(ctx, `ALTER TABLE ai_runs ADD COLUMN generation_profile_json TEXT CHECK(generation_profile_json IS NULL OR json_valid(generation_profile_json)); INSERT INTO service_runtime_migrations(version,applied_at) VALUES(21,strftime('%Y-%m-%dT%H:%M:%fZ','now'));`)
	return err
}

type aiGenerationContract struct{ version, grammar string }

func aiGenerationDigest(value string) contracts.DomainHash {
	h := sha256.Sum256([]byte(value))
	return contracts.DomainHash(hex.EncodeToString(h[:]))
}

// Select the existing transport without changing its bytes. A cited grammar
// was already constructed while choosing a context candidate; retain it.
func aiDefaultGenerationContract(p AIPrepared, groundedProse bool) aiGenerationContract {
	switch p.Run.Request.Mode {
	case "workspace_agent":
		return aiGenerationContract{"local-workspace-action-v1", actionPlanGrammar()}
	case "code_refactor", "code_comment":
		return aiGenerationContract{"local-code-source-v1", aiCodeDraftGrammar()}
	case "database", "schema":
		return aiGenerationContract{"local-database-draft-v1", aiDatabaseDraftGrammar()}
	}
	if aiRequiresCitations(p.Run.Request.Mode) && len(p.Run.Sources) > 0 {
		return aiGenerationContract{"local-cited-answer-v1", p.Grammar}
	}
	if groundedProse {
		if p.Run.Request.Mode == "expand" {
			return aiGenerationContract{aiStructuredExpansionVersion, p.Grammar}
		}
		return aiGenerationContract{"local-record-prose-v1", ""}
	}
	return aiGenerationContract{"local-free-text-v1", ""}
}

func bindAIGeneration(p *AIPrepared, contract aiGenerationContract, trustedGuidance string) {
	p.Grammar = contract.grammar
	p.Run.GenerationProfile = &contracts.ServiceAIGenerationProfile{SchemaVersion: 1, ProfileVersion: contract.version, TrustedGuidanceSHA256: aiGenerationDigest(trustedGuidance), SystemSHA256: aiGenerationDigest(p.System), GrammarSHA256: aiGenerationDigest(p.Grammar), OutputTokens: int64(p.OutputTokens)}
	p.generationSeal = aiPreparedSeal(*p)
}

// This private, in-memory seal binds the exact preparation, including private
// inputs, without adding those bytes or their digest to durable metadata/logs.
// It is an integrity guard across trusted Go callers, not an authorization grant.
func aiPreparedSeal(p AIPrepared) [32]byte {
	return sha256.Sum256(marshal(struct {
		ID                         string
		Request                    AIRequest
		Sources                    []AIEvidence
		System, Prompt, Grammar    string
		OutputTokens, PromptTokens int
		Profile                    *contracts.ServiceAIGenerationProfile
	}{p.Run.ID, p.Run.Request, p.Run.Sources, p.System, p.Prompt, p.Grammar, p.OutputTokens, p.PromptTokens, p.Run.GenerationProfile}))
}
func validateAIPrepared(p AIPrepared) error {
	profile := p.Run.GenerationProfile
	if profile == nil || p.generationSeal == ([32]byte{}) || p.generationSeal != aiPreparedSeal(p) || profile.SystemSHA256 != aiGenerationDigest(p.System) || profile.GrammarSHA256 != aiGenerationDigest(p.Grammar) || profile.OutputTokens != int64(p.OutputTokens) {
		return Fail("CONFLICT", "Prepared generation changed; prepare a new run")
	}
	if err := contracts.Validate("service", "AIGenerationProfile", marshal(profile)); err != nil {
		return Fail("INTEGRITY_ERROR", "Invalid generation profile")
	}
	return nil
}
func decodeAIGenerationProfile(raw sql.NullString) (*contracts.ServiceAIGenerationProfile, error) {
	if !raw.Valid {
		return nil, nil
	}
	if err := contracts.Validate("service", "AIGenerationProfile", []byte(raw.String)); err != nil {
		return nil, Fail("INTEGRITY_ERROR", "Stored generation profile is invalid")
	}
	var profile contracts.ServiceAIGenerationProfile
	if err := json.Unmarshal([]byte(raw.String), &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}
