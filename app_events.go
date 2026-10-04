package main

import (
	"encoding/json"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/workspace"
)

func (a *App) subscribe(k *workspace.Kernel) {
	channel, stop := k.SubscribeChanges()
	a.mu.Lock()
	a.stopEvents = stop
	a.mu.Unlock()
	go func() {
		for {
			for event := range channel {
				var value map[string]any
				if json.Unmarshal(event, &value) != nil {
					continue
				}
				name := "workspace:changed"
				if value["jobRevision"] != nil {
					name = "jobs:progress"
				}
				if value["eventType"] == "imports.previewProgress" {
					name = "imports:previewProgress"
				}
				runtime.EventsEmit(a.ctx, name, value)
			}
			// A bounded kernel subscriber closes on overflow. Reconnect before
			// requesting a fresh snapshot, so no intervening commit is lost.
			a.mu.Lock()
			if a.kernel != k || a.ctx.Err() != nil {
				a.mu.Unlock()
				return
			}
			channel, stop = k.SubscribeChanges()
			a.stopEvents = stop
			a.mu.Unlock()
			runtime.EventsEmit(a.ctx, "workspace:changed", map[string]any{"requiresSnapshot": true})
		}
	}()
}
