package connectors

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGoogleLongSnapshotRemainsPartialWithOriginalEvidence(t *testing.T) {
	text := strings.Repeat("界", 100000)
	for _, state := range []string{"current", "removed", "inaccessible"} {
		t.Run(state, func(t *testing.T) {
			snapshot := googleSnapshot(Session{Account: Account{Kind: Gmail}}, Selection{ID: "thread"}, "one", "gmail_thread", strings.Repeat("界", 1000), text, "", "revision", state, map[string]any{"body": text, "attachmentKeys": []string{"gmail-attachment:retained"}})
			expected := state
			if state == "current" {
				expected = "partial"
			}
			if snapshot.State != expected || len(snapshot.Text) > 256<<10 || len(snapshot.Title) > 2000 || !utf8.ValidString(snapshot.Text) || !utf8.ValidString(snapshot.Title) {
				t.Fatal("invalid bounded preview")
			}
			if !strings.Contains(string(snapshot.Content), text) || !strings.Contains(string(snapshot.Content), "gmail-attachment:retained") || snapshot.ExternalAIAllowed {
				t.Fatal("original evidence or origin restriction lost")
			}
		})
	}
}
