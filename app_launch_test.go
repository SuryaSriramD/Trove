package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestDesktopSecondLaunchValidatesWholePayload(t *testing.T) {
	link := "trove://page/019a1234-5678-7000-8000-000000000002?workspace=019a1234-5678-7000-8000-000000000001"
	for _, platform := range []string{"windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			app := NewApp()
			lock := desktopInstanceLock(platform, app)
			if lock == nil || lock.UniqueId != desktopInstanceID {
				t.Fatal("missing fixed app instance identity")
			}
			for _, args := range [][]string{{"--workspace", "/private/workspace"}, {link, "--trove-claude-login"}, {"/tmp/notes"}, {"https://example.com"}} {
				lock.OnSecondInstanceLaunch(options.SecondInstanceData{Args: args, WorkingDirectory: "/private/workspace"})
				if app.PendingDeepLink() != "" {
					t.Fatal("untrusted secondary payload changed navigation")
				}
			}
			lock.OnSecondInstanceLaunch(options.SecondInstanceData{Args: []string{link}, WorkingDirectory: "/private/workspace"})
			if app.PendingDeepLink() != link || app.PendingDeepLink() != "" {
				t.Fatal("valid secondary URL lost or duplicated")
			}
			if app.kernel != nil {
				t.Fatal("URL delivery opened a workspace without normal selection")
			}
		})
	}
	if desktopInstanceLock("darwin", NewApp()) != nil {
		t.Fatal("macOS URL event contract changed")
	}
}
func TestDesktopFolderLaunchUsesOneLiteralArgument(t *testing.T) {
	path := `/tmp/space and "$(do not run)"`
	for _, platform := range []string{"darwin", "windows", "linux"} {
		command, args, err := desktopFolderCommand(platform, path)
		if err != nil || command == "" || len(args) != 1 || args[0] != path {
			t.Fatal(platform, command, args, err)
		}
	}
	if _, _, err := desktopFolderCommand("other", path); err == nil {
		t.Fatal("unknown platform launched")
	}
}
