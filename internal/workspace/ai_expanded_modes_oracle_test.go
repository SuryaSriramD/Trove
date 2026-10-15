package workspace

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// This independent test oracle accepts only a display variation of the same
// decimal token: valid English thousands separators. It never uses float64 or
// normalizes signs, decimal precision, exponents, or changed digits.
var expandedDecimalNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+|[1-9][0-9]{0,2}(?:,[0-9]{3})+)(?:\.[0-9]+)?$`)

func expandedCanonicalNumber(s string) (string, bool) {
	if !expandedDecimalNumber.MatchString(s) {
		return "", false
	}
	return strings.ReplaceAll(s, ",", ""), true
}

func expandedWritingNumbers(s string) ([]string, error) {
	runes := []rune(s)
	numbers := []string{}
	sign := func(r rune) bool { return r == '+' || r == '-' || r == '\u2212' }
	word := func(r rune) bool { return unicode.IsLetter(r) || r == '_' }
	for i := 0; i < len(runes); {
		start := i
		if sign(runes[i]) || runes[i] == '.' {
			if i+1 >= len(runes) || !unicode.IsDigit(runes[i+1]) {
				i++
				continue
			}
			i++
		} else if !unicode.IsDigit(runes[i]) {
			i++
			continue
		}
		for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == ',' || runes[i] == '.') {
			i++
		}
		end := i
		// One terminal comma or full stop may be prose punctuation. Interior
		// punctuation stays in the token, so malformed grouping cannot split
		// into individually allowed numeric fragments.
		if end > start && (runes[end-1] == ',' || runes[end-1] == '.') {
			end--
		}
		literal := string(runes[start:end])
		if start > 0 && (word(runes[start-1]) || sign(runes[start-1]) || runes[start-1] == '.' || runes[start-1] == ',') || i < len(runes) && word(runes[i]) {
			return nil, fmt.Errorf("unsupported numeric token near %q", literal)
		}
		number, ok := expandedCanonicalNumber(literal)
		if !ok {
			return nil, fmt.Errorf("invalid decimal or comma grouping: %q", literal)
		}
		numbers = append(numbers, number)
	}
	return numbers, nil
}

func TestExpandedWritingNumericDisplayOracle(t *testing.T) {
	tc := expandedModeCase{Oracle: "writing", Required: []string{"26500", "GBP", "February"}, AllowedNumbers: []string{"26500"}}
	for _, out := range []string{"Budget: 26500 GBP; launch February.", "Budget: 26,500 GBP; launch February.", "February budget: GBP 26,500.", "February budget: 26,500, in GBP."} {
		if err := expandedModeOracle(tc, out); err != nil {
			t.Errorf("valid grouping/punctuation rejected: %q: %v", out, err)
		}
	}
	for _, amount := range []string{"26,501", "126,500", "2,650", "-26,500", "+26,500", "26,500.0", "26,500.25", "26,50", "2,65,000", "26,,500", "26,500,,", "26.500", "26,500.0.0", "026,500", "26,500e2", "x26500", "−26,500", "２６５００"} {
		if err := expandedModeOracle(tc, "Budget: "+amount+" GBP; launch February."); err == nil {
			t.Errorf("changed/malformed numeric value accepted: %q", amount)
		}
	}
	for _, out := range []string{"Budget: 26,500 GBP; launch February with 17 staff.", "Budget: 26,500 GBP; launch February with 17kg.", "Budget: 26,500 GBP; launch March.", "Budget: GBP; launch February."} {
		if err := expandedModeOracle(tc, out); err == nil {
			t.Errorf("extra number or missing original fact accepted: %q", out)
		}
	}
	fragments := expandedModeCase{Oracle: "writing", Required: []string{"GBP"}, AllowedNumbers: []string{"26", "500"}}
	if expandedModeOracle(fragments, "Budget: 26,,500 GBP.") == nil {
		t.Fatal("malformed grouping accepted as individually allowed fragments")
	}
	for _, literal := range []string{"-1234567.50", "+1234567.50", "1234567.50"} {
		want := strings.Replace(literal, "1234567", "1,234,567", 1)
		decimal := expandedModeCase{Oracle: "writing", Required: []string{literal}, AllowedNumbers: []string{literal}}
		if err := expandedModeOracle(decimal, "Amount: "+want+"."); err != nil {
			t.Fatal("exact sign/fraction grouping rejected", literal, err)
		}
		for _, changed := range []string{strings.Replace(want, ".50", ".5", 1), strings.Replace(want, ".50", ".51", 1), "1,234,568.50"} {
			if expandedModeOracle(decimal, "Amount: "+changed+".") == nil {
				t.Fatal("decimal digits or precision changed", literal, changed)
			}
		}
	}
	large := expandedModeCase{Oracle: "writing", Required: []string{"9007199254740993"}, AllowedNumbers: []string{"9007199254740993"}}
	if err := expandedModeOracle(large, "Amount: 9,007,199,254,740,993."); err != nil {
		t.Fatal("exact large integer rejected", err)
	}
	if expandedModeOracle(large, "Amount: 9,007,199,254,740,992.") == nil {
		t.Fatal("distinct large integers collapsed")
	}
	forbidden := expandedModeCase{Oracle: "writing", Required: []string{"26500"}, Forbidden: []string{"17000"}, AllowedNumbers: []string{"26500", "17000"}}
	if expandedModeOracle(forbidden, "Amounts: 26,500 and 17,000.") == nil {
		t.Fatal("grouped forbidden value accepted")
	}
}

func TestExpandedCodeOracleIgnoresOnlyLayoutAndComments(t *testing.T) {
	tc := expandedModeCase{Oracle: "code", ExpectedCode: "func Scale2(x int) int { return x + x }", RequireComment: true}
	for _, out := range []string{
		"func Scale2(x int) int {\n // Doubles x.\n return x + x\n}",
		"// Doubles x.\nfunc Scale2(x int) int { return x + x }",
		"func Scale2(x int) int { /* Doubles x. */ return x+x }",
		"\n\nfunc Scale2(\n x int,\n) int {\n /* Doubles x. */\n return x + x\n}\n",
	} {
		if err := expandedModeOracle(tc, out); err != nil {
			t.Errorf("layout-only/comment change rejected: %q: %v", out, err)
		}
	}
	for _, out := range []string{
		"func Scale2(x int) int { // Doubles x. return x + x }",
		"func Scale2(x int) int { return x + x }",
		"// Doubles x.\nfunc Scale2(x int) int { return x - x }",
		"// Doubles x.\nfunc Scale2(x int) int { return 2 * x }",
		"// Doubles x.\nfunc Scale2(x int) int { if x < 0 { return 0 }; return x + x }",
		"// Doubles x.\nfunc Scale2(x int64) int64 { return x + x }",
		"// Doubles x.\nfunc Scale2(x, y int) int { return x + x }",
		"// Doubles x.\nfunc Scale2(y int) int { return y + y }",
		"// Doubles x.\nfunc Scale2(x int) int { return x + x }; func Extra() {}",
		"// Doubles x.\nfunc Scale2(x int) int { return x + x }; var Extra = 7",
		"import \"fmt\"\n// Doubles x.\nfunc Scale2(x int) int { return x + x }",
	} {
		if expandedModeOracle(tc, out) == nil {
			t.Errorf("invalid/changed code or missing comment accepted: %q", out)
		}
	}
	for _, pair := range [][2]string{
		{"func Forward(xs []int) { sink(xs...) }", "func Forward(xs []int) { sink(xs) }"},
		{"type Count = int", "type Count int"},
	} {
		tokenPresence := expandedModeCase{Oracle: "code", ExpectedCode: pair[0]}
		if expandedModeOracle(tokenPresence, pair[1]) == nil {
			t.Fatal("semantic token presence was erased as position-only metadata", pair)
		}
	}
	literal := expandedModeCase{Oracle: "code", ExpectedCode: "func Offset(x int) int { return x + 7 }"}
	if expandedModeOracle(literal, "func Offset(x int) int {\nreturn x + 8\n}") == nil {
		t.Fatal("changed literal accepted")
	}
}
