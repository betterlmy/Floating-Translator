package app

import (
	"errors"

	"floating-translator/internal/config"
	"floating-translator/internal/fonts"
	subtitlepkg "floating-translator/internal/subtitle"
)

func (a *App) showSettings() {
	a.mutex.RLock()
	alreadyOpen := a.settingsOpen
	a.mutex.RUnlock()
	if a.settingsWindow == nil {
		return
	}
	a.mutex.Lock()
	a.settingsOpen = true
	a.mutex.Unlock()
	a.settingsWindow.Show()
	a.settingsWindow.Focus()
	if !alreadyOpen {
		a.settingsWindow.EmitEvent(settingsRefreshEvent, nil)
		a.logger.Info("已打开设置窗口")
	}
}

// GetSettings 返回设置窗口需要的完整配置，不包含已有 API Key 明文。
func (a *App) GetSettings() (config.Settings, error) {
	a.configurationMutex.Lock()
	defer a.configurationMutex.Unlock()
	return config.LoadSettingsFile(a.paths.ConfigFile)
}

// GetAvailableFonts 返回当前平台可用的字体族名称。
func (a *App) GetAvailableFonts() ([]string, error) {
	return fonts.List()
}

// SaveSettings 校验并保存设置，成功时立即应用；清空 API Key 后进入配置错误状态。
func (a *App) SaveSettings(settings config.Settings) error {
	a.configurationMutex.Lock()
	defer a.configurationMutex.Unlock()
	if err := config.SaveSettingsFile(a.paths.ConfigFile, settings); err != nil {
		return err
	}
	updatedConfig, err := config.LoadFile(a.paths.ConfigFile)
	if err != nil {
		if errors.Is(err, config.ErrMissingAPIKey) {
			a.setConfigError(err)
			return nil
		}
		return err
	}
	if err := a.installConfig(updatedConfig, true); err != nil {
		a.setConfigError(err)
		return err
	}
	a.logger.Info("设置已保存并应用")
	return nil
}

// RenderSubtitlePreview 以当前平台的真实字幕渲染器生成设置页预览图。
func (a *App) RenderSubtitlePreview(subtitle config.SubtitleConfig, width int, height int, deviceScale float64) (string, error) {
	return subtitlepkg.RenderPreview(subtitle, width, height, deviceScale)
}

// ReportSubtitleBounds 接收 Vue 测得的字幕文字边界，用于鼠标穿透状态下的原生悬停检测。
func (a *App) ReportSubtitleBounds(x int, y int, width int, height int, visible bool) {
	if a.subtitle != nil {
		a.subtitle.SetContentBounds(x, y, width, height, visible)
	}
}

// CloseSettings 隐藏独立的设置窗口。
func (a *App) CloseSettings() error {
	a.mutex.RLock()
	if !a.settingsOpen {
		a.mutex.RUnlock()
		return nil
	}
	a.mutex.RUnlock()
	if a.settingsWindow != nil {
		a.settingsWindow.Hide()
	}
	a.mutex.Lock()
	a.settingsOpen = false
	a.mutex.Unlock()
	a.logger.Info("已关闭设置窗口")
	return nil
}
