package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"floating-translator/internal/config"
	"floating-translator/internal/filter"
	"floating-translator/internal/hotkey"
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
)

func (a *App) applySelectionHotkey(cfg config.SelectionConfig) error {
	if !cfg.Enable {
		return a.desktop.SetSelectionHotkey(nil)
	}
	shortcut, err := hotkey.Parse(cfg.Hotkey)
	if err != nil {
		return fmt.Errorf("解析划词翻译快捷键失败: %w", err)
	}
	if err := a.desktop.SetSelectionHotkey(&shortcut); err != nil {
		return err
	}
	a.logger.Info("划词翻译快捷键已启用", logger.String("hotkey", selectionShortcutLabel(cfg.Hotkey)))
	return nil
}

func (a *App) translateSelection() {
	a.mutex.RLock()
	valid := a.configValid
	cfg := a.config
	a.mutex.RUnlock()
	if a.processor == nil {
		return
	}
	if !valid {
		a.processor.EmitMessage("selection", "划词翻译不可用：请先修复应用配置")
		return
	}

	if !a.selectionReadMutex.TryLock() {
		a.processor.EmitMessage("selection", "划词翻译：上一次选区读取尚未完成，请稍后重试")
		return
	}
	defer a.selectionReadMutex.Unlock()
	a.processor.BeginSelection()
	defer a.processor.EndSelection()
	a.processor.EmitPendingMessage("selection", "翻译中...")

	text, err := a.readSelectedTextLocked(cfg)
	if err != nil {
		a.logger.Warn("读取选中文本失败", logger.ErrorField(err))
		message := "划词翻译失败：无法读取当前应用的选中文本"
		switch {
		case errors.Is(err, platform.ErrNoSelectedText):
			message = "划词翻译：未获取到选中文本"
		case errors.Is(err, platform.ErrSelectionUnsupported):
			message = "划词翻译失败：当前应用不支持读取选中文本"
		case errors.Is(err, platform.ErrSelectedTextTooLong):
			message = fmt.Sprintf("划词翻译失败：选中文本超过 %d 字符", cfg.Clipboard.MaxTextLength)
		case errors.Is(err, platform.ErrSelectionBusy):
			message = "划词翻译：上一次选区读取尚未完成，请稍后重试"
		case errors.Is(err, platform.ErrClipboardUnsafe):
			message = "划词翻译失败：原剪贴板包含复杂格式，已取消兼容复制以避免覆盖内容"
		case errors.Is(err, platform.ErrClipboardChangedDuringCopy):
			message = "划词翻译已取消：兼容复制期间检测到新的剪贴板内容，已保留该内容"
		case errors.Is(err, context.DeadlineExceeded):
			message = "划词翻译失败：读取选中文本超时"
		}
		a.processor.EmitMessage("selection", message)
		return
	}
	text = filter.Normalize(text)
	if text == "" {
		a.processor.EmitMessage("selection", "划词翻译：未获取到选中文本")
		return
	}
	if utf8.RuneCountInString(text) > cfg.Clipboard.MaxTextLength {
		a.processor.EmitMessage("selection", fmt.Sprintf("划词翻译失败：选中文本超过 %d 字符", cfg.Clipboard.MaxTextLength))
		return
	}
	if filter.ContainsSensitive(text) {
		a.logger.Warn("划词翻译已阻止疑似敏感文本", logger.Int("text_length", utf8.RuneCountInString(text)))
		a.processor.EmitMessage("selection", "划词翻译已取消：选中文本疑似包含密钥或敏感凭据")
		return
	}
	a.processor.HandleSelection(text)
}

func (a *App) readSelectedText(cfg config.Config) (string, error) {
	if !a.selectionReadMutex.TryLock() {
		return "", platform.ErrSelectionBusy
	}
	defer a.selectionReadMutex.Unlock()
	return a.readSelectedTextLocked(cfg)
}

func (a *App) readSelectedTextLocked(cfg config.Config) (string, error) {
	directContext, cancelDirect := context.WithTimeout(a.context, 3*time.Second)
	defer cancelDirect()
	text, err := a.desktop.SelectedText(directContext, cfg.Clipboard.MaxTextLength+1)
	if err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	if err == nil {
		err = platform.ErrNoSelectedText
	}
	if !cfg.Selection.CompatibilityMode || errors.Is(err, platform.ErrSelectedTextTooLong) || errors.Is(err, context.Canceled) {
		return "", err
	}

	a.logger.Info("直接读取选区失败，尝试强制兼容模式", logger.ErrorField(err))
	compatibilityContext, cancelCompatibility := context.WithTimeout(a.context, 2*time.Second)
	defer cancelCompatibility()
	return a.desktop.CompatibleSelectedText(compatibilityContext, cfg.Clipboard.MaxTextLength+1)
}

func (a *App) toggleSelection() {
	a.configurationMutex.Lock()
	defer a.configurationMutex.Unlock()
	a.mutex.RLock()
	if !a.configValid {
		a.mutex.RUnlock()
		return
	}
	enabled := !a.config.Selection.Enable
	configPath := a.paths.ConfigFile
	a.mutex.RUnlock()

	if err := config.SetSelectionEnabled(configPath, enabled); err != nil {
		a.logger.Error("保存划词翻译开关失败", logger.Bool("enabled", enabled), logger.ErrorField(err))
		return
	}
	updatedConfig, err := config.LoadFile(configPath)
	if err != nil {
		a.setConfigError(err)
		return
	}
	if err := a.installConfig(updatedConfig, true); err != nil {
		a.setConfigError(err)
		return
	}
	a.logger.Info("划词翻译开关已更新", logger.Bool("enabled", enabled))
}

func selectionShortcutLabel(value string) string {
	shortcut, err := hotkey.Parse(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	if runtime.GOOS == "darwin" {
		return shortcut.MacCanonical()
	}
	return shortcut.Canonical
}
