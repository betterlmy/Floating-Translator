package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"floating-translator/internal/config"
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
	"floating-translator/internal/processor"
)

func (a *App) startup(ctx context.Context) {
	a.initialized.Store(false)
	a.context = ctx
	if err := a.desktop.Start(ctx, a.desktopCallbacks()); err != nil {
		a.logger.Error("启动桌面集成失败", logger.ErrorField(err))
	}

	paths, created, err := a.preparePaths()
	if err != nil {
		a.setConfigError(fmt.Errorf("初始化应用目录失败: %w", err))
		return
	}
	a.paths = paths

	loadedConfig, configErr := config.LoadFile(paths.ConfigFile)
	loggingConfig := config.Default()
	if configErr == nil {
		loggingConfig = loadedConfig
	}
	appLogger, err := logger.New(
		paths.LogFile,
		loggingConfig.App.LogLevel,
		loggingConfig.Logging.MaxSizeMB,
		loggingConfig.Logging.MaxBackups,
	)
	if err == nil {
		a.logger = appLogger
	} else {
		a.logger.Error("初始化文件日志失败", logger.ErrorField(err))
	}
	if created {
		a.logger.Info("已生成首次运行配置模板", logger.String("config_path", paths.ConfigFile))
	}
	a.processor = processor.New(ctx, a.logger, a.emitTranslation)

	if configErr != nil {
		a.setConfigError(configErr)
	} else if err := a.installConfig(loadedConfig, false); err != nil {
		a.setConfigError(err)
	}
	a.logger.Info("应用启动完成",
		logger.Bool("config_valid", a.isConfigValid()),
		logger.String("config_path", paths.ConfigFile),
		logger.String("log_path", paths.LogFile),
	)
	// Native tray and hotkey callbacks may start during desktop.Start. Keep
	// them inert until paths, config, logger, and processor are all ready.
	a.initialized.Store(true)
}

func (a *App) desktopCallbacks() platform.Callbacks {
	return platform.Callbacks{
		OnClipboardText: func(text string) {
			a.runInitializedSafely("clipboard_text", func() {
				if a.processor != nil {
					a.processor.Handle(text)
				}
			})
		},
		OnSelectionTranslate: func() {
			a.runInitializedSafely("selection_translate", a.translateSelection)
		},
		OnToggleSelection: func() {
			a.runInitializedSafely("toggle_selection", a.toggleSelection)
		},
		OnToggleListening: func() {
			a.runInitializedSafely("toggle_listening", a.toggleListening)
		},
		OnOpenSettings: func() {
			a.runInitializedSafely("open_settings", a.showSettings)
		},
		OnOpenLogs: func() {
			a.runInitializedSafely("open_logs", func() { a.openPath(a.paths.LogDir) })
		},
		OnQuit: func() {
			a.runSafely("quit", func() {
				if a.application != nil {
					a.application.Quit()
				}
			})
		},
	}
}

func (a *App) openPath(path string) {
	if err := a.desktop.OpenPath(path); err != nil {
		a.logger.Error("打开路径失败", logger.String("path", path), logger.ErrorField(err))
	}
}

func (a *App) isConfigValid() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.configValid
}

func (a *App) shutdown() {
	a.initialized.Store(false)
	if a.processor != nil {
		a.processor.Stop()
	}
	if err := a.desktop.Stop(); err != nil {
		a.logger.Warn("停止桌面集成失败", logger.ErrorField(err))
	}
	if a.subtitle != nil {
		a.subtitle.Close()
	}
	a.logger.Info("应用已退出")
	_ = a.logger.Sync()
}

func (a *App) runSafely(name string, action func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			a.logger.Error("桌面回调发生 panic",
				logger.String("callback", name),
				logger.Any("panic", recovered),
				logger.String("stack", string(debug.Stack())),
			)
		}
	}()
	action()
}

func (a *App) runInitializedSafely(name string, action func()) {
	if !a.initialized.Load() {
		return
	}
	a.runSafely(name, action)
}
