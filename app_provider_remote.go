package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/providers/remote"
	"trove.local/phase0/internal/workspace"
)

type remoteCertificate struct {
	certificate tls.Certificate
	expires     time.Time
}
type providerRemoteState struct {
	mu           sync.Mutex
	server       *remote.Server
	certificates map[string]remoteCertificate
}

var providerRemoteStates sync.Map

// Called under the shell's provider lifecycle exclusion boundary. The bridge is
// shared with local clients and managed runs and owns no second kernel.
func (a *App) getProviderBridge() (*workspace.ProviderBridge, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return nil, errors.New("No workspace open")
	}
	v, _ := providerHosts.LoadOrStore(a, &providerHost{bridge: workspace.NewProviderBridge(a.kernel, a.execution)})
	return v.(*providerHost).bridge, nil
}
func (a *App) stopProviderRemote() {
	if v, ok := providerRemoteStates.LoadAndDelete(a); ok {
		state := v.(*providerRemoteState)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.server != nil {
			_ = state.server.Close()
			state.server = nil
		}
		state.certificates = nil
	}
}
func remoteReadPEM(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Unable to open selected certificate file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("Select a regular PEM file smaller than 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("Unable to read selected certificate file")
	}
	return data, nil
}
func (a *App) providerRemoteDispatch(method string, raw json.RawMessage) string {
	names := map[string]string{"providers.remote.status": "RemoteStatusRequest", "providers.remote.selectCertificate": "RemoteStatusRequest", "providers.remote.enable": "RemoteEnableRequest", "providers.remote.disable": "RemoteStatusRequest", "providers.remote.createConnection": "RemoteConnectionRequest", "providers.remote.disconnect": "RemoteDisconnectRequest", "providers.remote.approveAuthorization": "RemoteDecisionRequest", "providers.remote.denyAuthorization": "RemoteDecisionRequest"}
	name, ok := names[method]
	if !ok || contracts.Validate("service", name, raw) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid remote connection request")
	}
	var r struct {
		WorkspaceID       string   `json:"workspaceId"`
		CertificateHandle string   `json:"certificateHandle"`
		PublicOrigin      string   `json:"publicOrigin"`
		BindAddress       string   `json:"bindAddress"`
		AllowedOrigins    []string `json:"allowedOrigins"`
		PageIDs           []string `json:"pageIds"`
		Tools             []string `json:"tools"`
		ConnectionID      string   `json:"connectionId"`
		AuthorizationID   string   `json:"authorizationId"`
		ReviewHash        string   `json:"reviewHash"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid remote request")
	}
	a.mu.RLock()
	k, e, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil || k.ID() != r.WorkspaceID {
		return errorEnvelope("WORKSPACE_LOCKED", "Remote operation belongs to another workspace")
	}
	if k.AuthorizeProviderAuth(e) != nil {
		return errorEnvelope("SCOPE_DENIED", "Only the current workspace owner can manage remote access")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bridge, err := a.getProviderBridge()
	if err != nil {
		return errorEnvelope("WORKSPACE_LOCKED", "No workspace open")
	}
	value, _ := providerRemoteStates.LoadOrStore(a, &providerRemoteState{certificates: map[string]remoteCertificate{}})
	state := value.(*providerRemoteState)
	state.mu.Lock()
	defer state.mu.Unlock()
	for id, c := range state.certificates {
		if !time.Now().Before(c.expires) {
			delete(state.certificates, id)
		}
	}
	success := func(result any) string {
		b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return string(b)
	}
	status := func() remote.Status {
		if state.server != nil {
			return state.server.Status()
		}
		return remote.Status{Connections: []remote.Connection{}, Authorizations: []remote.Authorization{}}
	}
	var result any
	switch method {
	case "providers.remote.status":
		result = status()
	case "providers.remote.selectCertificate":
		certPath, pickErr := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: "Select HTTPS certificate chain (PEM)"})
		if pickErr != nil || certPath == "" {
			return errorEnvelope("CANCELLED", "Certificate selection cancelled")
		}
		keyPath, pickErr := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: "Select matching HTTPS private key (PEM)"})
		if pickErr != nil || keyPath == "" {
			return errorEnvelope("CANCELLED", "Private key selection cancelled")
		}
		certPEM, readErr := remoteReadPEM(certPath)
		if readErr != nil {
			return errorEnvelope("INVALID_REQUEST", readErr.Error())
		}
		defer clear(certPEM)
		keyPEM, readErr := remoteReadPEM(keyPath)
		if readErr != nil {
			return errorEnvelope("INVALID_REQUEST", readErr.Error())
		}
		defer clear(keyPEM)
		pair, pairErr := tls.X509KeyPair(certPEM, keyPEM)
		if pairErr != nil || len(pair.Certificate) == 0 {
			return errorEnvelope("INVALID_REQUEST", "Certificate and unencrypted PEM key must match")
		}
		leaf, parseErr := x509.ParseCertificate(pair.Certificate[0])
		if parseErr != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return errorEnvelope("INVALID_REQUEST", "Certificate is not currently valid")
		}
		pair.Leaf = leaf
		handle := uuid.NewString()
		state.certificates = map[string]remoteCertificate{handle: {certificate: pair, expires: time.Now().Add(10 * time.Minute)}}
		result = map[string]any{"handle": handle, "dnsNames": leaf.DNSNames, "expiresAt": leaf.NotAfter}
	case "providers.remote.enable":
		if state.server != nil && state.server.Status().Enabled {
			return errorEnvelope("CONFLICT", "Disable the current listener before changing its configuration")
		}
		cert, ok := state.certificates[r.CertificateHandle]
		if !ok {
			return errorEnvelope("NOT_FOUND", "Select a certificate again; its handle expired")
		}
		config := remote.Config{PublicOrigin: r.PublicOrigin, BindAddress: r.BindAddress, AllowedOrigins: r.AllowedOrigins, Certificate: cert.certificate}
		var server *remote.Server
		server, err = remote.New(config, remote.Callbacks{RecordDecision: bridge.RecordRemoteDecision, Forward: bridge.RemoteMCPHandler(config.PublicOrigin, config.AllowedOrigins), Validate: func(ctx context.Context, id, token string) error {
			_, err := bridge.ManagedTools(id, token)
			return err
		}, Disconnect: bridge.Disconnect})
		if err == nil {
			err = server.Start()
		}
		if err == nil {
			state.server = server
			delete(state.certificates, r.CertificateHandle)
			result = status()
		}
	case "providers.remote.disable":
		if state.server != nil {
			err = state.server.Close()
			state.server = nil
		}
		state.certificates = map[string]remoteCertificate{}
		result = status()
	case "providers.remote.createConnection":
		if state.server == nil || !state.server.Status().Enabled {
			return errorEnvelope("CONFLICT", "Enable HTTPS before creating a connection")
		}
		var connection workspace.ProviderConnection
		connection, err = bridge.Connect(ctx, "mcp", r.PageIDs, r.Tools)
		if err == nil {
			result, err = state.server.AddConnection(ctx, remote.Binding{ID: connection.ID, Token: connection.Token, PageIDs: connection.PageIDs, Tools: connection.Tools})
			if err != nil {
				_ = bridge.Disconnect(ctx, connection.ID)
			}
		}
	case "providers.remote.disconnect":
		if state.server == nil {
			return errorEnvelope("NOT_FOUND", "Remote connection unavailable")
		}
		err = state.server.Disconnect(ctx, r.ConnectionID)
		result = status()
	case "providers.remote.approveAuthorization", "providers.remote.denyAuthorization":
		if state.server == nil {
			return errorEnvelope("NOT_FOUND", "Remote authorization unavailable")
		}
		err = state.server.Decide(ctx, r.AuthorizationID, r.ReviewHash, method == "providers.remote.approveAuthorization")
		result = status()
	}
	if err != nil {
		return errorEnvelope("INVALID_REQUEST", "Remote operation failed: verify the HTTPS configuration or review a fresh authorization request")
	}
	return success(result)
}
