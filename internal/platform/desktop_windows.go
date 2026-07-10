//go:build windows

package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"floating-translator/internal/hotkey"

	"golang.org/x/sys/windows"
)

const (
	clipboardFormatUnicodeText = 13
	windowMessageClipboard     = 0x031D
	windowMessageCommand       = 0x0111
	windowMessageDestroy       = 0x0002
	windowMessageContextMenu   = 0x007B
	windowMessageRightButtonUp = 0x0205
	windowMessageHotkey        = 0x0312
	windowMessageApp           = 0x8000
	trayCallbackMessage        = windowMessageApp + 1
	shutdownMessage            = windowMessageApp + 2
	configureHotkeyMessage     = windowMessageApp + 3

	trayIconID                 = 1
	trayCommandToggle          = 1001
	trayCommandReload          = 1002
	trayCommandConfig          = 1003
	trayCommandLogs            = 1004
	trayCommandQuit            = 1005
	trayCommandToggleSelection = 1006
	trayCommandSettings        = 1007
	selectionHotkeyID          = 2
	modifierNoRepeat           = 0x4000

	notifyIconAdd         = 0x00000000
	notifyIconDelete      = 0x00000002
	notifyIconSetVersion  = 0x00000004
	notifyIconFlagMessage = 0x00000001
	notifyIconFlagIcon    = 0x00000002
	notifyIconFlagTip     = 0x00000004
	notifyIconVersion4    = 4

	menuFlagString    = 0x00000000
	menuFlagGrayed    = 0x00000001
	menuFlagSeparator = 0x00000800
	trackMenuRight    = 0x00000002
	trackMenuReturn   = 0x00000100

	windowStyleTransparent = 0x00000020
	windowStyleToolWindow  = 0x00000080
	windowStyleAppWindow   = 0x00040000
	windowStyleLayered     = 0x00080000
	windowStyleNoActivate  = 0x08000000

	setWindowNoActivate   = 0x0010
	setWindowFrameChanged = 0x0020
	setWindowShow         = 0x0040

	systemParameterGetWorkArea = 0x0030
	showWindowNormal           = 1
	maxClipboardBytes          = 8 * 1024 * 1024
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procIsClipboardFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procLoadIconW                     = user32.NewProc("LoadIconW")
	procDestroyIcon                   = user32.NewProc("DestroyIcon")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procFindWindowW                   = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW             = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW             = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID      = user32.NewProc("GetWindowThreadProcessId")
	procGetGUIThreadInfo              = user32.NewProc("GetGUIThreadInfo")
	procGetClassNameW                 = user32.NewProc("GetClassNameW")
	procSendMessageTimeoutW           = user32.NewProc("SendMessageTimeoutW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procSetThreadDpiAwarenessContext  = user32.NewProc("SetThreadDpiAwarenessContext")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalSize       = kernel32.NewProc("GlobalSize")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
	procExtractIconExW   = shell32.NewProc("ExtractIconExW")

	messageWindowCallback = windows.NewCallback(messageWindowProcedure)
	hostRegistry          sync.Map
)

type point struct {
	X int32
	Y int32
}

type rectangle struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type windowMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type windowClassEx struct {
	Size        uint32
	Style       uint32
	WindowProc  uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
	SmallIcon   uintptr
}

type notifyIconData struct {
	Size            uint32
	Window          uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            windows.GUID
	BalloonIcon     uintptr
}

type windowsDesktop struct {
	context context.Context

	callbacks Callbacks
	window    uintptr
	instance  uintptr
	icon      uintptr
	iconOwned bool

	ready          chan error
	done           chan struct{}
	hotkeyCommands chan hotkeyCommand

	listening                atomic.Bool
	debounce                 atomic.Int64
	internalClipboard        atomic.Bool
	ignoredClipboardSequence atomic.Uint32

	stateMutex         sync.RWMutex
	trayStatus         TrayStatus
	hotkey             *hotkey.Shortcut
	selectionEnabled   bool
	selectionShortcut  string
	hotkeyRequestMutex sync.Mutex

	timerMutex         sync.Mutex
	timer              *time.Timer
	compatibilityMutex sync.Mutex

	cleanupOnce sync.Once
	stopOnce    sync.Once

	taskbarCreatedMessage uint32
}

type hotkeyCommand struct {
	shortcut *hotkey.Shortcut
	result   chan error
}

// NewDesktop 创建 Windows 桌面实现。
func NewDesktop() Desktop {
	host := &windowsDesktop{
		ready:          make(chan error, 1),
		done:           make(chan struct{}),
		hotkeyCommands: make(chan hotkeyCommand, 1),
		trayStatus:     TrayStatusConfigError,
	}
	host.debounce.Store(int64(300 * time.Millisecond))
	return host
}

func (d *windowsDesktop) Start(ctx context.Context, callbacks Callbacks) error {
	d.context = ctx
	d.callbacks = callbacks
	go d.runMessageLoop()
	select {
	case err := <-d.ready:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	go func() {
		<-ctx.Done()
		_ = d.Stop()
	}()
	return nil
}

func (d *windowsDesktop) SetListening(enabled bool) {
	d.listening.Store(enabled)
	if !enabled {
		d.stopDebounceTimer()
	}
}

func (d *windowsDesktop) SetDebounce(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	d.debounce.Store(int64(duration))
}

func (d *windowsDesktop) SetTrayStatus(status TrayStatus) {
	d.stateMutex.Lock()
	d.trayStatus = status
	d.stateMutex.Unlock()
}

func (d *windowsDesktop) SetSelectionStatus(enabled bool, shortcut string) {
	d.stateMutex.Lock()
	d.selectionEnabled = enabled
	d.selectionShortcut = shortcut
	d.stateMutex.Unlock()
}

func (d *windowsDesktop) SetSelectionHotkey(shortcut *hotkey.Shortcut) error {
	d.hotkeyRequestMutex.Lock()
	defer d.hotkeyRequestMutex.Unlock()
	var shortcutCopy *hotkey.Shortcut
	if shortcut != nil {
		value := *shortcut
		shortcutCopy = &value
	}
	result, _, callErr := procPostMessageW.Call(d.window, configureHotkeyMessage, 0, 0)
	if result == 0 {
		return fmt.Errorf("向 Windows 消息线程投递快捷键配置失败: %w", callErr)
	}
	command := hotkeyCommand{shortcut: shortcutCopy, result: make(chan error, 1)}
	d.hotkeyCommands <- command
	select {
	case err := <-command.result:
		return err
	case <-d.done:
		return errors.New("Windows 消息循环已退出")
	}
}

func (d *windowsDesktop) applySelectionHotkey(shortcut *hotkey.Shortcut) error {
	d.stateMutex.Lock()
	defer d.stateMutex.Unlock()
	if d.window == 0 {
		return errors.New("Windows 消息窗口尚未初始化")
	}
	if shortcutsEqual(d.hotkey, shortcut) {
		return nil
	}

	previous := d.hotkey
	if previous != nil {
		procUnregisterHotKey.Call(d.window, selectionHotkeyID)
		d.hotkey = nil
	}
	if shortcut == nil {
		return nil
	}
	if err := d.registerSelectionHotkey(*shortcut); err != nil {
		if previous != nil {
			if restoreErr := d.registerSelectionHotkey(*previous); restoreErr != nil {
				return fmt.Errorf("%w；恢复原快捷键也失败: %v", err, restoreErr)
			}
		}
		return err
	}
	return nil
}

func (d *windowsDesktop) registerSelectionHotkey(shortcut hotkey.Shortcut) error {
	result, _, callErr := procRegisterHotKey.Call(
		d.window,
		selectionHotkeyID,
		uintptr(shortcut.Modifiers)|modifierNoRepeat,
		uintptr(shortcut.VirtualKey),
	)
	if result == 0 {
		return fmt.Errorf("注册划词翻译快捷键 %s 失败，快捷键可能已被占用: %w", shortcut.Canonical, callErr)
	}
	stored := shortcut
	d.hotkey = &stored
	return nil
}

func shortcutsEqual(left *hotkey.Shortcut, right *hotkey.Shortcut) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Modifiers == right.Modifiers && left.VirtualKey == right.VirtualKey
}

func (d *windowsDesktop) ApplyOverlay(options OverlayOptions) error {
	handle, err := findWailsWindow(options.WindowClassName, options.WindowTitle)
	if err != nil {
		return err
	}

	index := ^uintptr(19) // GWL_EXSTYLE 在 Win32 API 中的值为 -20。
	style, _, _ := procGetWindowLongPtrW.Call(handle, index)
	style = overlayExtendedStyle(style)
	previousStyle, _, callErr := procSetWindowLongPtrW.Call(handle, index, style)
	if previousStyle == 0 && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("设置字幕窗口扩展样式失败: %w", callErr)
	}

	var workArea rectangle
	result, _, callErr := procSystemParametersInfoW.Call(
		systemParameterGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)
	if result == 0 {
		return fmt.Errorf("读取主屏幕工作区失败: %w", callErr)
	}
	workWidth := int(workArea.Right - workArea.Left)
	workHeight := int(workArea.Bottom - workArea.Top)
	width := workWidth * options.WidthPercent / 100
	height := workHeight * 28 / 100
	x := int(workArea.Left) + (workWidth-width)/2
	y := int(workArea.Bottom) - height - workHeight*options.BottomOffsetPercent/100

	result, _, callErr = procSetWindowPos.Call(
		handle,
		topMostWindowHandle(),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		setWindowNoActivate|setWindowFrameChanged|setWindowShow,
	)
	if result == 0 {
		return fmt.Errorf("设置字幕窗口样式失败: %w", callErr)
	}
	return nil
}

