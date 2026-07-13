//go:build windows

package platform

import (
	"encoding/binary"
	"fmt"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

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
