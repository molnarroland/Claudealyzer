package main

import (
	"embed"
	"log"
	"runtime"

	spartannative "github.com/spartan-native/host-wails"
	"github.com/wailsapp/wails/v3/pkg/application"

	"claudealyzer/internal/core"
	"claudealyzer/internal/services"
)

// The Angular build output (frontend/dist) is embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[core.ScanStatus]("scan:progress")
	application.RegisterEvent[core.ScanStatus]("index:updated")
	application.RegisterEvent[[]string]("session:updated")
}

func main() {
	var app *application.App
	c := core.New(func(name string, data any) {
		if app != nil {
			app.Event.Emit(name, data)
		}
	})

	app = application.New(application.Options{
		Name:        "Claudealyzer",
		Description: "Where your coding agents' tokens, time and money go",
		Services: []application.Service{
			// Spartan Native: real OS appearance (accent, scheme, contrast, text size), window
			// buttons, native menus and dialogs for the @spartan-native/host-wails adapter.
			application.NewService(spartannative.New()),
			application.NewService(&services.SourceService{Core: c}),
			application.NewService(&services.HistoryService{Core: c}),
			application.NewService(&services.UsageService{Core: c}),
			application.NewService(&services.InsightService{Core: c}),
			application.NewService(&services.PricingService{Core: c}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Claudealyzer",
		Width:     1440,
		Height:    900,
		MinWidth:  1100,
		MinHeight: 680,
		// <sn-title-bar> draws the title bar and window buttons on Windows and Linux;
		// on macOS the OS keeps its traffic lights in a hidden-inset title bar.
		Frameless: runtime.GOOS != "darwin",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 38,
			Backdrop:                application.MacBackdropNormal,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
