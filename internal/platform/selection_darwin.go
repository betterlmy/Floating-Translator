//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework ApplicationServices -framework Carbon
#include <stdint.h>
#include <stdlib.h>

void macosSetSelectionHotkey(uint32_t modifiers, uint32_t key, int enabled);
int macosAccessibilityTrusted(void);
char *macosReadSelectedText(void);
int64_t macosClipboardChangeCount(void);
char *macosReadClipboard(void);
void macosFreeString(char *value);
void *macosClipboardSnapshotCreate(void);
void macosClipboardSnapshotRelease(void *value);
int macosClipboardSnapshotRestore(void *value);
int macosWaitForModifierKeysReleased(int timeoutMilliseconds);
int macosSendCopyShortcut(void);
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
	"unsafe"

	"floating-translator/internal/hotkey"
)

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
