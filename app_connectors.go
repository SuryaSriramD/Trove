package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/workspace"
)

type connectorFlight struct {
	accountID string
	cancel    context.CancelFunc
	done      chan struct{}
}
type connectorAttemptBinding struct{ accountID, expectedRevision, workspaceID string }
type connectorHost struct {
	mu       sync.Mutex
	broker   *connectors.Broker
	flights  map[string]*connectorFlight
	attempts map[string]connectorAttemptBinding
	closing  bool
}

var connectorHosts sync.Map

func (a *App) getConnectors(k *workspace.Kernel) (*connectorHost, error) {
	if value, ok := connectorHosts.Load(a); ok {
		return value.(*connectorHost), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, errors.New("Connector account storage is unavailable")
	}
	root := filepath.Join(base, "Trove", "connector-auth")
	if pathsOverlap(root, k.Path()) {
		return nil, errors.New("Connector credential storage must be outside the workspace")
	}
	broker, err := connectors.NewBroker(root)
	if err != nil {
		return nil, err
	}
	host := &connectorHost{broker: broker, flights: map[string]*connectorFlight{}, attempts: map[string]connectorAttemptBinding{}}
	value, loaded := connectorHosts.LoadOrStore(a, host)
	if loaded {
		broker.Close()
		return value.(*connectorHost), nil
	}
	return host, nil
}
func (a *App) stopConnectors() {
	if value, ok := connectorHosts.LoadAndDelete(a); ok {
		host := value.(*connectorHost)
		host.mu.Lock()
		host.closing = true
		flights := []*connectorFlight{}
		for _, f := range host.flights {
			f.cancel()
			flights = append(flights, f)
		}
		host.mu.Unlock()
		host.broker.Close()
		for _, f := range flights {
			<-f.done
		}
	}
}
func connectorError(err error) string {
	var f *workspace.ServiceFailure
	if errors.As(err, &f) {
		return errorEnvelope(f.Code, f.Message)
	}
	var c *connectors.Failure
	if errors.As(err, &c) {
		return errorEnvelope("WORKER_FAILED", c.Message)
	}
	return errorEnvelope("WORKER_FAILED", "Connector operation failed; review its account and retained operation history")
}
func connectorSuccess(v any) string {
	b, _ := json.Marshal(map[string]any{"ok": true, "result": v})
	return string(b)
}
func (h *connectorHost) start(ctx context.Context, id, account string, fn func(context.Context)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return errors.New("connector host is closing")
	}
	if h.flights[id] != nil {
		return workspace.Fail("CONFLICT", "This connector operation is already running")
	}
	if len(h.flights) >= 4 {
		return workspace.Fail("RESOURCE_EXHAUSTED", "Wait for an existing connector operation or cancel it")
	}
	flow, cancel := context.WithTimeout(ctx, 10*time.Minute)
	f := &connectorFlight{accountID: account, cancel: cancel, done: make(chan struct{})}
	h.flights[id] = f
	go func() {
		defer close(f.done)
		defer cancel()
		defer func() { h.mu.Lock(); delete(h.flights, id); h.mu.Unlock() }()
		fn(flow)
	}()
	return nil
}
func (a *App) connectorDispatch(method string, raw json.RawMessage) string {
	definitions := map[string]string{
		"connectors.native.authBegin": "ConnectorAuthBeginRequest", "connectors.native.authPoll": "ConnectorAuthAttemptRequest", "connectors.native.authCancel": "ConnectorAuthAttemptRequest",
		"connectors.native.disconnect": "ConnectorDisconnectRequest", "connectors.native.discover": "ConnectorDiscoverRequest", "connectors.native.refresh": "ConnectorRefreshRequest",
		"connectors.native.prepare": "ConnectorPrepareRequest", "connectors.native.execute": "ConnectorExecuteRequest", "connectors.native.reconcile": "ConnectorProposalRequest", "connectors.native.cancel": "ConnectorCancelRequest",
	}
	definition := definitions[method]
	if definition == "" || contracts.Validate("service", definition, raw) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid connector operation")
	}
	var r struct {
		WorkspaceID      string                  `json:"workspaceId"`
		AccountID        string                  `json:"accountId"`
		ExpectedRevision string                  `json:"expectedRevision"`
		AttemptID        string                  `json:"attemptId"`
		ProposalID       string                  `json:"proposalId"`
		Hash             string                  `json:"hash"`
		Cursor           string                  `json:"cursor"`
		ResumeID         string                  `json:"resumeId"`
		OperationID      string                  `json:"operationId"`
		Registration     connectors.Registration `json:"registration"`
		Action           connectors.Action       `json:"action"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid connector operation")
	}
	a.mu.RLock()
	k, e, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil || k.ID() != r.WorkspaceID {
		return errorEnvelope("WORKSPACE_LOCKED", "Connector request belongs to another workspace")
	}
	if err := k.AuthorizeConnector(e); err != nil {
		return connectorError(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	host, err := a.getConnectors(k)
	if err != nil {
		return connectorError(err)
	}
	switch method {
	case "connectors.native.authBegin":
		if _, err = connectors.NewAdapter(r.Registration.Kind); err != nil {
			return connectorError(err)
		}
		if r.AccountID == "" {
			r.AccountID = workspace.NewID()
		} else {
			account, readErr := k.ConnectorAccount(ctx, e, r.AccountID)
			if readErr != nil {
				return connectorError(readErr)
			}
			if account.Revision != r.ExpectedRevision || account.Kind != r.Registration.Kind {
				return errorEnvelope("CONFLICT", "Reload the connection before authorizing it")
			}
		}
		attempt, err := host.broker.Begin(ctx, k.ID(), k.Path(), r.AccountID, r.Registration, func(target string) error { runtime.BrowserOpenURL(ctx, target); return nil })
		if err != nil {
			return connectorError(err)
		}
		host.mu.Lock()
		host.attempts[attempt.ID] = connectorAttemptBinding{accountID: r.AccountID, expectedRevision: r.ExpectedRevision, workspaceID: k.ID()}
		host.mu.Unlock()
		return connectorSuccess(attempt)
	case "connectors.native.authCancel":
		return connectorSuccessOrError(nil, host.broker.Cancel(r.AttemptID))
	case "connectors.native.authPoll":
		host.mu.Lock()
		binding, exists := host.attempts[r.AttemptID]
		host.mu.Unlock()
		if !exists || binding.workspaceID != k.ID() {
			return errorEnvelope("NOT_FOUND", "Authorization attempt is unavailable")
		}
		attempt, err := host.broker.Attempt(r.AttemptID)
		if err != nil {
			return connectorError(err)
		}
		if attempt.State == "authorized" {
			adapter, err := connectors.NewAdapter(attempt.Kind)
			if err != nil {
				return connectorError(err)
			}
			account, err := host.broker.Complete(ctx, r.AttemptID, adapter)
			if err != nil {
				return connectorError(err)
			}
			if account.ID != binding.accountID {
				_ = host.broker.Forget(ctx, account.RegistrationID)
				return errorEnvelope("CONFLICT", "Authorization account binding changed")
			}
			if _, err = k.ConnectConnector(ctx, e, account, binding.expectedRevision); err != nil {
				_ = host.broker.Forget(ctx, account.RegistrationID)
				return connectorError(err)
			}
			attempt, _ = host.broker.Attempt(r.AttemptID)
		}
		return connectorSuccess(attempt)
	case "connectors.native.cancel":
		host.mu.Lock()
		f := host.flights[r.OperationID]
		if f != nil {
			f.cancel()
		}
		host.mu.Unlock()
		return connectorSuccess(map[string]any{"state": "cancellation_requested", "operationId": r.OperationID})
	case "connectors.native.disconnect":
		account, err := k.ConnectorAccount(ctx, e, r.AccountID)
		if err != nil {
			return connectorError(err)
		}
		if err = k.DisconnectConnector(ctx, e, account.ID, r.ExpectedRevision); err != nil {
			return connectorError(err)
		}
		host.mu.Lock()
		for _, f := range host.flights {
			if f.accountID == account.ID {
				f.cancel()
			}
		}
		host.mu.Unlock()
		if account.RegistrationID != "" {
			err = host.broker.Forget(ctx, account.RegistrationID)
		}
		if err != nil {
			return connectorSuccess(map[string]any{"state": "disconnected", "localCredentialCleared": false, "detail": "Connection is inactive. Protected credential deletion failed; retry cleanup on this host."})
		}
		return connectorSuccess(map[string]any{"state": "disconnected", "localCredentialCleared": true, "remoteRevoked": false, "detail": "Local access is disconnected. Provider authorization can also be revoked in your provider account settings."})
	}
	if r.ProposalID != "" {
		proposal, err := k.ConnectorProposal(ctx, e, r.ProposalID)
		if err != nil {
			return connectorError(err)
		}
		r.AccountID = proposal.AccountID
	}
	account, err := k.ConnectorAccount(ctx, e, r.AccountID)
	if err != nil {
		return connectorError(err)
	}
	adapter, err := connectors.NewAdapter(account.Kind)
	if err != nil {
		return connectorError(err)
	}
	session, err := host.broker.Session(ctx, k.ID(), k.Path(), account)
	if err != nil {
		return connectorError(err)
	}
	switch method {
	case "connectors.native.discover":
		options, cursor, err := adapter.Discover(ctx, session, r.Cursor)
		if err != nil {
			return connectorError(err)
		}
		return connectorSuccess(map[string]any{"selections": options, "nextCursor": cursor})
	case "connectors.native.refresh":
		j, err := k.PrepareConnectorRefresh(ctx, e, account.ID, r.ResumeID)
		if err != nil {
			return connectorError(err)
		}
		err = host.start(ctx, j.ID, account.ID, func(flow context.Context) { _ = k.RunConnectorRefresh(flow, e, j, adapter, session) })
		if err != nil {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			_ = k.RunConnectorRefresh(cancelled, e, j, adapter, session)
			return connectorError(err)
		}
		return connectorSuccess(j)
	case "connectors.native.prepare":
		p, err := k.PrepareConnectorProposal(ctx, e, account.ID, r.Action, adapter, session)
		return connectorSuccessOrError(p, err)
	case "connectors.native.execute":
		err = host.start(ctx, r.ProposalID, account.ID, func(flow context.Context) {
			_, _ = k.ExecuteConnectorProposal(flow, e, r.ProposalID, r.Hash, adapter, session)
		})
		if err != nil {
			return connectorError(err)
		}
		return connectorSuccess(map[string]any{"operationId": r.ProposalID, "state": "started"})
	case "connectors.native.reconcile":
		receipt, err := k.ReconcileConnectorProposal(ctx, e, r.ProposalID, adapter, session)
		return connectorSuccessOrError(receipt, err)
	}
	return errorEnvelope("INVALID_REQUEST", "Unknown connector operation")
}
func connectorSuccessOrError(result any, err error) string {
	if err != nil {
		return connectorError(err)
	}
	return connectorSuccess(result)
}
