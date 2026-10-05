package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"trove.local/phase0/internal/providers"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--trove-claude-login" {
		if len(os.Args) != 3 {
			os.Exit(1)
		}
		os.Exit(providers.RunClaudeLoginHelper(os.Args[2]))
	}
	app := NewApp()
	if err := app.acceptLaunchArguments(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	assets, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	desktopOptions := &options.App{Title: "Trove", Width: 1380, Height: 900, MinWidth: 960, MinHeight: 640,
		BackgroundColour: options.NewRGB(246, 245, 241),
		AssetServer:      &assetserver.Options{Assets: assets, Handler: http.HandlerFunc(app.serveFile), Middleware: preventAppFraming},
		OnStartup:        app.startup, OnDomReady: app.domReady, OnShutdown: app.shutdown, Bind: []interface{}{app},
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Mac:         &mac.Options{TitleBar: mac.TitleBarDefault(), OnUrlOpen: app.openURL},
	}
	configureDesktopPlatform(desktopOptions, app)
	err = wails.Run(desktopOptions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