func (d *windowsDesktop) ApplySettingsWindow(options WindowOptions) error {
	previousDPIContext, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorAwareV2Context())
	if previousDPIContext != 0 {
		defer procSetThreadDpiAwarenessContext.Call(previousDPIContext)
	}
	handle, err := findWailsWindow(options.WindowClassName, options.WindowTitle)
	if err != nil {
		return err
	}
	index := ^uintptr(19)
	style, _, _ := procGetWindowLongPtrW.Call(handle, index)
	style = settingsExtendedStyle(style)
	previousStyle, _, callErr := procSetWindowLongPtrW.Call(handle, index, style)
	if previousStyle == 0 && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("设置设置窗口扩展样式失败: %w", callErr)
	}
	var workArea rectangle
	result, _, callErr := procSystemParametersInfoW.Call(
		systemParameterGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)
	if result == 0 {
		return fmt.Errorf("读取主屏幕工作区失败: %w", callErr)
	}
	dpi, _, _ := procGetDpiForWindow.Call(handle)
	if dpi == 0 {
		dpi = 96
	}
	bounds := settingsWindowBounds(options.Width, options.Height, int(dpi), workArea)
	result, _, callErr = procSetWindowPos.Call(
		handle,
		topMostWindowHandle(),
		uintptr(bounds.Left),
		uintptr(bounds.Top),
		uintptr(bounds.Right-bounds.Left),
		uintptr(bounds.Bottom-bounds.Top),
		setWindowFrameChanged|setWindowShow,
	)
	if result == 0 {
		return fmt.Errorf("刷新设置窗口样式失败: %w", callErr)
	}
	procSetForegroundWindow.Call(handle)
	return nil
}

