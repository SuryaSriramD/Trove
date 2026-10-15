package workspace

import (
	"context"
	"strings"
	"testing"
)

func TestProviderGmailManifestRejectsRestrictedMetadata(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	restricted, _, _ := gmailAssetFixture(t, k, e)
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	preview, err := bridge.PreviewContext(ctx, []string{restricted})
	if err == nil {
		t.Fatalf("Gmail-derived title entered external context preview: %s", marshal(preview))
	}
	normal := createPage(t, k, e, "Deliberately shareable note")
	preview, err = bridge.PreviewContext(ctx, []string{normal})
	if err != nil || !strings.Contains(string(marshal(preview)), "Deliberately shareable note") {
		t.Fatal("ordinary explicit context was blocked", preview, err)
	}
}
func TestProviderGmailConnectionRejectsRestrictedSelection(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	restricted, _, _ := gmailAssetFixture(t, k, e)
	normal := createPage(t, k, e, "Ordinary context")
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	if _, err := bridge.ConnectManaged(ctx, "codex", "registered-account", "siwc", "chatgpt_plan", []string{normal, restricted}, []string{"readPage"}); err == nil {
		t.Fatal("managed connection admitted Gmail-derived selection")
	}
	if _, err := bridge.Connect(ctx, "mcp", []string{restricted}, []string{"readPage"}); err == nil {
		t.Fatal("paired connection admitted Gmail-derived selection")
	}
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM provider_connections").Scan(&count); err != nil || count != 0 {
		t.Fatal("denied selection persisted a provider connection", count, err)
	}
}
