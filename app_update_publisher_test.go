package main

import (
	"context"
	"strings"
	"testing"
	"trove.local/phase0/internal/updates"
)

func TestRollbackPublisherCanonicalTagsAndPlatformIsolation(t *testing.T) {
	current := updates.Application{Identity: updates.Identity{BundleID: updates.BundleID, TeamID: "ABCDEFGHIJ", Build: 4, Architecture: "arm64"}}
	prior := current
	prior.Identity.Build = 2
	prior.Identity.Platform = "darwin"
	prior.Identity.Publisher = updates.Publisher{Kind: updates.AppleDeveloperID, ID: current.Identity.TeamID}
	prior.Identity.TeamID = ""
	if err := compatibleRollbackPublisher(current, updates.Kit{Previous: prior}); err != nil {
		t.Fatalf("legacy publisher changed: %v", err)
	}
	for _, platform := range []string{"linux", "windows"} {
		changed := prior
		changed.Identity.Platform = platform
		changed.Identity.Publisher.ID = strings.Repeat("a", 64)
		changed.Identity.Publisher.Kind = updates.LinuxEd25519
		if platform == "windows" {
			changed.Identity.Publisher.Kind = updates.WindowsAuthenticode
		}
		if compatibleRollbackPublisher(current, updates.Kit{Previous: changed}) == nil {
			t.Fatal("cross-platform rollback publisher accepted")
		}
	}
}
func TestRollbackLaunchLegacyReviewMatchesTaggedReverification(t *testing.T) {
	f := newRollbackFixture(t)
	deps := f.dependencies(t)
	tagged := f.application
	tagged.Identity.Platform = "darwin"
	tagged.Identity.Publisher = updates.Publisher{Kind: updates.AppleDeveloperID, ID: tagged.Identity.TeamID}
	tagged.Identity.TeamID = ""
	deps.verifier = rollbackVerifierFunc(func(context.Context, string) (updates.Application, error) { return tagged, nil })
	launched, quit := false, false
	deps.start = func(_ context.Context, path string, _ []string, target updates.RollbackTarget) error {
		if path != f.source || target.WorkspacePath != f.destination {
			t.Fatal("reviewed target changed")
		}
		launched = true
		return nil
	}
	deps.quit = func() {
		if !launched {
			t.Fatal("quit before reviewed startup")
		}
		quit = true
	}
	if err := f.a.launchApplicationRollback(context.Background(), f.id, deps); err != nil {
		t.Fatal(err)
	}
	if !launched || !quit {
		t.Fatal("canonical publisher did not preserve reviewed handshake path")
	}
}