func settingsWindowBounds(width int, height int, dpi int, workArea rectangle) rectangle {
	if dpi <= 0 {
		dpi = 96
	}
	workWidth := int(workArea.Right - workArea.Left)
	workHeight := int(workArea.Bottom - workArea.Top)
	scaledWidth := min(width*dpi/96, workWidth*92/100)
	scaledHeight := min(height*dpi/96, workHeight*92/100)
	left := int(workArea.Left) + (workWidth-scaledWidth)/2
	top := int(workArea.Top) + (workHeight-scaledHeight)/2
	return rectangle{
		Left:   int32(left),
		Top:    int32(top),
		Right:  int32(left + scaledWidth),
		Bottom: int32(top + scaledHeight),
	}
}

func findWailsWindow(classNameValue string, titleValue string) (uintptr, error) {
	className, err := windows.UTF16PtrFromString(classNameValue)
	if err != nil {
		return 0, fmt.Errorf("转换窗口类名失败: %w", err)
	}
	title, err := windows.UTF16PtrFromString(titleValue)
	if err != nil {
		return 0, fmt.Errorf("转换窗口标题失败: %w", err)
	}
	handle, _, callErr := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)))
	if handle == 0 {
		return 0, fmt.Errorf("查找 Wails 窗口失败: %w", callErr)
	}
	return handle, nil
}

func overlayExtendedStyle(style uintptr) uintptr {
	style |= windowStyleTransparent | windowStyleToolWindow | windowStyleLayered | windowStyleNoActivate
	return style &^ windowStyleAppWindow
}

func settingsExtendedStyle(style uintptr) uintptr {
	style &^= windowStyleTransparent | windowStyleToolWindow | windowStyleNoActivate
	return style | windowStyleAppWindow
}

