package main

import (
	"fmt"
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

const desktopInstanceID = "local.trove.desktop"

// OS protocol handlers supply one URL, never a workspace path or executable
// option. Validate the whole payload before changing the one-shot link slot.
func (a *App) acceptLaunchArguments(args []string) error {
	if len(args) == 0 {
		return nil
	}
	if len(args) != 1 || validateDeepLink(args[0]) != nil {
		return fmt.Errorf("unsupported Trove launch arguments")
	}
	a.openURL(args[0])
	return nil
}

func desktopInstanceLock(platform string, a *App) *options.SingleInstanceLock {
	if platform != "windows" && platform != "linux" {
		return nil
	}
	return &options.SingleInstanceLock{UniqueId: desktopInstanceID, OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
		// WorkingDirectory and arbitrary options are untrusted OS delivery data.
		// Navigation still goes through the renderer's normal workspace selection.
		_ = a.acceptLaunchArguments(data.Args)
	}}
}

func configureDesktopPlatform(o *options.App, a *App) {
	o.SingleInstanceLock = desktopInstanceLock(runtime.GOOS, a)
	if runtime.GOOS == "windows" {
		messages := windows.DefaultMessages()
		messages.ContactAdmin = "Trove requires Microsoft Edge WebView2 Runtime. Install the approved offline WebView2 runtime, then start Trove again. Trove does not download it automatically."
		o.Windows = &windows.Options{Messages: messages, DLLSearchPaths: windows.DLLSearchApplicationDir | windows.DLLSearchSystem32}
	}
}

func desktopHelperFilename(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func desktopFolderCommand(platform, folder string) (string, []string, error) {
	switch platform {
	case "darwin":
		return "/usr/bin/open", []string{folder}, nil
	case "windows":
		return "explorer.exe", []string{folder}, nil
	case "linux":
		return "/usr/bin/xdg-open", []string{folder}, nil
	default:
		return "", nil, fmt.Errorf("opening folders is unavailable on this platform")
	}
}
