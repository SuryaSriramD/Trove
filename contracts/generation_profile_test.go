package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAIGenerationProfileClosedContract(t *testing.T) {
	p := ServiceAIGenerationProfile{SchemaVersion: 1, ProfileVersion: "local-cited-answer-v1", TrustedGuidanceSHA256: DomainHash(strings.Repeat("a", 64)), SystemSHA256: DomainHash(strings.Repeat("b", 64)), GrammarSHA256: DomainHash(strings.Repeat("c", 64)), OutputTokens: 512}
	raw, err := json.Marshal(p)
	if err != nil || Validate("service", "AIGenerationProfile", raw) != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err = json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"schemaVersion": 2, "profileVersion": "unversioned", "trustedGuidanceSHA256": "bad", "systemSHA256": "bad", "grammarSHA256": "bad", "outputTokens": 2049, "prompt": "private"} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy[key] = value
		b, _ := json.Marshal(copy)
		if Validate("service", "AIGenerationProfile", b) == nil {
			t.Fatal("invalid profile accepted", key)
		}
	}
	for key := range base {
		copy := map[string]any{}
		for k, v := range base {
			if k != key {
				copy[k] = v
			}
		}
		b, _ := json.Marshal(copy)
		if Validate("service", "AIGenerationProfile", b) == nil {
			t.Fatal("required profile identity absent", key)
		}
	}
}