func (d *windowsDesktop) OpenPath(path string) error {
	operation, _ := windows.UTF16PtrFromString("open")
	filePath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("转换打开路径失败: %w", err)
	}
	result, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(operation)),
		uintptr(unsafe.Pointer(filePath)),
		0,
		0,
		showWindowNormal,
	)
	if result <= 32 {
		return fmt.Errorf("打开路径失败，返回码 %d: %w", result, callErr)
	}
	return nil
}

func (d *windowsDesktop) Stop() error {
	d.stopOnce.Do(func() {
		d.stopDebounceTimer()
		if d.window != 0 {
			procPostMessageW.Call(d.window, shutdownMessage, 0, 0)
		}
	})
	select {
	case <-d.done:
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("等待 Windows 消息循环退出超时")
	}
}

func (d *windowsDesktop) runMessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.done)

	if err := d.createMessageWindow(); err != nil {
		d.cleanup()
		if d.window != 0 {
			procDestroyWindow.Call(d.window)
			hostRegistry.Delete(d.window)
		}
		d.ready <- err
		return
	}
	d.ready <- nil

	var message windowMessage
	for {
		result, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			_ = callErr
			break
		}
		if result == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
	d.cleanup()
	if d.window != 0 {
		hostRegistry.Delete(d.window)
	}
}

func (d *windowsDesktop) createMessageWindow() error {
	instance, _, callErr := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return fmt.Errorf("获取应用模块句柄失败: %w", callErr)
	}
	d.instance = instance
	className, _ := windows.UTF16PtrFromString("FloatingTranslatorMessageWindow")
	windowName, _ := windows.UTF16PtrFromString("悬浮翻译器消息窗口")
	class := windowClassEx{
		Size:       uint32(unsafe.Sizeof(windowClassEx{})),
		WindowProc: messageWindowCallback,
		Instance:   instance,
		ClassName:  className,
	}
	registered, _, registerErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if registered == 0 && !errors.Is(registerErr, windows.ERROR_CLASS_ALREADY_EXISTS) {
		return fmt.Errorf("注册消息窗口类失败: %w", registerErr)
	}

	handle, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0,
		0,
		0,
		0,
		messageOnlyWindowHandle(),
		0,
		instance,
		0,
	)
	if handle == 0 {
		return fmt.Errorf("创建消息窗口失败: %w", callErr)
	}
	d.window = handle
	hostRegistry.Store(handle, d)

	taskbarCreated, _ := windows.UTF16PtrFromString("TaskbarCreated")
	messageID, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarCreated)))
	d.taskbarCreatedMessage = uint32(messageID)

	result, _, callErr := procAddClipboardFormatListener.Call(handle)
	if result == 0 {
		return fmt.Errorf("注册剪切板监听失败: %w", callErr)
	}
	if err := d.addTrayIcon(); err != nil {
		procRemoveClipboardFormatListener.Call(handle)
		return err
	}
	return nil
}

func (d *windowsDesktop) addTrayIcon() error {
	if d.icon == 0 {
		d.icon, d.iconOwned = loadExecutableIcon()
	}
	if d.icon == 0 {
		return errors.New("加载应用托盘图标失败")
	}
	data := d.newNotifyIconData()
	result, _, callErr := procShellNotifyIconW.Call(notifyIconAdd, uintptr(unsafe.Pointer(&data)))
	if result == 0 {
		return fmt.Errorf("创建系统托盘图标失败: %w", callErr)
	}
	data.Version = notifyIconVersion4
	procShellNotifyIconW.Call(notifyIconSetVersion, uintptr(unsafe.Pointer(&data)))
	return nil
}

func loadExecutableIcon() (uintptr, bool) {
	executablePath, err := os.Executable()
	if err == nil {
		path, pathErr := windows.UTF16PtrFromString(executablePath)
		if pathErr == nil {
			var smallIcon uintptr
			count, _, _ := procExtractIconExW.Call(
				uintptr(unsafe.Pointer(path)),
				0,
				0,
				uintptr(unsafe.Pointer(&smallIcon)),
				1,
			)
			if count > 0 && smallIcon != 0 {
				return smallIcon, true
			}
		}
	}
	icon, _, _ := procLoadIconW.Call(0, 32512)
	return icon, false
}

func (d *windowsDesktop) newNotifyIconData() notifyIconData {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Window:          d.window,
		ID:              trayIconID,
		Flags:           notifyIconFlagMessage | notifyIconFlagIcon | notifyIconFlagTip,
		CallbackMessage: trayCallbackMessage,
		Icon:            d.icon,
	}
	copy(data.Tip[:], windows.StringToUTF16("悬浮翻译器"))
	return data
}

