package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"floating-translator/internal/config"
	"floating-translator/internal/hotkey"
	"floating-translator/internal/platform"
	"floating-translator/internal/processor"
)

type startupDesktop struct {
	events                  *[]string
	started                 bool
	listening               bool
	status                  platform.TrayStatus
	selectedText            string
	selectedTextErr         error
	compatibleText          string
	compatibleTextErr       error
	compatibleSelectedCalls int
	callbacks               platform.Callbacks
	openPathCalls           int
}

type blockingSelectedDesktop struct {
	*startupDesktop
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockingSelectedDesktop) SelectedText(context.Context, int) (string, error) {
	d.once.Do(func() { close(d.entered) })
	<-d.release
	return "selected text", nil
}

type testApplicationController struct {
	area    workArea
	areaErr error
	quit    bool
}

func (c *testApplicationController) Quit() {
	c.quit = true
}

func (c *testApplicationController) PrimaryWorkArea() (workArea, error) {
	return c.area, c.areaErr
}

type testWindowController struct {
	shown       bool
	hidden      bool
	focused     bool
	alwaysOnTop bool
	width       int
	height      int
	x           int
	y           int
	events      []string
	subtitleCfg config.SubtitleConfig
	translation processor.Event
	closed      bool
}

func (c *testWindowController) Show()                         { c.shown = true }
func (c *testWindowController) Hide()                         { c.hidden = true }
func (c *testWindowController) Focus()                        { c.focused = true }
func (c *testWindowController) SetAlwaysOnTop(enabled bool)   { c.alwaysOnTop = enabled }
func (c *testWindowController) SetSize(width int, height int) { c.width, c.height = width, height }
func (c *testWindowController) SetPosition(x int, y int)      { c.x, c.y = x, y }
func (c *testWindowController) EmitEvent(name string, _ any)  { c.events = append(c.events, name) }
func (c *testWindowController) Configure(bounds windowBounds, cfg config.SubtitleConfig) error {
	c.width, c.height = bounds.Width, bounds.Height
	c.x, c.y = bounds.X, bounds.Y
	c.subtitleCfg = cfg
	return nil
}
func (c *testWindowController) Display(event processor.Event) { c.translation = event }
func (c *testWindowController) Close()                        { c.closed = true }

func (d *startupDesktop) Start(_ context.Context, callbacks platform.Callbacks) error {
	*d.events = append(*d.events, "desktop_start")
	d.callbacks = callbacks
	d.started = true
	return nil
}

func (d *startupDesktop) SetApplicationIcon([]byte) {}

func (d *startupDesktop) SetListening(enabled bool) {
	d.listening = enabled
}

func (d *startupDesktop) SetDebounce(time.Duration) {}

func (d *startupDesktop) SetTrayStatus(status platform.TrayStatus) {
	d.status = status
}

func (d *startupDesktop) SetSelectionStatus(bool, string) {}

func (d *startupDesktop) SetSelectionHotkey(*hotkey.Shortcut) error { return nil }

func (d *startupDesktop) SelectedText(context.Context, int) (string, error) {
	if d.selectedTextErr != nil {
		return "", d.selectedTextErr
	}
	return d.selectedText, nil
}

func (d *startupDesktop) CompatibleSelectedText(context.Context, int) (string, error) {
	d.compatibleSelectedCalls++
	return d.compatibleText, d.compatibleTextErr
}

func (d *startupDesktop) ApplyOverlay(platform.OverlayOptions) error { return nil }

func (d *startupDesktop) ApplySettingsWindow(platform.WindowOptions) error { return nil }

func (d *startupDesktop) OpenPath(string) error {
	d.openPathCalls++
	return nil
}

func (d *startupDesktop) Stop() error { return nil }

func TestStartupStartsTrayBeforeConfigFailure(t *testing.T) {
	events := make([]string, 0, 2)
	desktop := &startupDesktop{events: &events}
	app := NewApp()
	app.desktop = desktop
	app.preparePaths = func() (config.Paths, bool, error) {
		events = append(events, "prepare_paths")
		// desktop.Start happens before path/config initialization, so every
		// user callback must be harmless during this window.
		desktop.callbacks.OnClipboardText("early clipboard text")
		desktop.callbacks.OnSelectionTranslate()
		desktop.callbacks.OnReloadConfig()
		desktop.callbacks.OnOpenConfig()
		desktop.callbacks.OnOpenLogs()
		return config.Paths{}, false, errors.New("模拟配置目录错误")
	}

	app.startup(context.Background())

	if !desktop.started {
		t.Fatal("配置失败前应启动桌面托盘")
	}
	if got, want := events, []string{"desktop_start", "prepare_paths"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("启动顺序 = %v，want %v", got, want)
	}
	if desktop.status != platform.TrayStatusConfigError {
		t.Fatalf("托盘状态 = %q，want %q", desktop.status, platform.TrayStatusConfigError)
	}
	if desktop.listening {
		t.Fatal("配置失败时不应启用剪切板监听")
	}
	if desktop.openPathCalls != 0 {
		t.Fatalf("初始化完成前不应打开路径，调用次数 = %d", desktop.openPathCalls)
	}
	if app.isConfigValid() {
		t.Fatal("配置失败时不应标记为有效")
	}
}

