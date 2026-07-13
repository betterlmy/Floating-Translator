//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework ApplicationServices -framework Carbon
#include <stdint.h>
#include <stdlib.h>

void macosStartTray(void);
void macosStopTray(void);
void macosSetTrayState(int status, int listening, int selectionEnabled,
                       const char *shortcut);
void macosSetSelectionHotkey(uint32_t modifiers, uint32_t key, int enabled);
int macosRequestAccessibilityPermission(void);
int64_t macosClipboardChangeCount(void);
char *macosReadClipboard(void);
void macosFreeString(char *value);
void *macosClipboardSnapshotCreate(void);
void macosClipboardSnapshotRelease(void *value);
int macosClipboardSnapshotRestore(void *value);
int macosWaitForModifierKeysReleased(int timeoutMilliseconds);
int macosSendCopyShortcut(void);
int macosAccessibilityTrusted(void);
void macosOpenAccessibilitySettings(void);
char *macosReadSelectedText(void);
int macosCursorPosition(double *x, double *y);
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	"floating-translator/internal/hotkey"
)

const (
	macEventSelectionTranslate = 1
	macEventToggleSelection    = 2
	macEventToggleListening    = 3
	macEventOpenSettings       = 5
	macEventOpenLogs           = 7
	macEventQuit               = 8

	macTrayStatusRunning     = 0
	macTrayStatusPaused      = 1
	macTrayStatusConfigError = 2

	macClipboardPollInterval = 100 * time.Millisecond
	macClipboardReadLimit    = 8 * 1024 * 1024
)

var activeDarwinDesktop struct {
	sync.RWMutex
	desktop *darwinDesktop
}

type darwinDesktop struct {
	context   context.Context
	callbacks Callbacks
	done      chan struct{}

	listening         atomic.Bool
	debounce          atomic.Int64
	lastClipboard     atomic.Int64
	internalClipboard atomic.Bool
	ignoredSequence   atomic.Int64

	timerMutex sync.Mutex
	timer      *time.Timer

	stateMutex         sync.RWMutex
	trayStatus         TrayStatus
	selectionEnabled   bool
	selectionShortcut  string
	hotkey             *hotkey.Shortcut
	hotkeyRequestMutex sync.Mutex
	compatibilityMutex sync.Mutex

	stopOnce sync.Once
}

// NewDesktop creates the macOS desktop integration.
func NewDesktop() Desktop {
	desktop := &darwinDesktop{
		done:       make(chan struct{}),
		trayStatus: TrayStatusConfigError,
	}
	desktop.debounce.Store(int64(300 * time.Millisecond))
	return desktop
}

func (d *darwinDesktop) SetApplicationIcon(_ []byte) {
	// macOS uses the application bundle icon for the Dock. The status item
	// intentionally uses a text label so it remains visible in light and dark
	// menu bars without requiring a separate template image asset.
}

func (d *darwinDesktop) Start(ctx context.Context, callbacks Callbacks) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d.context = ctx
	d.callbacks = callbacks
	d.lastClipboard.Store(int64(C.macosClipboardChangeCount()))

	activeDarwinDesktop.Lock()
	activeDarwinDesktop.desktop = d
	activeDarwinDesktop.Unlock()

	C.macosStartTray()
	// Ask macOS to show its first-run Accessibility permission prompt. The
	// selection hotkey and compatibility copy path both depend on this grant.
	C.macosRequestAccessibilityPermission()
	go d.monitorClipboard()
	go func() {
		<-ctx.Done()
		_ = d.Stop()
	}()
	return nil
}

func (d *darwinDesktop) SetListening(enabled bool) {
	d.listening.Store(enabled)
	if !enabled {
		d.stopDebounceTimer()
	}
	d.updateTray()
}

func (d *darwinDesktop) SetDebounce(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	d.debounce.Store(int64(duration))
}

func (d *darwinDesktop) SetTrayStatus(status TrayStatus) {
	d.stateMutex.Lock()
	d.trayStatus = status
	d.stateMutex.Unlock()
	d.updateTray()
}

func (d *darwinDesktop) SetSelectionStatus(enabled bool, shortcut string) {
	d.stateMutex.Lock()
	d.selectionEnabled = enabled
	d.selectionShortcut = shortcut
	d.stateMutex.Unlock()
	d.updateTray()
}

