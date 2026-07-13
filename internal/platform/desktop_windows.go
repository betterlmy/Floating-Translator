//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
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
	windowMessageLeftButtonUp  = 0x0202
	windowMessageRightButtonUp = 0x0205
	trayNotificationSelect     = 0x0400
	windowMessageHotkey        = 0x0312
	windowMessageApp           = 0x8000
	trayCallbackMessage        = windowMessageApp + 1
	shutdownMessage            = windowMessageApp + 2
	configureHotkeyMessage     = windowMessageApp + 3

	trayIconID                 = 1
	trayCommandToggle          = 1001
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
	iconData  []byte

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

func (d *windowsDesktop) SetApplicationIcon(icon []byte) {
	d.stateMutex.Lock()
	d.iconData = append(d.iconData[:0], icon...)
	d.stateMutex.Unlock()
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
		switch notification {
		case windowMessageLeftButtonUp, trayNotificationSelect:
			invoke(host.callbacks.OnOpenSettings)
		case windowMessageRightButtonUp, windowMessageContextMenu:
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
