//go:build windows

package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	subtitleWindowTitle   = "悬浮翻译器"
	settingsWindowTitle   = "悬浮翻译器设置"
	applicationInstanceID = "4d80d8a9-6da6-47a6-b64f-6e24b77e65d2"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service := NewApp()
	app := application.New(application.Options{
		Name: "悬浮翻译器",
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		OnShutdown: service.shutdown,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               applicationInstanceID,
			OnSecondInstanceLaunch: service.secondInstanceLaunched,
		},
	})
	app.RegisterService(application.NewService(service))

	overlayWindow := app.Window.NewWithOptions(subtitleWindowOptions())
	settingsWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "settings",
		Title:                      settingsWindowTitle,
		URL:                        "/?view=settings",
		Width:                      settingsWindowWidth,
		Height:                     settingsWindowHeight,
		MinWidth:                   720,
		MinHeight:                  560,
		DisableResize:              false,
		Frameless:                  true,
		Hidden:                     true,
		AlwaysOnTop:                true,
		BackgroundColour:           application.NewRGB(16, 20, 22),
		DefaultContextMenuDisabled: true,
		Windows: application.WindowsWindow{
			DisableIcon:                       true,
			DisableFramelessWindowDecorations: true,
		},
	})
	service.setWindows(
		&wailsApplicationController{application: app},
		&wailsWindowController{window: overlayWindow},
		&wailsWindowController{window: settingsWindow},
	)
	settingsWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		service.runSafely("settings_native_close", func() {
			_ = service.CloseSettings()
		})
	})

	err := app.Run()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "应用启动失败: %v\n", err)
	}
}

func subtitleWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:                       "subtitle",
		Title:                      subtitleWindowTitle,
		Width:                      1000,
		Height:                     300,
		DisableResize:              true,
		Frameless:                  true,
		Hidden:                     true,
		AlwaysOnTop:                true,
		IgnoreMouseEvents:          true,
		BackgroundType:             application.BackgroundTypeTransparent,
		BackgroundColour:           application.NewRGBA(0, 0, 0, 0),
		DefaultContextMenuDisabled: true,
		Windows: application.WindowsWindow{
			DisableIcon:                       true,
			DisableFramelessWindowDecorations: true,
			HiddenOnTaskbar:                   true,
			WebView2CompositionHosting:        true,
		},
	}
}
