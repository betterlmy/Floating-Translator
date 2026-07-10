//go:build windows

package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type wailsApplicationController struct {
	application *application.App
}

func (c *wailsApplicationController) Quit() {
	c.application.Quit()
}

func (c *wailsApplicationController) PrimaryWorkArea() (workArea, error) {
	screen := c.application.Screen.GetPrimary()
	if screen == nil {
		return workArea{}, errors.New("Wails 未返回主屏幕")
	}
	return workArea{
		X:      screen.WorkArea.X,
		Y:      screen.WorkArea.Y,
		Width:  screen.WorkArea.Width,
		Height: screen.WorkArea.Height,
	}, nil
}

type wailsWindowController struct {
	window *application.WebviewWindow
}

func (c *wailsWindowController) Show() {
	c.window.Show()
}

func (c *wailsWindowController) Hide() {
	c.window.Hide()
}

func (c *wailsWindowController) Focus() {
	c.window.Focus()
}

func (c *wailsWindowController) SetAlwaysOnTop(enabled bool) {
	c.window.SetAlwaysOnTop(enabled)
}

func (c *wailsWindowController) SetSize(width int, height int) {
	c.window.SetSize(width, height)
}

func (c *wailsWindowController) SetPosition(x int, y int) {
	c.window.SetPosition(x, y)
}

func (c *wailsWindowController) EmitEvent(name string, data any) {
	c.window.EmitEvent(name, data)
}

// ServiceStartup 初始化桌面集成与应用配置。
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}

func (a *App) secondInstanceLaunched(_ application.SecondInstanceData) {
	a.logger.Info("检测到第二个应用实例，已阻止重复启动")
}