func (d *windowsDesktop) showTrayMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	d.stateMutex.RLock()
	status := d.trayStatus
	selectionEnabled := d.selectionEnabled
	selectionShortcut := d.selectionShortcut
	d.stateMutex.RUnlock()
	statusLabel := "状态：配置错误"
	switch status {
	case TrayStatusRunning:
		statusLabel = "状态：正在监听"
	case TrayStatusPaused:
		statusLabel = "状态：已暂停"
	}
	d.appendMenu(menu, menuFlagString|menuFlagGrayed, 0, statusLabel)
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	if status != TrayStatusConfigError {
		label := "暂停监听"
		if !d.listening.Load() {
			label = "恢复监听"
		}
		d.appendMenu(menu, menuFlagString, trayCommandToggle, label)
	}
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	if selectionShortcut == "" {
		selectionShortcut = "未配置"
	}
	d.appendMenu(menu, menuFlagString|menuFlagGrayed, 0, "划词翻译快捷键："+selectionShortcut)
	selectionLabel := "开启划词翻译"
	if selectionEnabled {
		selectionLabel = "关闭划词翻译"
	}
	selectionFlags := uintptr(menuFlagString)
	if status == TrayStatusConfigError {
		selectionFlags |= menuFlagGrayed
	}
	d.appendMenu(menu, selectionFlags, trayCommandToggleSelection, selectionLabel)
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	d.appendMenu(menu, menuFlagString, trayCommandSettings, "设置…")
	d.appendMenu(menu, menuFlagString, trayCommandReload, "重新加载配置")
	d.appendMenu(menu, menuFlagString, trayCommandConfig, "打开配置")
	d.appendMenu(menu, menuFlagString, trayCommandLogs, "打开日志目录")
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	d.appendMenu(menu, menuFlagString, trayCommandQuit, "退出")

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(d.window)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		trackMenuRight|trackMenuReturn,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		d.window,
		0,
	)
	if command != 0 {
		d.handleTrayCommand(command)
	}
}

func (d *windowsDesktop) appendMenu(menu uintptr, flags uintptr, id uintptr, label string) {
	if flags&menuFlagSeparator != 0 {
		procAppendMenuW.Call(menu, flags, id, 0)
		return
	}
	text, _ := windows.UTF16PtrFromString(label)
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
}

func (d *windowsDesktop) handleTrayCommand(command uintptr) {
	switch command {
	case trayCommandToggle:
		invoke(d.callbacks.OnToggleListening)
	case trayCommandToggleSelection:
		invoke(d.callbacks.OnToggleSelection)
	case trayCommandSettings:
		invoke(d.callbacks.OnOpenSettings)
	case trayCommandReload:
		invoke(d.callbacks.OnReloadConfig)
	case trayCommandConfig:
		invoke(d.callbacks.OnOpenConfig)
	case trayCommandLogs:
		invoke(d.callbacks.OnOpenLogs)
	case trayCommandQuit:
		invoke(d.callbacks.OnQuit)
	}
}

func (d *windowsDesktop) onClipboardUpdate() {
	if d.internalClipboard.Load() {
		return
	}
	sequence := clipboardSequenceNumber()
	ignoredSequence := d.ignoredClipboardSequence.Load()
	if ignoredSequence != 0 {
		if sequence == ignoredSequence {
			return
		}
		d.ignoredClipboardSequence.CompareAndSwap(ignoredSequence, 0)
	}
	if !d.listening.Load() {
		return
	}
	d.timerMutex.Lock()
	defer d.timerMutex.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
	delay := time.Duration(d.debounce.Load())
	d.timer = time.AfterFunc(delay, func() {
		text, ok, err := d.readClipboardWithRetry()
		if err != nil || !ok || !d.listening.Load() {
			return
		}
		if d.callbacks.OnClipboardText != nil {
			d.callbacks.OnClipboardText(text)
		}
	})
}

func (d *windowsDesktop) readClipboardWithRetry() (string, bool, error) {
	delays := []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	var lastErr error
	for _, delay := range delays {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-d.context.Done():
				return "", false, d.context.Err()
			}
		}
		text, available, err := d.readClipboard()
		if err == nil {
			return text, available, nil
		}
		lastErr = err
	}
	return "", false, lastErr
}

