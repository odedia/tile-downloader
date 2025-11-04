package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()
	broadcom := NewBroadcomService()
	aiModel := NewAIModelService()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Tile Downloader",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			broadcom.startup(ctx)
			aiModel.startup(ctx)
		},
		Bind: []interface{}{
			app,
			broadcom,
			aiModel,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