func TestReadSelectedTextFallsBackToCompatibilityMode(t *testing.T) {
	desktop := &startupDesktop{
		selectedTextErr: platform.ErrSelectionUnsupported,
		compatibleText:  "selected from IDE",
	}
	app := NewApp()
	app.context = context.Background()
	app.desktop = desktop
	cfg := config.Default()
	cfg.Selection.CompatibilityMode = true

	text, err := app.readSelectedText(cfg)
	if err != nil {
		t.Fatalf("readSelectedText() error = %v", err)
	}
	if text != "selected from IDE" {
		t.Fatalf("readSelectedText() = %q", text)
	}
	if desktop.compatibleSelectedCalls != 1 {
		t.Fatalf("CompatibleSelectedText() 调用次数 = %d, want 1", desktop.compatibleSelectedCalls)
	}
}

func TestReadSelectedTextDoesNotUseCompatibilityModeWhenDisabled(t *testing.T) {
	desktop := &startupDesktop{selectedTextErr: platform.ErrSelectionUnsupported}
	app := NewApp()
	app.context = context.Background()
	app.desktop = desktop
	cfg := config.Default()

	_, err := app.readSelectedText(cfg)
	if !errors.Is(err, platform.ErrSelectionUnsupported) {
		t.Fatalf("readSelectedText() error = %v, want ErrSelectionUnsupported", err)
	}
	if desktop.compatibleSelectedCalls != 0 {
		t.Fatalf("CompatibleSelectedText() 调用次数 = %d, want 0", desktop.compatibleSelectedCalls)
	}
}

func TestReadSelectedTextRejectsConcurrentBlockedRead(t *testing.T) {
	base := &startupDesktop{}
	desktop := &blockingSelectedDesktop{
		startupDesktop: base,
		entered:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	app := NewApp()
	app.context = context.Background()
	app.desktop = desktop
	cfg := config.Default()

	firstResult := make(chan error, 1)
	go func() {
		_, err := app.readSelectedText(cfg)
		firstResult <- err
	}()
	select {
	case <-desktop.entered:
	case <-time.After(time.Second):
		t.Fatal("第一次选区读取未开始")
	}

	if _, err := app.readSelectedText(cfg); !errors.Is(err, platform.ErrSelectionBusy) {
		t.Fatalf("并发 readSelectedText() error = %v, want ErrSelectionBusy", err)
	}
	close(desktop.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("第一次 readSelectedText() error = %v", err)
	}
}

func TestCalculateSubtitleWindowBounds(t *testing.T) {
	cfg := config.Default().Subtitle
	area := workArea{X: 120, Y: 40, Width: 3200, Height: 2000}

	bounds, err := calculateSubtitleWindowBounds(cfg, area)
	if err != nil {
		t.Fatalf("calculateSubtitleWindowBounds() error = %v", err)
	}
	if bounds.Width != 2240 || bounds.Height != 560 {
		t.Fatalf("窗口尺寸 = %dx%d, want 2240x560", bounds.Width, bounds.Height)
	}
	if bounds.X != 600 || bounds.Y != 1400 {
		t.Fatalf("窗口位置 = (%d,%d), want (600,1400)", bounds.X, bounds.Y)
	}
}

func TestApplySubtitleConfigUpdatesNativeSubtitleBounds(t *testing.T) {
	appController := &testApplicationController{area: workArea{Width: 2000, Height: 1000}}
	window := &testWindowController{}
	app := NewApp()
	app.application = appController
	app.subtitle = window
	cfg := config.Default().Subtitle
	cfg.WidthPercent = 60
	cfg.BottomOffsetPercent = 10

	if err := app.applySubtitleConfig(cfg); err != nil {
		t.Fatalf("applySubtitleConfig() error = %v", err)
	}
	if window.width != 1200 || window.height != 280 || window.x != 400 || window.y != 620 {
		t.Fatalf("字幕窗口布局 = size(%d,%d) position(%d,%d)", window.width, window.height, window.x, window.y)
	}
	if window.subtitleCfg != cfg {
		t.Fatalf("字幕配置 = %#v, want %#v", window.subtitleCfg, cfg)
	}
}

func TestEmitTranslationDisplaysOnlyWhenFrontendIsReady(t *testing.T) {
	window := &testWindowController{}
	app := NewApp()
	app.subtitle = window
	event := processor.Event{RequestID: 7, Text: "translated text"}

	app.emitTranslation(event)
	if window.translation.RequestID != 0 {
		t.Fatalf("前端未就绪时不应展示字幕: %#v", window.translation)
	}

	app.frontendReady = true
	app.emitTranslation(event)
	if window.translation != event {
		t.Fatalf("字幕事件 = %#v, want %#v", window.translation, event)
	}
}

func TestShowSettingsRefreshesPersistentWindow(t *testing.T) {
	window := &testWindowController{}
	app := NewApp()
	app.settingsWindow = window

	app.showSettings()

	if !window.shown || !window.focused {
		t.Fatalf("设置窗口状态 shown=%t focused=%t", window.shown, window.focused)
	}
	if len(window.events) != 1 || window.events[0] != settingsRefreshEvent {
		t.Fatalf("窗口事件 = %v, want [%s]", window.events, settingsRefreshEvent)
	}
	app.mutex.RLock()
	settingsOpen := app.settingsOpen
	app.mutex.RUnlock()
	if !settingsOpen {
		t.Fatal("settingsOpen = false, want true")
	}

	app.showSettings()
	if len(window.events) != 1 {
		t.Fatalf("窗口已打开时不应重复刷新，事件 = %v", window.events)
	}
}
