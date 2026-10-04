package workspace

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"trove.local/phase0/internal/providers/remote"
)

// The real HTTPS listener, OAuth endpoints, official client, canonical SQLite
// kernel, approval service and revocation run together. The custom client dialer
// maps a synthetic public hostname to loopback; no external listener is opened.
func TestRemoteTLSCanonicalScopeAndReviewedWrites(t *testing.T) {
	for _, protocol := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(protocol, func(t *testing.T) {
			k, e := testKernel(t)
			shared := createPage(t, k, e, "Remote shared destination")
			hidden := createPage(t, k, e, "Private sibling")
			bridge := NewProviderBridge(k, e)
			defer bridge.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			pairing, err := bridge.Connect(ctx, "mcp", []string{shared}, []string{"readPage", "proposeActions"})
			if err != nil {
				t.Fatal(err)
			}
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(5), DNSNames: []string{"trove.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			origin := "https://trove.example.test"
			server, err := remote.New(remote.Config{PublicOrigin: origin, BindAddress: address, Certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}}, remote.Callbacks{RecordDecision: bridge.RecordRemoteDecision, Forward: bridge.RemoteMCPHandler(origin, nil), Validate: func(ctx context.Context, id, token string) error {
				_, err := bridge.ManagedTools(id, token)
				return err
			}, Disconnect: bridge.Disconnect})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			if err = server.Start(); err != nil {
				t.Fatal(err)
			}
			connection, err := server.AddConnection(ctx, remote.Binding{ID: pairing.ID, Token: pairing.Token, PageIDs: pairing.PageIDs, Tools: pairing.Tools})
			if err != nil {
				t.Fatal(err)
			}
			leaf, _ := x509.ParseCertificate(der)
			roots := x509.NewCertPool()
			roots.AddCert(leaf)
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
				if target != "trove.example.test:443" {
					return nil, errors.New("external networking forbidden in fixture")
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp", address)
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			auth, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{Client: client, RedirectURL: "https://client.example.test/callback", DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "Synthetic canonical client", RedirectURIs: []string{"https://client.example.test/callback"}, TokenEndpointAuthMethod: "none"}}, AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
				jar, _ := cookiejar.New(nil)
				browser := *client
				browser.Jar = jar
				resp, err := browser.Get(args.URL)
				if err != nil {
					return nil, err
				}
				resp.Body.Close()
				if resp.StatusCode != 303 {
					t.Fatalf("authorize: %d", resp.StatusCode)
				}
				review := origin + resp.Header.Get("Location")
				status := server.Status()
				if len(status.Authorizations) != 1 || status.Connections[0].State != "awaiting_client" || len(status.Connections[0].PageIDs) != 1 || status.Connections[0].PageIDs[0] != shared {
					t.Fatal("unexpected pre-consent scope", status)
				}
				a := status.Authorizations[0]
				if err = server.Decide(ctx, a.ID, a.ReviewHash, true); err != nil {
					return nil, err
				}
				resp, err = browser.Get(review)
				if err != nil {
					return nil, err
				}
				resp.Body.Close()
				destination, _ := url.Parse(resp.Header.Get("Location"))
				q := destination.Query()
				return &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			sdk := mcp.NewClient(&mcp.Implementation{Name: "canonical-remote-test", Version: "1.8.0"}, nil)
			session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: connection.URL, HTTPClient: client, OAuthHandler: auth}, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			var auditCount int
			k.db.QueryRow("SELECT count(*) FROM provider_audit WHERE connection_id=? AND action='remote_authorization' AND state='approved' AND json_extract(detail,'$.clientId') IS NOT NULL", pairing.ID).Scan(&auditCount)
			if auditCount != 1 {
				t.Fatal("native consent missing from canonical audit")
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "readPage", Arguments: map[string]any{"pageId": shared}})
			if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "Remote shared destination") {
				t.Fatal(result, err)
			}
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "readPage", Arguments: map[string]any{"pageId": hidden}})
			if err != nil || !result.IsError {
				t.Fatal("hidden page readable", result, err)
			}
			actions := map[string]any{"idempotencyKey": "remote-approved-page", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Remote shared destination", "title": "Remotely proposed child", "body": "Only committed after native review"}}}}
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "proposeActions", Arguments: actions})
			if err != nil || result.IsError {
				t.Fatal(result, err)
			}
			var proposal struct {
				Result ProviderProposal `json:"result"`
			}
			if json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &proposal) != nil {
				t.Fatal("invalid proposal")
			}
			var count int
			k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Remotely proposed child'").Scan(&count)
			if count != 0 {
				t.Fatal("remote write auto-committed")
			}
			approved, err := bridge.Approve(ctx, proposal.Result.ID)
			if err != nil || !strings.Contains(string(approved), `"ok":true`) {
				t.Fatal(string(approved), err)
			}
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "proposeActions", Arguments: actions})
			if err != nil || result.IsError {
				t.Fatal(result, err)
			}
			k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Remotely proposed child'").Scan(&count)
			if count != 1 {
				t.Fatal("remote retry duplicated content")
			}
			if err = server.Disconnect(ctx, connection.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = session.ListTools(ctx, nil); err == nil {
				t.Fatal("disconnected remote client retained tools")
			}
			if err = server.Close(); err != nil {
				t.Fatal(err)
			}
			local, err := bridge.Connect(ctx, "mcp", []string{hidden}, []string{"readPage"})
			if err != nil {
				t.Fatal(err)
			}
			localResult, err := bridge.Call(ctx, local.ID, local.Token, "readPage", map[string]any{"pageId": hidden})
			if err != nil || !strings.Contains(string(localResult), "Private sibling") {
				t.Fatal("disabling remote broke local bridge", string(localResult), err)
			}
		})
	}
}
