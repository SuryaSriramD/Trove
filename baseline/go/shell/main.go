package main

import (
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed index.html
var assets embed.FS

func main() {
	if err := wails.Run(&options.App{Title: "Trove baseline probe", Width: 800, Height: 600,
		AssetServer: &assetserver.Options{Assets: assets}}); err != nil { panic(err) }
}