func (d *darwinDesktop) SetSelectionHotkey(shortcut *hotkey.Shortcut) error {
	d.hotkeyRequestMutex.Lock()
	defer d.hotkeyRequestMutex.Unlock()

	d.stateMutex.Lock()
	if shortcutsEqual(d.hotkey, shortcut) {
		d.stateMutex.Unlock()
		return nil
	}

	var copyShortcut *hotkey.Shortcut
	if shortcut != nil {
		value := *shortcut
		copyShortcut = &value
	}
	d.stateMutex.Unlock()

	if copyShortcut == nil {
		C.macosSetSelectionHotkey(0, 0, 0)
	} else {
		C.macosSetSelectionHotkey(
			C.uint32_t(copyShortcut.Modifiers),
			C.uint32_t(copyShortcut.VirtualKey),
			1,
		)
	}

	d.stateMutex.Lock()
	d.hotkey = copyShortcut
	d.stateMutex.Unlock()
	return nil
}

func shortcutsEqual(left *hotkey.Shortcut, right *hotkey.Shortcut) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Modifiers == right.Modifiers && left.VirtualKey == right.VirtualKey
}

func (d *darwinDesktop) SelectedText(ctx context.Context, maxLength int) (string, error) {
	if maxLength <= 0 {
		return "", errors.New("选中文本长度上限必须大于 0")
	}
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if C.macosAccessibilityTrusted() == 0 {
		return "", ErrSelectionUnsupported
	}
	text := macosString(C.macosReadSelectedText())
	if text == "" {
		return "", ErrNoSelectedText
	}
	if utf8.RuneCountInString(text) > maxLength {
		return "", ErrSelectedTextTooLong
	}
	return text, nil
}

func (d *darwinDesktop) CompatibleSelectedText(ctx context.Context, maxLength int) (string, error) {
	if maxLength <= 0 {
		return "", errors.New("选中文本长度上限必须大于 0")
	}
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if C.macosAccessibilityTrusted() == 0 {
		return "", ErrSelectionUnsupported
	}

	d.compatibilityMutex.Lock()
	defer d.compatibilityMutex.Unlock()

	snapshot := C.macosClipboardSnapshotCreate()
	if snapshot == nil {
		return "", fmt.Errorf("%w：创建剪贴板快照失败，已终止兼容选区读取", ErrClipboardUnsafe)
	}
	defer C.macosClipboardSnapshotRelease(snapshot)

	d.internalClipboard.Store(true)
	defer d.internalClipboard.Store(false)

	before := int64(C.macosClipboardChangeCount())
	if C.macosWaitForModifierKeysReleased(750) == 0 {
		return "", errors.New("等待划词快捷键修饰键释放超时，已终止兼容选区读取")
	}
	if C.macosSendCopyShortcut() == 0 {
		return "", errors.New("发送复制快捷键失败，请在系统设置中授予辅助功能权限")
	}

	var copiedSequence int64
	restoreAllowed := false
	defer func() {
		currentSequence := int64(C.macosClipboardChangeCount())
		if !restoreAllowed || copiedSequence == 0 || currentSequence != copiedSequence {
			d.internalClipboard.Store(false)
			if currentSequence != before {
				d.onClipboardUpdate(currentSequence)
			}
			return
		}
		if !d.restoreClipboardSnapshot(snapshot, copiedSequence) {
			d.internalClipboard.Store(false)
			d.onClipboardUpdate(int64(C.macosClipboardChangeCount()))
			return
		}
		d.internalClipboard.Store(false)
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(1200 * time.Millisecond)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			current := int64(C.macosClipboardChangeCount())
			if current == before {
				continue
			}
			copiedSequence = current
			text := macosString(C.macosReadClipboard())
			if text == "" {
				return "", ErrNoSelectedText
			}
			settleTimer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				settleTimer.Stop()
				return "", ctx.Err()
			case <-settleTimer.C:
			}
			if settledSequence := int64(C.macosClipboardChangeCount()); settledSequence != current {
				return "", fmt.Errorf("%w：检测到新的剪贴板内容", ErrClipboardChangedDuringCopy)
			}
			restoreAllowed = true
			if utf8.RuneCountInString(text) > maxLength {
				return "", ErrSelectedTextTooLong
			}
			return text, nil
		case <-deadline.C:
			return "", ErrNoSelectedText
		}
	}
}

func (d *darwinDesktop) restoreClipboardSnapshot(snapshot unsafe.Pointer, copiedSequence int64) bool {
	if snapshot == nil || copiedSequence == 0 {
		return false
	}
	current := int64(C.macosClipboardChangeCount())
	if current != copiedSequence {
		return false
	}
	if C.macosClipboardSnapshotRestore(snapshot) == 0 {
		return false
	}
	restoredSequence := int64(C.macosClipboardChangeCount())
	if restoredSequence != 0 {
		d.ignoredSequence.Store(restoredSequence)
	}
	return true
}

