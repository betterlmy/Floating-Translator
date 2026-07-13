//go:build windows

package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var applicationIcon []byte

func main() {
	service := NewAppWithIcon(applicationIcon)
	app := application.New(application.Options{
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
	app.RegisterService(application.NewService(service))

	subtitle, err := newNativeSubtitleWindow(service.reportSubtitleRenderError)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "创建字幕窗口失败: %v\n", err)
		return
	}
	defer subtitle.Close()

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
			DisableFramelessWindowDecorations: true,
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
