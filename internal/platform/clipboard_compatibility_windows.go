//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

const (
	inputTypeKeyboard         = 1
	keyboardEventKeyUp        = 0x0002
	virtualKeyControl         = 0x11
	virtualKeyMenu            = 0x12
	virtualKeyShift           = 0x10
	virtualKeyLeftWindows     = 0x5B
	virtualKeyRightWindows    = 0x5C
	virtualKeyC               = 0x43
	globalMemoryMoveable      = 0x0002
	clipboardFormatText       = 1
	clipboardFormatBitmap     = 2
	clipboardFormatMetafile   = 3
	clipboardFormatPalette    = 9
	clipboardFormatEnhMeta    = 14
	clipboardFormatOEMText    = 7
	clipboardFormatLocale     = 16
	clipboardPollInterval     = 15 * time.Millisecond
	clipboardCopySettleTime   = 50 * time.Millisecond
	modifierReleaseInterval   = 10 * time.Millisecond
	modifierReleaseSettleTime = 25 * time.Millisecond
	maxClipboardSnapshotBytes = 256 * 1024 * 1024
)

var (
	errClipboardChangedDuringRestore = errors.New("剪贴板在恢复前发生变化")
	ole32                            = windows.NewLazySystemDLL("ole32.dll")

	procSendInput                  = user32.NewProc("SendInput")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procEnumClipboardFormats       = user32.NewProc("EnumClipboardFormats")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procOleInitialize              = ole32.NewProc("OleInitialize")
	procOleUninitialize            = ole32.NewProc("OleUninitialize")
	procOleFlushClipboard          = ole32.NewProc("OleFlushClipboard")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
)

type keyboardInputEvent struct {
	Type       uint32
	Padding    uint32
	VirtualKey uint16
	ScanCode   uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
	Reserved   [8]byte
}

type clipboardSnapshotEntry struct {
	format uint32
	handle uintptr
}

type clipboardSnapshot struct {
	entries []clipboardSnapshotEntry
}

