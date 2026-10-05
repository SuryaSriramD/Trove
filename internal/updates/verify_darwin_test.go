//go:build darwin

package updates

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMacVerifierRejectsAdhocCandidate(t *testing.T) {
	path := os.Getenv("TROVE_ADHOC_UPDATE_TEST_APP")
	if path == "" {
		t.Skip("requires explicit ad-hoc test candidate")
	}
	_, err := (MacVerifier{}).Inspect(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "Developer ID") {
		t.Fatalf("want Developer ID rejection, got %v", err)
	}
}
