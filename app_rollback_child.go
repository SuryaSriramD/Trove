package main

import (
	"context"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sync"
	"sync/atomic"
	"trove.local/phase0/internal/updates"
)

type rollbackChildState struct {
	pending atomic.Bool
	child   *updates.RollbackChild
	ready   sync.Once
}

func (a *App) requireRollbackCommitted() error {
	if a.rollback != nil && a.rollback.pending.Load() {
		return errors.New("restored workspace is waiting for verified rollback handoff")
	}
	return nil
}
func (a *App) attachRollbackChild(child *updates.RollbackChild, abort func()) {
	a.rollback = &rollbackChildState{child: child}
	a.rollback.pending.Store(true)
	go func() { <-child.Failed(); abort() }()
}
func (a *App) confirmRollbackShellReady() {
	state := a.rollback
	if state == nil || state.child == nil {
		return
	}
	state.ready.Do(func() {
		go func() {
			a.mu.RLock()
			ctx := a.ctx
			a.mu.RUnlock()
			if ctx == nil {
				ctx = context.Background()
			}
			if err := a.waitForStartup(ctx); err != nil {
				state.child.Abort()
				return
			}
			a.lifecycle.Lock()
			defer a.lifecycle.Unlock()
			select {
			case <-a.shutdownDone:
				state.child.Abort()
				return
			default:
			}
			a.mu.RLock()
			k := a.kernel
			a.mu.RUnlock()
			if k == nil {
				state.child.Abort()
				return
			}
			format, err := k.DescribeCompatibility(ctx)
			if err != nil {
				state.child.Abort()
				return
			}
			actual := updates.RollbackTarget{WorkspacePath: k.Path(), WorkspaceID: k.ID(), UserVersion: format.UserVersion, StorageGeneration: format.StorageGeneration}
			// The callback runs only after the private parent commit. Required runtime
			// initialization must succeed before the final acknowledgement lets it quit.
			if err = state.child.ConfirmReady(actual, func() error { a.mu.Lock(); defer a.mu.Unlock(); return k.CompleteCompatibleStartup(ctx) }); err != nil {
				state.child.Abort()
				return
			}
			markStartupStage("rollback_committed")
			state.pending.Store(false)
			a.startJobs(k)
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "workspace:changed", map[string]any{"rollbackCommitted": true})
			}
		}()
	})
}
