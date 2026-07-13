//go:build darwin

package app

import (
	"embed"
	"fmt"

	"floating-translator/internal/subtitle"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Run 装配并运行 macOS Wails 应用。
func Run(assets embed.FS, applicationIcon []byte) error {
	service := NewAppWithIcon(applicationIcon)
	wailsApp := application.New(application.Options{
		Name: "悬浮翻译器",
		Icon: applicationIcon,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		OnShutdown: service.shutdown,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               applicationInstanceID,
			OnSecondInstanceLaunch: service.secondInstanceLaunched,
		},
	})
	wailsApp.RegisterService(application.NewService(service))

	subtitleWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
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
	subtitleController := subtitle.NewWindow(subtitleWindow, service.desktop.CursorPosition)
	defer subtitleController.Close()

	settingsWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
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
		&wailsApplicationController{application: wailsApp},
		subtitleController,
		&wailsWindowController{window: settingsWindow},
	)
	settingsWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		service.runSafely("settings_native_close", func() {
			_ = service.CloseSettings()
		})
	})

	if err := wailsApp.Run(); err != nil {
		return fmt.Errorf("应用启动失败: %w", err)
	}
	return nil
}
