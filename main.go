package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is injected at build time with -ldflags "-X main.version=...". It shows
// in the window title so the running build is identifiable from a screenshot,
// which matters when diagnosing something that only reproduces in a release.
var version = "dev"

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "lifx-maestro " + version,
		Width:     1440,
		Height:    940,
		MinWidth:  1180,
		MinHeight: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 20, B: 24, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
