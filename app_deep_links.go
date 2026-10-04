package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var linkID = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`
var linkPath = regexp.MustCompile(`^/` + linkID + `(?:/block/` + linkID + `)?$`)
var linkWorkspace = regexp.MustCompile(`^` + linkID + `$`)

func validateDeepLink(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 512 || u.Scheme != "trove" || u.Host != "page" || u.User != nil ||
		u.RawPath != "" || !linkPath.MatchString(u.Path) {
		return fmt.Errorf("invalid Trove link")
	}
	if u.Fragment != "" && (strings.Contains(u.Path, "/block/") || !linkWorkspace.MatchString(u.Fragment)) {
		return fmt.Errorf("invalid Trove block fragment")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid Trove link query")
	}
	for key, values := range query {
		if key != "workspace" || len(values) != 1 || !linkWorkspace.MatchString(values[0]) {
			return fmt.Errorf("invalid Trove workspace link")
		}
	}
	return nil
}

// Keep cold-launch links until the renderer has a workspace. Live events only
// signal availability; the one-shot read avoids duplicate navigation.
func (a *App) openURL(raw string) {
	if validateDeepLink(raw) != nil {
		return
	}
	a.mu.Lock()
	a.pendingDeepLink = raw
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "native:openURL")
	}
}

func (a *App) PendingDeepLink() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	raw := a.pendingDeepLink
	a.pendingDeepLink = ""
	return raw
}
