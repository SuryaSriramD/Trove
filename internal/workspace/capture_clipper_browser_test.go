package workspace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"trove.local/phase0/internal/clipper"
)

// Opt-in real MV3 extension qualification. It uses an isolated profile, the real
// authenticated bridge and canonical importer; it never uses the user's tabs.
func TestClipperBrowserQualification(t *testing.T) {
	if os.Getenv("TROVE_CLIPPER_BROWSER") == "" {
		t.Skip("set TROVE_CLIPPER_BROWSER to a Chromium executable supporting unpacked extensions")
	}
	k, e := testKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	parent := createPage(t, k, e, "Browser selected destination")
	allowed := []string{"inbox", parent}
	bridge, pairing, err := clipper.Start(ctx, clipper.Config{ExtensionID: "mmdbnaapeedpfjhafldjcacmanhjmjbb", Destinations: func(ctx context.Context) ([]clipper.Destination, error) {
		return k.ClipperDestinations(ctx, e, allowed)
	}, Preview: func(ctx context.Context, capture clipper.Capture) (clipper.Receipt, error) {
		plan, err := k.PreviewClipperCapture(ctx, e, "mmdbnaapeedpfjhafldjcacmanhjmjbb", capture)
		return clipper.Receipt{PlanID: plan.ID, Title: plan.SourceName, State: "awaiting_owner_review"}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	folder := t.TempDir()
	path := filepath.Join(folder, "pairing.json")
	raw, _ := json.Marshal(map[string]any{"pairing": pairing, "destinationId": parent, "revokeFile": filepath.Join(folder, "revoke")})
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(filepath.Join(folder, "revoke")); err == nil {
					bridge.Close()
					return
				}
			}
		}
	}()
	node := os.Getenv("TROVE_NODE")
	if node == "" {
		node = "node"
	}
	command := exec.CommandContext(ctx, node, "../../frontend/tests/clipper-extension.mjs", path)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	plans, err := k.ClipperReviewPlans(ctx, e)
	if err != nil || len(plans) != 3 {
		t.Fatal("expected article, selection and URL drafts", len(plans), err)
	}
	var pages int
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&pages)
	if pages != 1 {
		t.Fatal("extension committed without owner review", pages)
	}
	for _, plan := range plans {
		if plan.CaptureExtension == "" || plan.CaptureDigest == "" {
			t.Fatal("capture provenance missing")
		}
		plan = completeImportFixture(t, k, e, plan)
		if plan.State != "complete" {
			t.Fatal("reviewed clip did not save")
		}
	}
	workspace := k.path
	k.Close()
	reopened, err := Open(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	plans, err = reopened.ClipperReviewPlans(ctx, reopened.OwnerContext())
	if err != nil || len(plans) != 3 {
		t.Fatal("offline retained captures lost", err)
	}
	for _, plan := range plans {
		if plan.State != "complete" {
			t.Fatal("offline clip not saved")
		}
	}
}
