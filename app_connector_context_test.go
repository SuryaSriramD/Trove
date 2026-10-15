package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/workspace"
)

type contextPreviewAdapter struct {
	connectors.Adapter
	identity func(context.Context, connectors.Session) (connectors.Identity, error)
	preview  func(context.Context, connectors.Session, connectors.Selection, string) (connectors.Snapshot, error)
}

func (a contextPreviewAdapter) Identity(ctx context.Context, s connectors.Session) (connectors.Identity, error) {
	return a.identity(ctx, s)
}
func (a contextPreviewAdapter) Preview(ctx context.Context, s connectors.Session, sel connectors.Selection, id string) (connectors.Snapshot, error) {
	return a.preview(ctx, s, sel, id)
}

func TestNativeConnectedContextRequiresExactLiveAccountAndSelection(t *testing.T) {
	for _, scenario := range []string{"valid", "changed-account", "revoked-account", "changed-selection", "changed-resource", "cancel-after-identity", "cancel-after-preview"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sel := connectors.Selection{ID: "org/repo", Kind: "repository", Query: "README.md"}
			identity := connectors.Identity{Provider: "github", Subject: "verified-account"}
			session := connectors.Session{Account: connectors.Account{Identity: identity, Selections: []connectors.Selection{sel}}}
			reads := 0
			adapter := contextPreviewAdapter{
				identity: func(context.Context, connectors.Session) (connectors.Identity, error) {
					if scenario == "revoked-account" {
						return connectors.Identity{}, errors.New("denied")
					}
					if scenario == "changed-account" {
						return connectors.Identity{Provider: "github", Subject: "other"}, nil
					}
					if scenario == "cancel-after-identity" {
						cancel()
					}
					return identity, nil
				},
				preview: func(_ context.Context, _ connectors.Session, s connectors.Selection, id string) (connectors.Snapshot, error) {
					reads++
					if id != "issue:1" || s != sel {
						t.Fatal("expanded context scope")
					}
					if scenario == "cancel-after-preview" {
						cancel()
					}
					if scenario == "changed-resource" {
						id = "issue:2"
					}
					return connectors.Snapshot{RemoteID: id, Text: "Exact selected evidence"}, nil
				},
			}
			requested := sel
			if scenario == "changed-selection" {
				requested.Query = "secret.md"
			}
			result, err := readConnectedPreview(ctx, session, adapter, requested, "issue:1")
			if scenario == "valid" {
				if err != nil || result.Text != "Exact selected evidence" || reads != 1 {
					t.Fatal(result, err, reads)
				}
				return
			}
			if err == nil || result.Text != "" {
				t.Fatal("untrusted source released", result, err)
			}
			if (scenario == "changed-account" || scenario == "revoked-account" || scenario == "changed-selection" || scenario == "cancel-after-identity") && reads != 0 {
				t.Fatal("read occurred before authority validation")
			}
		})
	}
}

func TestNativeConnectedContextRejectsOldWorkspaceBeforeCredentialAccess(t *testing.T) {
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	reader := a.connectorContextReader(k)
	if _, err = reader(context.Background(), connectors.Account{}, connectors.Selection{}, "source"); err == nil {
		t.Fatal("inactive workspace used credentials")
	}
	if _, exists := connectorHosts.Load(a); exists {
		t.Fatal("created credential host for stale workspace")
	}
	a.kernel = k
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = reader(ctx, connectors.Account{}, connectors.Selection{}, "source"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, exists := connectorHosts.Load(a); exists {
		t.Fatal("created credential host after cancellation")
	}
}