// CompatibleSelectedText 通过模拟复制读取选区，并恢复调用前的剪贴板数据。
func (d *windowsDesktop) CompatibleSelectedText(ctx context.Context, maxLength int) (text string, err error) {
	if maxLength <= 0 {
		return "", errors.New("选中文本长度上限必须大于 0")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	d.compatibilityMutex.Lock()
	defer d.compatibilityMutex.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	result, _, _ := procOleInitialize.Call(0)
	if hresultFailed(result) {
		return "", fmt.Errorf("初始化 OLE 剪贴板失败: %w", ole.NewError(result))
	}
	defer procOleUninitialize.Call()

	d.internalClipboard.Store(true)
	procOleFlushClipboard.Call()
	snapshot, err := d.snapshotClipboard(ctx)
	if err != nil {
		d.internalClipboard.Store(false)
		return "", err
	}
	defer snapshot.release()
	snapshotSequence := clipboardSequenceNumber()
	restoreSequence := snapshotSequence
	copyAttempted := false
	restoreAllowed := false
	defer func() {
		currentSequence := clipboardSequenceNumber()
		if !copyAttempted || !restoreAllowed || currentSequence != restoreSequence {
			d.internalClipboard.Store(false)
			if copyAttempted && currentSequence != snapshotSequence {
				d.onClipboardUpdate()
			}
			return
		}
		restoreContext, cancelRestore := context.WithTimeout(context.Background(), time.Second)
		restoreErr := d.restoreClipboard(restoreContext, snapshot, restoreSequence)
		cancelRestore()
		if errors.Is(restoreErr, errClipboardChangedDuringRestore) {
			d.internalClipboard.Store(false)
			d.onClipboardUpdate()
			return
		}
		if restoreErr != nil {
			d.internalClipboard.Store(false)
			d.onClipboardUpdate()
			if err == nil {
				text = ""
				err = restoreErr
			}
			return
		}
		d.ignoredClipboardSequence.Store(clipboardSequenceNumber())
		d.internalClipboard.Store(false)
	}()

	if err := waitForModifierKeysReleased(ctx); err != nil {
		return "", fmt.Errorf("等待划词快捷键释放失败: %w", err)
	}
	if clipboardSequenceNumber() != snapshotSequence {
		return "", errors.New("兼容读取开始前剪贴板已被其他程序更新，已取消操作")
	}
	initialSequence := snapshotSequence
	if err := sendCopyShortcut(); err != nil {
		return "", err
	}
	copyAttempted = true
	text, copiedSequence, waitErr := d.waitForCopiedText(ctx, initialSequence)
	restoreSequence = copiedSequence
	err = waitErr
	if err != nil {
		return "", err
	}
	restoreAllowed = true
	if utf8.RuneCountInString(text) > maxLength {
		return "", ErrSelectedTextTooLong
	}
	return text, nil
}

func waitForModifierKeysReleased(ctx context.Context) error {
	ticker := time.NewTicker(modifierReleaseInterval)
	defer ticker.Stop()
	for {
		if !modifierKeyPressed() {
			settleTimer := time.NewTimer(modifierReleaseSettleTime)
			select {
			case <-ctx.Done():
				settleTimer.Stop()
				return ctx.Err()
			case <-settleTimer.C:
				if !modifierKeyPressed() {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func modifierKeyPressed() bool {
	for _, virtualKey := range [...]uintptr{
		virtualKeyControl,
		virtualKeyMenu,
		virtualKeyShift,
		virtualKeyLeftWindows,
		virtualKeyRightWindows,
	} {
		state, _, _ := procGetAsyncKeyState.Call(virtualKey)
		if uint16(state)&0x8000 != 0 {
			return true
		}
	}
	return false
}

func (d *windowsDesktop) snapshotClipboard(ctx context.Context) (*clipboardSnapshot, error) {
	if err := d.openClipboard(ctx); err != nil {
		return nil, fmt.Errorf("打开剪贴板以创建快照失败: %w", err)
	}
	defer procCloseClipboard.Call()

	snapshot := &clipboardSnapshot{}
	totalBytes := uintptr(0)
	for format := uint32(0); ; {
		nextFormat, _, _ := procEnumClipboardFormats.Call(uintptr(format))
		if nextFormat == 0 {
			break
		}
		format = uint32(nextFormat)
		if !isSafeClipboardSnapshotFormat(format) {
			snapshot.release()
			return nil, fmt.Errorf("%w：格式 %d 不是纯文本格式", ErrClipboardUnsafe, format)
		}
		handle, _, callErr := procGetClipboardData.Call(uintptr(format))
		if handle == 0 {
			snapshot.release()
			return nil, fmt.Errorf("保存原剪贴板失败：读取格式 %d 失败: %w", format, callErr)
		}
		clone, size, cloneErr := cloneGlobalMemory(handle, maxClipboardSnapshotBytes-totalBytes)
		if cloneErr != nil {
			snapshot.release()
			return nil, fmt.Errorf("保存原剪贴板失败：复制格式 %d 失败: %w", format, cloneErr)
		}
		totalBytes += size
		snapshot.entries = append(snapshot.entries, clipboardSnapshotEntry{format: format, handle: clone})
	}
	return snapshot, nil
}

func (d *windowsDesktop) restoreClipboard(ctx context.Context, snapshot *clipboardSnapshot, expectedSequence uint32) error {
	if err := d.openClipboard(ctx); err != nil {
		return fmt.Errorf("打开剪贴板以恢复快照失败: %w", err)
	}
	defer procCloseClipboard.Call()
	if clipboardSequenceNumber() != expectedSequence {
		return errClipboardChangedDuringRestore
	}
	emptied, _, callErr := procEmptyClipboard.Call()
	if emptied == 0 {
		return fmt.Errorf("清空临时剪贴板失败: %w", callErr)
	}

	var firstErr error
	for index := range snapshot.entries {
		entry := &snapshot.entries[index]
		if entry.handle == 0 {
			continue
		}
		stored, _, storeErr := procSetClipboardData.Call(uintptr(entry.format), entry.handle)
		if stored == 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("恢复剪贴板格式 %d 失败: %w", entry.format, storeErr)
			}
			continue
		}
		entry.handle = 0
	}
	return firstErr
}

func (d *windowsDesktop) openClipboard(ctx context.Context) error {
	ticker := time.NewTicker(clipboardPollInterval)
	defer ticker.Stop()
	for {
		opened, _, callErr := procOpenClipboard.Call(d.window)
		if opened != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待剪贴板可用超时: %w: %v", ctx.Err(), callErr)
		case <-ticker.C:
		}
	}
}

func cloneGlobalMemory(handle uintptr, remainingBytes uintptr) (uintptr, uintptr, error) {
	size, _, callErr := procGlobalSize.Call(handle)
	if size == 0 {
		return 0, 0, fmt.Errorf("读取剪贴板数据大小失败: %w", callErr)
	}
	if size > remainingBytes {
		return 0, 0, fmt.Errorf("剪贴板快照超过 %d bytes 安全上限", maxClipboardSnapshotBytes)
	}
	clone, _, callErr := procGlobalAlloc.Call(globalMemoryMoveable, size)
	if clone == 0 {
		return 0, 0, fmt.Errorf("分配剪贴板快照内存失败: %w", callErr)
	}
	sourceData, _, sourceErr := procGlobalLock.Call(handle)
	if sourceData == 0 {
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("锁定原剪贴板数据失败: %w", sourceErr)
	}
	defer procGlobalUnlock.Call(handle)
	targetData, _, targetErr := procGlobalLock.Call(clone)
	if targetData == 0 {
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("锁定剪贴板快照内存失败: %w", targetErr)
	}
	buffer := make([]byte, size)
	var bytesRead uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), sourceData, &buffer[0], size, &bytesRead); err != nil {
		procGlobalUnlock.Call(clone)
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("读取原剪贴板数据失败: %w", err)
	}
	if bytesRead != size {
		procGlobalUnlock.Call(clone)
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("读取原剪贴板数据不完整: %d/%d bytes", bytesRead, size)
	}
	var bytesWritten uintptr
	if err := windows.WriteProcessMemory(windows.CurrentProcess(), targetData, &buffer[0], size, &bytesWritten); err != nil {
		procGlobalUnlock.Call(clone)
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("写入剪贴板快照失败: %w", err)
	}
	if bytesWritten != size {
		procGlobalUnlock.Call(clone)
		procGlobalFree.Call(clone)
		return 0, 0, fmt.Errorf("写入剪贴板快照不完整: %d/%d bytes", bytesWritten, size)
	}
	procGlobalUnlock.Call(clone)
	return clone, size, nil
}

func isSafeClipboardSnapshotFormat(format uint32) bool {
	// Only plain-text formats and the standard locale metadata are copied and
	// restored. Registered, private and delayed-rendered formats may look like
	// memory handles but are not safe to reconstruct byte-for-byte without their
	// owning application.
	switch format {
	case clipboardFormatText, clipboardFormatOEMText, clipboardFormatUnicodeText, clipboardFormatLocale:
		return true
	default:
		return false
	}
}

func (snapshot *clipboardSnapshot) release() {
	for index := range snapshot.entries {
		if snapshot.entries[index].handle != 0 {
			procGlobalFree.Call(snapshot.entries[index].handle)
			snapshot.entries[index].handle = 0
		}
	}
}

func (d *windowsDesktop) waitForCopiedText(ctx context.Context, initialSequence uint32) (string, uint32, error) {
	ticker := time.NewTicker(clipboardPollInterval)
	defer ticker.Stop()
	clipboardChanged := false
	lastSequence := initialSequence
	var lastReadErr error
	for {
		currentSequence := clipboardSequenceNumber()
		if !clipboardChanged && currentSequence != initialSequence {
			clipboardChanged = true
			lastSequence = currentSequence
		} else if clipboardChanged && currentSequence != lastSequence {
			return "", currentSequence, fmt.Errorf("%w：检测到多次剪贴板更新", ErrClipboardChangedDuringCopy)
		}
		if clipboardChanged {
			text, available, readErr := d.readClipboard()
			if readErr == nil && available && text != "" {
				settleTimer := time.NewTimer(clipboardCopySettleTime)
				select {
				case <-ctx.Done():
					settleTimer.Stop()
					return "", currentSequence, ctx.Err()
				case <-settleTimer.C:
				}
				if settledSequence := clipboardSequenceNumber(); settledSequence != currentSequence {
					return "", settledSequence, fmt.Errorf("%w：检测到新的剪贴板内容", ErrClipboardChangedDuringCopy)
				}
				return text, currentSequence, nil
			}
			if readErr != nil {
				lastReadErr = readErr
			}
		}

		select {
		case <-ctx.Done():
			if lastReadErr != nil {
				return "", lastSequence, fmt.Errorf("模拟复制后读取剪贴板失败: %w", lastReadErr)
			}
			return "", lastSequence, fmt.Errorf("%w：模拟复制后未获取到文本", ErrNoSelectedText)
		case <-ticker.C:
		}
	}
}

func sendCopyShortcut() error {
	events := copyShortcutEvents()
	sent, _, callErr := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
	if sent != uintptr(len(events)) {
		return fmt.Errorf("模拟 Ctrl+C 失败，仅发送 %d/%d 个键盘事件: %w", sent, len(events), callErr)
	}
	return nil
}

func copyShortcutEvents() [4]keyboardInputEvent {
	return [4]keyboardInputEvent{
		{Type: inputTypeKeyboard, VirtualKey: virtualKeyControl},
		{Type: inputTypeKeyboard, VirtualKey: virtualKeyC},
		{Type: inputTypeKeyboard, VirtualKey: virtualKeyC, Flags: keyboardEventKeyUp},
		{Type: inputTypeKeyboard, VirtualKey: virtualKeyControl, Flags: keyboardEventKeyUp},
	}
}

func clipboardSequenceNumber() uint32 {
	sequence, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(sequence)
}

func hresultFailed(result uintptr) bool {
	return int32(result) < 0
}
