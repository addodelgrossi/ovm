package main

import (
	"context"
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	desktopConfig, err := parseDesktopOptions(os.Args[1:])
	if err != nil {
		log.Print("desktop application arguments are invalid")
		return
	}
	app, err := composeDesktop(context.Background(), desktopConfig, productionCompositionFactories())
	if err != nil {
		// Composition errors may wrap platform diagnostics. Keep the startup log
		// generic so no backend value can cross into a user-visible log.
		log.Print("desktop application could not be composed")
		return
	}
	defer app.shutdown(context.Background())

	err = wails.Run(&options.App{
		Title:                    "ovm",
		Width:                    1280,
		Height:                   800,
		MinWidth:                 960,
		MinHeight:                640,
		BackgroundColour:         &options.RGBA{R: 244, G: 246, B: 248, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		CSSDragProperty:          "--wails-draggable",
		CSSDragValue:             "drag",
		EnableDefaultContextMenu: false,
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				FullSizeContent:            true,
				HideToolbarSeparator:       true,
			},
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Print("desktop application failed")
	}
}
