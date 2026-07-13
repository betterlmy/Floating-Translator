//go:build windows

package app

import (
	"embed"
	"fmt"

	"floating-translator/internal/subtitle"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Run 装配并运行 Windows Wails 应用。
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

	subtitleWindow, err := subtitle.NewWindow(service.reportSubtitleRenderError)
	if err != nil {
		return fmt.Errorf("创建字幕窗口失败: %w", err)
	}
	defer subtitleWindow.Close()

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
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: true,
		},
	})
	service.setWindows(
		&wailsApplicationController{application: wailsApp},
		subtitleWindow,
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
