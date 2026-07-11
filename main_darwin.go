//go:build darwin

package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var darwinAssets embed.FS

//go:embed build/appicon.png
var darwinApplicationIcon []byte

func main() {
	service := NewAppWithIcon(darwinApplicationIcon)
	app := application.New(application.Options{
		Name: "悬浮翻译器",
		Icon: darwinApplicationIcon,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(darwinAssets),
		},
		OnShutdown: service.shutdown,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               applicationInstanceID,
			OnSecondInstanceLaunch: service.secondInstanceLaunched,
		},
	})
	app.RegisterService(application.NewService(service))

	subtitleWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "subtitle",
		Title:                      subtitleWindowTitle,
		URL:                        "/?view=subtitle",
		Width:                      1000,
		Height:                     300,
		Frameless:                  true,
		Hidden:                     true,
		AlwaysOnTop:                true,
		IgnoreMouseEvents:          true,
		BackgroundType:             application.BackgroundTypeTransparent,
		BackgroundColour:           application.NewRGBA(0, 0, 0, 0),
		DefaultContextMenuDisabled: true,
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropTransparent,
			DisableShadow: true,
			TitleBar: application.MacTitleBar{
				Hide:               true,
				AppearsTransparent: true,
				FullSizeContent:    true,
			},
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
	})
	subtitle := &darwinSubtitleWindow{window: subtitleWindow}

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
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropNormal,
			TitleBar: application.MacTitleBar{
				Hide:               true,
				AppearsTransparent: true,
				FullSizeContent:    true,
			},
			TabbingMode: application.MacWindowTabbingModeDisallowed,
		},
	})

	service.setWindows(
		&wailsApplicationController{application: app},
		subtitle,
		&wailsWindowController{window: settingsWindow},
	)
	settingsWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		service.runSafely("settings_native_close", func() {
			_ = service.CloseSettings()
		})
	})

	if err := app.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "应用启动失败: %v\n", err)
	}
}
