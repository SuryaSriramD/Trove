package workspace

import (
	"os"
	"testing"
)

func qualityComparisonPack(pack string) bool {
	return pack == "01990000-0000-7000-8000-000000000105" || pack == "01990000-0000-7000-8000-000000000109"
}

func requireQualityComparison(t *testing.T) string {
	t.Helper()
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("explicit native comparison required")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if !qualityComparisonPack(pack) {
		t.Fatal("comparison requires exact reviewed pack105 or pack109")
	}
	return pack
}

func TestRealLocalIndependentModeModelComparison(t *testing.T) {
	runRealLocalIndependentModeControls(t, requireQualityComparison(t))
}

func TestRealLocalReviewTransferModelComparison(t *testing.T) {
	pack := requireQualityComparison(t)
	s, p := workflowNative(t, "chat")
	if p.ID != pack {
		t.Fatal("admitted comparison pack differs")
	}
	runReviewTransfers(t, s, pack)
}

func TestQualityComparisonPackSelection(t *testing.T) {
	for _, pack := range []string{"", "01990000-0000-7000-8000-000000000106", "01990000-0000-7000-8000-000000000107", "arbitrary"} {
		if qualityComparisonPack(pack) {
			t.Fatalf("unreviewed comparison pack admitted: %q", pack)
		}
	}
	for _, pack := range []string{"01990000-0000-7000-8000-000000000105", "01990000-0000-7000-8000-000000000109"} {
		if !qualityComparisonPack(pack) {
			t.Fatalf("reviewed comparison pack rejected: %q", pack)
		}
	}
}