func (d *darwinDesktop) ApplyOverlay(_ OverlayOptions) error {
	// The macOS subtitle window is configured through Wails' AppKit window
	// options and subtitleController, so no additional native style mutation is
	// necessary here.
	return nil
}

func (d *darwinDesktop) ApplySettingsWindow(_ WindowOptions) error {
	return nil
}

func (d *darwinDesktop) CursorPosition() (int, int, bool) {
	var x C.double
	var y C.double
	if C.macosCursorPosition(&x, &y) == 0 {
		return 0, 0, false
	}
	return int(x), int(y), true
}

func (d *darwinDesktop) OpenPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("打开路径不能为空")
	}
	command := exec.Command("open", path)
	if err := command.Run(); err != nil {
		return fmt.Errorf("调用 macOS open 命令失败: %w", err)
	}
	return nil
}

func (d *darwinDesktop) Stop() error {
	d.stopOnce.Do(func() {
		d.stopDebounceTimer()

		activeDarwinDesktop.Lock()
		if activeDarwinDesktop.desktop == d {
			activeDarwinDesktop.desktop = nil
		}
		activeDarwinDesktop.Unlock()

		C.macosStopTray()
		close(d.done)
	})
	return nil
}

func (d *darwinDesktop) monitorClipboard() {
	ticker := time.NewTicker(macClipboardPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			current := int64(C.macosClipboardChangeCount())
			previous := d.lastClipboard.Swap(current)
			if current != previous {
				d.onClipboardUpdate(current)
			}
		case <-d.done:
			return
		case <-d.context.Done():
			return
		}
	}
}

func (d *darwinDesktop) onClipboardUpdate(sequence int64) {
	if d.internalClipboard.Load() {
		return
	}
	ignored := d.ignoredSequence.Load()
	if ignored != 0 {
		if sequence == ignored {
			return
		}
		d.ignoredSequence.CompareAndSwap(ignored, 0)
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
		text, available, err := d.readClipboardWithRetry()
		if err != nil || !available || !d.listening.Load() {
			return
		}
		if d.callbacks.OnClipboardText != nil {
			d.callbacks.OnClipboardText(text)
		}
	})
}

func (d *darwinDesktop) readClipboardWithRetry() (string, bool, error) {
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

func (d *darwinDesktop) readClipboard() (string, bool, error) {
	value := C.macosReadClipboard()
	if value == nil {
		return "", false, nil
	}
	defer C.macosFreeString(value)
	text := C.GoString(value)
	if len(text) > macClipboardReadLimit {
		return "", false, fmt.Errorf("剪贴板文本超过安全读取上限: %d bytes", len(text))
	}
	return text, true, nil
}

func (d *darwinDesktop) stopDebounceTimer() {
	d.timerMutex.Lock()
	defer d.timerMutex.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func (d *darwinDesktop) updateTray() {
	d.stateMutex.RLock()
	status := d.trayStatus
	selectionEnabled := d.selectionEnabled
	selectionShortcut := d.selectionShortcut
	d.stateMutex.RUnlock()

	nativeStatus := macTrayStatusConfigError
	switch status {
	case TrayStatusRunning:
		nativeStatus = macTrayStatusRunning
	case TrayStatusPaused:
		nativeStatus = macTrayStatusPaused
	}
	var cShortcut *C.char
	if selectionShortcut != "" {
		cShortcut = C.CString(selectionShortcut)
		defer C.free(unsafe.Pointer(cShortcut))
	}
	listening := 0
	if d.listening.Load() {
		listening = 1
	}
	selection := 0
	if selectionEnabled {
		selection = 1
	}
	C.macosSetTrayState(
		C.int(nativeStatus),
		C.int(listening),
		C.int(selection),
		cShortcut,
	)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func macosString(value *C.char) string {
	if value == nil {
		return ""
	}
	defer C.macosFreeString(value)
	return C.GoString(value)
}

//export goMacEventCallback
func goMacEventCallback(eventID C.int) {
	activeDarwinDesktop.RLock()
	desktop := activeDarwinDesktop.desktop
	activeDarwinDesktop.RUnlock()
	if desktop == nil {
		return
	}

	var callback func()
	switch int(eventID) {
	case macEventSelectionTranslate:
		callback = desktop.callbacks.OnSelectionTranslate
	case macEventToggleSelection:
		callback = desktop.callbacks.OnToggleSelection
	case macEventToggleListening:
		callback = desktop.callbacks.OnToggleListening
	case macEventOpenSettings:
		callback = desktop.callbacks.OnOpenSettings
	case macEventOpenLogs:
		callback = desktop.callbacks.OnOpenLogs
	case macEventQuit:
		callback = desktop.callbacks.OnQuit
	}
	if callback != nil {
		go callback()
	}
}
