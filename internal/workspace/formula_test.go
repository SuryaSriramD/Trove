package workspace

import (
	"strings"
	"testing"
	"time"
)

func TestFormulaIndependentReferenceFixtures(t *testing.T) {
	// Literal expected answers are arithmetic/calendar fixtures, not a second copy
	// of the interpreter. Decimal cases include values beyond JS safe integers.
	at, _ := time.Parse(time.RFC3339, "2026-09-30T20:00:00Z")
	fixtures := []struct{ expression, want string }{{"0.1 + 0.2", "0.3"}, {"9007199254740993 + 0.000001", "9007199254740993.000001"}, {"(12 + 3) * 4 / 2", "30"}, {"ifElse(true, 7, 1 / 0)", "7"}, {"round(-2.5)", "-3"}, {"length(\"नमस्ते\")", "6"}, {"min(4, 2, 8)", "2"}, {"today()", "2026-10-01"}, {"now()", "2026-10-01T01:30:00+05:30"}, {"upper(\"Trove\")", "TROVE"}, {"false && (1 / 0 > 0)", "false"}}
	for _, f := range fixtures {
		t.Run(f.expression, func(t *testing.T) {
			v, e := EvaluateFormula(f.expression, FormulaClock{at, "Asia/Kolkata"}, nil)
			if e != nil || textValue(v) != f.want {
				t.Fatalf("got %v (%v), want %s", v, e, f.want)
			}
		})
	}
	for _, expr := range []string{"os.Exit(0)", "1 / 0", "unknown(1)", "prop(1)", "[1,2]", "strings.ToUpper(\"x\")", strings.Repeat("1+", 600) + "1"} {
		if _, e := EvaluateFormula(expr, FormulaClock{at, "UTC"}, nil); e == nil {
			t.Fatalf("accepted %q", expr)
		}
	}
}
func TestImportedFormulaTranslationRetainsSeparateEvidence(t *testing.T) {
	source := `if(prop("Price") > 10, prop("Price") * 2, 0)`
	translated, err := TranslateImportedFormula(source, map[string]string{"Price": "stable-price-id"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(translated, "ifElse") || !strings.Contains(translated, "stable-price-id") || source != `if(prop("Price") > 10, prop("Price") * 2, 0)` {
		t.Fatal(translated)
	}
	if translated, err := TranslateImportedFormula(`map(prop("Price"), current*2)`, map[string]string{"Price": "stable-price-id"}); err == nil || translated != "" {
		t.Fatal("unsupported expression translated", translated, err)
	}
}
