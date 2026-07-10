package main

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"floating-translator/internal/config"
	"floating-translator/internal/filter"
	"floating-translator/internal/hotkey"
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
	"floating-translator/internal/processor"
	"floating-translator/internal/translator"
)

const (
	translationResultEvent = "translation:result"
	subtitleConfigEvent    = "subtitle:config"
	settingsRefreshEvent   = "settings:refresh"
	settingsWindowWidth    = 1080
	settingsWindowHeight   = 760
	subtitleHeightPercent  = 28
)

type workArea struct {
	X      int
	Y      int
	Width  int
	Height int
}

type windowBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

type applicationController interface {
	Quit()
	PrimaryWorkArea() (workArea, error)
}

type windowController interface {
	Show()
	Hide()
	Focus()
	SetAlwaysOnTop(enabled bool)
	SetSize(width int, height int)
	SetPosition(x int, y int)
	EmitEvent(name string, data any)
}

// subtitleController owns the platform-native subtitle surface. Keeping it
// separate from the Wails settings window prevents WebView hosting behaviour
// from affecting the transparency of the overlay.
type subtitleController interface {
	Configure(windowBounds, config.SubtitleConfig) error
	Display(processor.Event)
	Close()
}

// App 管理应用配置、翻译调度和桌面生命周期。
type App struct {
	mutex              sync.RWMutex
	configurationMutex sync.Mutex

	context        context.Context
	application    applicationController
	subtitle       subtitleController
	settingsWindow windowController
	logger         *logger.Logger
	desktop        platform.Desktop
	processor      *processor.Processor
	paths          config.Paths
	config         config.Config

	preparePaths func() (config.Paths, bool, error)

	configValid   bool
	frontendReady bool
	listening     bool
	settingsOpen  bool
}

func (a *App) setWindows(app applicationController, subtitle subtitleController, settingsWindow windowController) {
	a.application = app
	a.subtitle = subtitle
	a.settingsWindow = settingsWindow
}

// NewApp 创建应用实例。
func NewApp() *App {
	return NewAppWithIcon(nil)
}

func NewAppWithIcon(icon []byte) *App {
	desktop := platform.NewDesktop()
	desktop.SetApplicationIcon(icon)
	return &App{
		logger:       logger.NewNop(),
		desktop:      desktop,
		preparePaths: config.PreparePaths,
	}
}

func (a *App) startup(ctx context.Context) {
	a.context = ctx
	if err := a.desktop.Start(ctx, a.desktopCallbacks()); err != nil {
		a.logger.Error("启动 Windows 桌面集成失败", logger.ErrorField(err))
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
}

func (a *App) desktopCallbacks() platform.Callbacks {
	return platform.Callbacks{
		OnClipboardText: func(text string) {
			a.runSafely("clipboard_text", func() {
				if a.processor != nil {
					a.processor.Handle(text)
				}
			})
		},
		OnSelectionTranslate: func() {
			a.runSafely("selection_translate", a.translateSelection)
		},
		OnToggleSelection: func() {
			a.runSafely("toggle_selection", a.toggleSelection)
		},
		OnToggleListening: func() {
			a.runSafely("toggle_listening", a.toggleListening)
		},
		OnReloadConfig: func() {
			a.runSafely("reload_config", a.reloadConfig)
		},
		OnOpenSettings: func() {
			a.runSafely("open_settings", a.showSettings)
		},
		OnOpenConfig: func() {
			a.runSafely("open_config", func() { a.openPath(a.paths.ConfigFile) })
		},
		OnOpenLogs: func() {
			a.runSafely("open_logs", func() { a.openPath(a.paths.LogDir) })
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

func (a *App) reloadConfig() {
	a.configurationMutex.Lock()
	defer a.configurationMutex.Unlock()
	cfg, err := config.LoadFile(a.paths.ConfigFile)
	if err != nil {
		a.setConfigError(err)
		return
	}
	if err := a.installConfig(cfg, true); err != nil {
		a.setConfigError(err)
		return
	}
	a.logger.Info("配置重新加载成功")
}

func (a *App) installConfig(cfg config.Config, applyToFrontend bool) error {
	textTranslator, err := translator.NewEino(a.context, cfg.LLM)
	if err != nil {
		return err
	}
	if err := a.logger.SetLevel(cfg.App.LogLevel); err != nil {
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
	a.logger.Info("划词翻译快捷键已启用", logger.String("hotkey", shortcut.Canonical))
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

	text, err := a.readSelectedText(cfg)
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
	directContext, cancelDirect := context.WithTimeout(a.context, 3*time.Second)
	text, err := a.desktop.SelectedText(directContext, cfg.Clipboard.MaxTextLength+1)
	cancelDirect()
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
	return shortcut.Canonical
}

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

// SaveSettings 校验并保存设置，成功后立即应用新配置。
func (a *App) SaveSettings(settings config.Settings) error {
	a.configurationMutex.Lock()
	defer a.configurationMutex.Unlock()
	if err := config.SaveSettingsFile(a.paths.ConfigFile, settings); err != nil {
		return err
	}
	updatedConfig, err := config.LoadFile(a.paths.ConfigFile)
	if err != nil {
		return err
	}
	if err := a.installConfig(updatedConfig, true); err != nil {
		a.setConfigError(err)
		return err
	}
	a.logger.Info("设置已保存并应用")
	return nil
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

func (a *App) toggleListening() {
	a.mutex.Lock()
	if !a.configValid || !a.frontendReady {
		a.mutex.Unlock()
		return
	}
	a.listening = !a.listening
	listening := a.listening
	a.mutex.Unlock()
	a.setListening(listening)
	a.logger.Info("剪切板监听状态已更新", logger.Bool("enabled", listening))
}

func (a *App) setListening(enabled bool) {
	a.mutex.Lock()
	a.listening = enabled
	a.mutex.Unlock()
	a.processor.SetEnabled(enabled)
	a.desktop.SetListening(enabled)
	if enabled {
		a.desktop.SetTrayStatus(platform.TrayStatusRunning)
	} else {
		a.desktop.SetTrayStatus(platform.TrayStatusPaused)
	}
}

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