func (d *windowsDesktop) readClipboard() (string, bool, error) {
	available, _, _ := procIsClipboardFormatAvailable.Call(clipboardFormatUnicodeText)
	if available == 0 {
		return "", false, nil
	}
	opened, _, callErr := procOpenClipboard.Call(d.window)
	if opened == 0 {
		return "", false, fmt.Errorf("打开剪切板失败: %w", callErr)
	}
	defer procCloseClipboard.Call()

	handle, _, callErr := procGetClipboardData.Call(clipboardFormatUnicodeText)
	if handle == 0 {
		return "", false, fmt.Errorf("读取剪切板文本句柄失败: %w", callErr)
	}
	data, _, callErr := procGlobalLock.Call(handle)
	if data == 0 {
		return "", false, fmt.Errorf("锁定剪切板文本失败: %w", callErr)
	}
	defer procGlobalUnlock.Call(handle)
	size, _, callErr := procGlobalSize.Call(handle)
	if size == 0 {
		return "", false, fmt.Errorf("读取剪切板文本大小失败: %w", callErr)
	}
	if size > maxClipboardBytes {
		return "", false, fmt.Errorf("剪切板文本超过安全读取上限: %d bytes", size)
	}
	buffer := make([]byte, size)
	var bytesRead uintptr
	if err := windows.ReadProcessMemory(
		windows.CurrentProcess(),
		data,
		&buffer[0],
		size,
		&bytesRead,
	); err != nil {
		return "", false, fmt.Errorf("复制剪切板文本失败: %w", err)
	}
	return decodeUTF16LE(buffer, bytesRead), true, nil
}

func decodeUTF16LE(buffer []byte, length uintptr) string {
	if length > uintptr(len(buffer)) {
		length = uintptr(len(buffer))
	}
	codeUnits := make([]uint16, 0, length/2)
	for index := uintptr(0); index+1 < length; index += 2 {
		value := binary.LittleEndian.Uint16(buffer[index : index+2])
		if value == 0 {
			break
		}
		codeUnits = append(codeUnits, value)
	}
	return string(utf16.Decode(codeUnits))
}

func (d *windowsDesktop) stopDebounceTimer() {
	d.timerMutex.Lock()
	defer d.timerMutex.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func (d *windowsDesktop) cleanup() {
	d.cleanupOnce.Do(func() {
		d.stopDebounceTimer()
		if d.window != 0 {
			d.stateMutex.Lock()
			if d.hotkey != nil {
				procUnregisterHotKey.Call(d.window, selectionHotkeyID)
				d.hotkey = nil
			}
			d.stateMutex.Unlock()
			procRemoveClipboardFormatListener.Call(d.window)
			data := d.newNotifyIconData()
			procShellNotifyIconW.Call(notifyIconDelete, uintptr(unsafe.Pointer(&data)))
		}
		if d.iconOwned && d.icon != 0 {
			procDestroyIcon.Call(d.icon)
		}
		d.icon = 0
		d.iconOwned = false
	})
}

func messageWindowProcedure(window uintptr, message uint32, wParam uintptr, lParam uintptr) uintptr {
	value, found := hostRegistry.Load(window)
	if !found {
		result, _, _ := procDefWindowProcW.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	host := value.(*windowsDesktop)
	switch {
	case message == windowMessageClipboard:
		host.onClipboardUpdate()
		return 0
	case message == windowMessageHotkey && wParam == selectionHotkeyID:
		invoke(host.callbacks.OnSelectionTranslate)
		return 0
	case message == configureHotkeyMessage:
		command := <-host.hotkeyCommands
		command.result <- host.applySelectionHotkey(command.shortcut)
		return 0
	case message == trayCallbackMessage:
		notification := uint32(lParam & 0xFFFF)
		if notification == windowMessageRightButtonUp || notification == windowMessageContextMenu {
			host.showTrayMenu()
		}
		return 0
	case message == windowMessageCommand:
		host.handleTrayCommand(wParam & 0xFFFF)
		return 0
	case host.taskbarCreatedMessage != 0 && message == host.taskbarCreatedMessage:
		_ = host.addTrayIcon()
		return 0
	case message == shutdownMessage:
		host.cleanup()
		procDestroyWindow.Call(window)
		return 0
	case message == windowMessageDestroy:
		procPostQuitMessage.Call(0)
		return 0
	default:
		result, _, _ := procDefWindowProcW.Call(window, uintptr(message), wParam, lParam)
		return result
	}
}

func invoke(callback func()) {
	if callback != nil {
		go callback()
	}
}

func messageOnlyWindowHandle() uintptr { return ^uintptr(2) }

func topMostWindowHandle() uintptr { return ^uintptr(0) }

func perMonitorAwareV2Context() uintptr { return ^uintptr(3) }
