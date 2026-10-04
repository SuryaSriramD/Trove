package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	app := NewApp()
	for _, argument := range os.Args[1:] {
		app.openURL(argument)
	}
	assets, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	err = wails.Run(&options.App{Title: "Trove", Width: 1380, Height: 900, MinWidth: 960, MinHeight: 640,
		BackgroundColour: options.NewRGB(246, 245, 241),
		AssetServer:      &assetserver.Options{Assets: assets, Handler: http.HandlerFunc(app.serveFile), Middleware: preventAppFraming},
		OnStartup:        app.startup, OnDomReady: app.domReady, OnShutdown: app.shutdown, Bind: []interface{}{app},
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Mac:         &mac.Options{TitleBar: mac.TitleBarDefault(), OnUrlOpen: app.openURL},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
