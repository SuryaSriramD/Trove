package main

import (
	"context"
	"reflect"

	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/workspace"
)

// Authenticate the actual account on every read. The service separately checks
// current canonical selection and revision after network work has completed.
func readConnectedPreview(ctx context.Context, session connectors.Session, adapter connectors.Adapter, selection connectors.Selection, remoteID string) (connectors.Snapshot, error) {
	var empty connectors.Snapshot
	selected, err := connectors.Selected(session.Account, selection.ID)
	if err != nil || !reflect.DeepEqual(selected, selection) {
		return empty, workspace.Fail("SCOPE_DENIED", "Connected source selection changed")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	identity, err := adapter.Identity(ctx, session)
	if err != nil {
		return empty, err
	}
	if err = connectors.RequireIdentity(session.Account.Identity, identity); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	previewer, ok := adapter.(connectors.Previewer)
	if !ok {
		return empty, connectors.Unsupported("authenticated connected context")
	}
	fresh, err := previewer.Preview(ctx, session, selection, remoteID)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if fresh.RemoteID != remoteID {
		return empty, workspace.Fail("STALE_CITATION", "Connected source identity changed")
	}
	return fresh, nil
}

func (a *App) connectorContextReader(k *workspace.Kernel) workspace.ConnectorContextReader {
	current := func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.kernel == k }
	return func(ctx context.Context, account connectors.Account, selection connectors.Selection, remoteID string) (connectors.Snapshot, error) {
		var empty connectors.Snapshot
		if !current() {
			return empty, workspace.Fail("WORKSPACE_LOCKED", "Connected context belongs to another workspace")
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		host, err := a.getConnectors(k)
		if err != nil {
			return empty, err
		}
		type result struct {
			source connectors.Snapshot
			err    error
		}
		output := make(chan result, 1)
		err = host.start(ctx, workspace.NewID(), account.ID, func(flow context.Context) {
			session, err := host.broker.Session(flow, k.ID(), k.Path(), account)
			if err != nil {
				output <- result{err: err}
				return
			}
			adapter, err := connectors.NewAdapter(account.Kind)
			if err != nil {
				output <- result{err: err}
				return
			}
			source, err := readConnectedPreview(flow, session, adapter, selection, remoteID)
			if err == nil && !current() {
				err = workspace.Fail("WORKSPACE_LOCKED", "Workspace changed during connected context retrieval")
			}
			output <- result{source: source, err: err}
		})
		if err != nil {
			return empty, err
		}
		select {
		case <-ctx.Done():
			return empty, ctx.Err()
		case out := <-output:
			if err := ctx.Err(); err != nil {
				return empty, err
			}
			if out.err != nil {
				return empty, out.err
			}
			return out.source, nil
		}
	}
}
