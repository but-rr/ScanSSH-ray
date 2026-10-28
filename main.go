package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"scanssh-ray/backend/tray"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title: "ScanSSH-ray", Width: 1440, Height: 920, MinWidth: 760, MinHeight: 480,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 20, G: 21, B: 23, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup, OnDomReady: app.domReady, OnBeforeClose: app.beforeClose, OnShutdown: app.shutdown, Bind: []interface{}{app},
		Windows:            &windows.Options{Theme: windows.Dark, WebviewIsTransparent: false, WindowIsTranslucent: false},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: tray.Identity, OnSecondInstanceLaunch: func(_ options.SecondInstanceData) { app.restoreWindow() }},
	})
	if err != nil {
		log.Fatal(err)
	}
}
