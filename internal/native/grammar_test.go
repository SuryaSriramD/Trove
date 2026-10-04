package native

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestGrammarMetadataIsBoundedAndGenerationOnly(t *testing.T) {
	pack := fixturePack([]byte("fixture pack"))
	input := func(metadata InputMetadata) Input {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "metadata-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		if err = json.NewEncoder(f).Encode(metadata); err != nil {
			t.Fatal(err)
		}
		in, err := OpenInput(f, "text", NewID())
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	valid := InputMetadata{SchemaVersion: 1, Text: "Return a greeting", ContextTokens: 128, Grammar: `root ::= "hello"`}
	if _, err := validateInputMetadata(pack, "generate", []Input{input(valid)}); err != nil {
		t.Fatal(err)
	}
	oversized := valid
	oversized.Grammar = strings.Repeat("x", (64<<10)+1)
	if _, err := validateInputMetadata(pack, "generate", []Input{input(oversized)}); err == nil {
		t.Fatal("unbounded native grammar accepted")
	}
	embedded := valid
	embedded.Text = ""
	embedded.Texts = []string{"query"}
	// Equal nonzero input/locations count reaches the embed-only grammar guard.
	json.Unmarshal([]byte(`[{"pageId":"01990000-0000-7000-8000-000000000001","sourceRevision":"1"}]`), &embedded.Locations)
	if _, err := validateInputMetadata(pack, "embed", []Input{input(embedded)}); err == nil {
		t.Fatal("generation grammar accepted for embedding")
	}
}
