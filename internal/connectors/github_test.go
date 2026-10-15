package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func githubFixtureSession(handle func(*http.Request) (string, error)) Session {
	return Session{Account: Account{Kind: GitHub, Identity: Identity{Subject: "7", Provider: "github"}, Selections: []Selection{{ID: "chosen/repo", Kind: "repository", ParentID: "1"}}}, Token: Secret{AccessToken: "fixture-token"}, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := handle(r)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
}

func TestGitHubSnapshotMarksTruncatedContentPartial(t *testing.T) {
	s := githubSnapshot(map[string]any{"title": strings.Repeat("界", 1000), "body": strings.Repeat("界", 100000)}, "issue", "9")
	if s.State != "partial" || !utf8.ValidString(s.Title) || !utf8.ValidString(s.Text) {
		t.Fatal("incomplete or split Unicode content was presented as complete")
	}
}

func TestGitHubDocumentationUsesCanonicalSelectionAndText(t *testing.T) {
	for _, data := range [][]byte{[]byte("valid selected README"), {0xff, 0xfe}, []byte("binary\x00payload")} {
		t.Run(base64.RawURLEncoding.EncodeToString(data), func(t *testing.T) {
			requested := ""
			s := githubFixtureSession(func(r *http.Request) (string, error) {
				if r.URL.Path == "/user/installations/1/repositories" {
					return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
				}
				requested = r.URL.Path
				return stringify(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(data), "sha": "revision", "path": "README.md"}), nil
			})
			g := &githubAdapter{}
			selection := s.Account.Selections[0]
			selection.Query = "unselected.txt"
			if _, err := g.Preview(context.Background(), s, selection, "document:unselected.txt"); err == nil || requested != "" {
				t.Fatal("caller expanded selected documentation")
			}
			selection = s.Account.Selections[0]
			got, err := g.Preview(context.Background(), s, selection, "document:")
			if utf8.Valid(data) && !strings.ContainsRune(string(data), 0) {
				if err != nil || got.Text != string(data) {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("non-text documentation accepted")
			}
		})
	}
}

func TestGitHubRefreshCursorBoundToAccountAndSelection(t *testing.T) {
	s := githubFixtureSession(func(r *http.Request) (string, error) {
		if r.URL.Path == "/user/installations/1/repositories" {
			return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
		}
		return `[]`, nil
	})
	s.Account.ID = "account-one"
	s.Account.Revision = "revision-one"
	g := &githubAdapter{}
	page, err := g.Refresh(context.Background(), s, s.Account.Selections[0], "")
	if err != nil || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	for _, change := range []string{"account", "identity", "revision", "selection"} {
		t.Run(change, func(t *testing.T) {
			other := s
			other.Account.Selections = append([]Selection{}, s.Account.Selections...)
			switch change {
			case "account":
				other.Account.ID = "other"
			case "identity":
				other.Account.Identity.Subject = "other"
			case "revision":
				other.Account.Revision = "other"
			case "selection":
				other.Account.Selections[0].Query = "different.md"
			}
			if _, err := g.Refresh(context.Background(), other, other.Account.Selections[0], page.NextCursor); err == nil {
				t.Fatal("stale cursor accepted")
			}
		})
	}
}

func TestGitHubDiscoveryCursorRejectsAccountSwitch(t *testing.T) {
	s := githubFixtureSession(func(r *http.Request) (string, error) {
		if r.URL.Path == "/user/installations" {
			return `{"installations":[{"id":1},{"id":2}]}`, nil
		}
		return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
	})
	s.Account.ID = "one"
	g := &githubAdapter{}
	_, cursor, err := g.Discover(context.Background(), s, "")
	if err != nil || cursor == "" {
		t.Fatal(cursor, err)
	}
	s.Account.ID = "two"
	if _, _, err = g.Discover(context.Background(), s, cursor); err == nil {
		t.Fatal("cross-account discovery cursor accepted")
	}
}
func TestGitHubReviewedArgumentsRevisionAndUnknownReconciliation(t *testing.T) {
	ctx := context.Background()
	sent := 0
	body := "Original"
	changed := false
	g := NewGitHubAdapter()
	session := githubFixtureSession(func(r *http.Request) (string, error) {
		if r.URL.Path == "/user/installations/1/repositories" {
			return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
		}
		if r.URL.Path == "/repos/chosen/repo/issues/3" && r.Method == "GET" {
			if changed {
				return `{"number":3,"title":"Remote changed","body":"Other text"}`, nil
			}
			return `{"number":3,"title":"Existing","body":"Original"}`, nil
		}
		if r.URL.Path == "/repos/chosen/repo/issues/3" && r.Method == "PATCH" {
			sent++
			var args map[string]any
			_ = json.NewDecoder(r.Body).Decode(&args)
			body = remoteString(args["body"])
			return stringify(map[string]any{"number": 3, "body": body, "html_url": "https://github.com/chosen/repo/issues/3"}), nil
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		return "", nil
	})
	a, err := g.Prepare(ctx, session, Action{Version: 1, Kind: "issue.update", SelectionID: "chosen/repo", RemoteID: "3", Arguments: json.RawMessage(`{"title":"Reviewed title"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(a.Arguments), "Original") || !strings.Contains(string(a.Arguments), "trove-operation:") || len(a.Effects) < 3 || a.ExpectedRevision == "" {
		t.Fatal("preview lost original body, marker, revision or notification effects")
	}
	changed = true
	if err = g.Check(ctx, session, a); err == nil || sent != 0 {
		t.Fatal("stale source passed preflight")
	}
	changed = false
	if err = g.Check(ctx, session, a); err != nil {
		t.Fatal(err)
	}
	receipt, err := g.Send(ctx, session, Proposal{ID: "proposal", Action: a})
	if err != nil || receipt.RemoteID != "3" || sent != 1 {
		t.Fatal(receipt, err)
	}
	if !strings.Contains(body, "Original") || !strings.Contains(body, "trove-operation:") {
		t.Fatal("sent body differs from reviewed body")
	}
}
func TestGitHubRepositoryBoundaryAndExistingBranches(t *testing.T) {
	ctx := context.Background()
	g := NewGitHubAdapter()
	branchCalls := 0
	session := githubFixtureSession(func(r *http.Request) (string, error) {
		switch {
		case r.URL.Path == "/user/installations/1/repositories":
			return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
		case strings.HasPrefix(r.URL.Path, "/repos/chosen/repo/branches/"):
			branchCalls++
			return `{"commit":{"sha":"0123456789012345678901234567890123456789"}}`, nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return "", nil
	})
	if _, err := g.Prepare(ctx, session, Action{Kind: "issue.create", SelectionID: "unselected/repo", Arguments: json.RawMessage(`{"title":"No"}`)}); err == nil {
		t.Fatal("unselected repository allowed")
	}
	a, err := g.Prepare(ctx, session, Action{Version: 1, Kind: "pull_request.create", SelectionID: "chosen/repo", Arguments: json.RawMessage(`{"title":"Reviewed PR","body":"Change","head":"feature/ready","base":"main","draft":true}`)})
	if err != nil || branchCalls != 2 || a.ExpectedRevision == "" {
		t.Fatal("existing branches not bound", err, branchCalls)
	}
	if _, err = g.Prepare(ctx, session, Action{Kind: "pull_request.create", SelectionID: "chosen/repo", Arguments: json.RawMessage(`{"title":"No","head":"outside:branch","base":"main"}`)}); err == nil {
		t.Fatal("unselected cross-repository branch allowed")
	}
	if _, err = g.Prepare(ctx, session, Action{Kind: "issue.create", SelectionID: "chosen/repo", Arguments: json.RawMessage(`{"title":"No","unreviewedField":"x"}`)}); err == nil {
		t.Fatal("unsupported argument accepted")
	}
}
func TestGitHubReconciliationReadsWithoutResending(t *testing.T) {
	ctx := context.Background()
	marker := "<!-- trove-operation:fixture-operation -->"
	writes := 0
	g := NewGitHubAdapter()
	session := githubFixtureSession(func(r *http.Request) (string, error) {
		if r.Method != "GET" {
			writes++
		}
		if r.URL.Path == "/user/installations/1/repositories" {
			return `{"repositories":[{"full_name":"chosen/repo"}]}`, nil
		}
		if r.URL.Path == "/repos/chosen/repo/issues" {
			return stringify([]any{map[string]any{"number": 11, "body": "Reviewed\n\n" + marker, "html_url": "https://github.com/chosen/repo/issues/11"}}), nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return "", nil
	})
	p := Proposal{ID: "proposal", Action: Action{Kind: "issue.create", SelectionID: "chosen/repo", Arguments: json.RawMessage(stringify(map[string]any{"title": "Reviewed", "body": "Reviewed\n\n" + marker}))}}
	r, err := g.Reconcile(ctx, session, p)
	if err != nil || r.Outcome != "succeeded" || r.RemoteID != "11" || writes != 0 {
		t.Fatal(r, err, writes)
	}
}
