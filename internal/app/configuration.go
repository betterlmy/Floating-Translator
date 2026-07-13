package app

import (
	"time"

	"floating-translator/internal/config"
	"floating-translator/internal/filter"
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
	"floating-translator/internal/translator"
)

// FrontendReady 表示 Vue 字幕组件已完成事件订阅，可以开始监听剪切板。
func (a *App) FrontendReady() error {
	a.mutex.Lock()
	a.frontendReady = true
	cfg := a.config
	valid := a.configValid
	a.mutex.Unlock()

	if valid {
		if err := a.applySelectionHotkey(cfg.Selection); err != nil {
			a.setConfigError(err)
			return err
		}
		if err := a.applySubtitleConfig(cfg.Subtitle); err != nil {
			return err
		}
		listening := cfg.Clipboard.Enable
		a.setListening(listening)
	}
	return nil
}

func (a *App) installConfig(cfg config.Config, applyToFrontend bool) error {
	textTranslator, err := translator.NewEino(a.context, cfg.LLM)
	if err != nil {
		return err
	}
	if err := a.logger.SetLevel(cfg.App.LogLevel); err != nil {
		return err
	}
	if err := a.logger.Reconfigure(cfg.Logging.MaxSizeMB, cfg.Logging.MaxBackups); err != nil {
		return err
	}

	a.mutex.RLock()
	frontendReady := a.frontendReady
	a.mutex.RUnlock()
	if frontendReady {
		if err := a.applySelectionHotkey(cfg.Selection); err != nil {
			return err
		}
	}

	a.mutex.Lock()
	a.config = cfg
	a.configValid = true
	a.listening = frontendReady && cfg.Clipboard.Enable
	listening := a.listening
	a.mutex.Unlock()

	a.processor.Configure(
		filter.New(cfg.Clipboard),
		textTranslator,
		time.Duration(cfg.LLM.TimeoutSeconds)*time.Second,
		cfg.Logging.IncludeSourceText,
		listening,
	)
	a.desktop.SetDebounce(time.Duration(cfg.Clipboard.DebounceMS) * time.Millisecond)
	a.desktop.SetListening(listening)
	a.desktop.SetSelectionStatus(cfg.Selection.Enable, selectionShortcutLabel(cfg.Selection.Hotkey))
	if listening {
		a.desktop.SetTrayStatus(platform.TrayStatusRunning)
	} else {
		a.desktop.SetTrayStatus(platform.TrayStatusPaused)
	}

	if applyToFrontend && frontendReady {
		return a.applySubtitleConfig(cfg.Subtitle)
	}
	return nil
}

func (a *App) setConfigError(err error) {
	a.mutex.Lock()
	a.configValid = false
	a.listening = false
	selectionShortcut := a.config.Selection.Hotkey
	a.mutex.Unlock()
	if a.processor != nil {
		a.processor.SetEnabled(false)
	}
	_ = a.desktop.SetSelectionHotkey(nil)
	a.desktop.SetSelectionStatus(false, selectionShortcutLabel(selectionShortcut))
	a.desktop.SetListening(false)
	a.desktop.SetTrayStatus(platform.TrayStatusConfigError)
	a.logger.Error("配置无效，翻译功能已禁用", logger.ErrorField(err))
}
