package app

import (
	"fmt"

	"floating-translator/internal/config"
	"floating-translator/internal/logger"
	"floating-translator/internal/processor"
)

func (a *App) applySubtitleConfig(cfg config.SubtitleConfig) error {
	if a.subtitle == nil {
		return fmt.Errorf("字幕窗口尚未初始化")
	}
	if a.application == nil {
		return fmt.Errorf("应用窗口运行时尚未初始化")
	}
	area, err := a.application.PrimaryWorkArea()
	if err != nil {
		return fmt.Errorf("读取主屏幕工作区失败: %w", err)
	}
	bounds, err := calculateSubtitleWindowBounds(cfg, area)
	if err != nil {
		return err
	}
	return a.subtitle.Configure(bounds, cfg)
}

func calculateSubtitleWindowBounds(cfg config.SubtitleConfig, area workArea) (windowBounds, error) {
	if area.Width <= 0 || area.Height <= 0 {
		return windowBounds{}, fmt.Errorf("主屏幕工作区尺寸无效: %dx%d", area.Width, area.Height)
	}
	width := area.Width * cfg.WidthPercent / 100
	height := area.Height * subtitleHeightPercent / 100
	x := area.X + (area.Width-width)/2
	y := area.Y + area.Height - height - area.Height*cfg.BottomOffsetPercent/100
	return windowBounds{X: x, Y: y, Width: width, Height: height}, nil
}

func (a *App) emitTranslation(event processor.Event) {
	a.mutex.RLock()
	ready := a.frontendReady
	a.mutex.RUnlock()
	if !ready || a.subtitle == nil {
		return
	}
	a.subtitle.Display(event)
}

func (a *App) reportSubtitleRenderError(err error) {
	a.logger.Warn("原生字幕渲染失败", logger.ErrorField(err))
}
