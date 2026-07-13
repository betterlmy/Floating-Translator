//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa
#include <stdint.h>
#include <stdlib.h>

int64_t macosClipboardChangeCount(void);
char *macosReadClipboard(void);
void macosFreeString(char *value);
*/
import "C"

import (
	"fmt"
	"time"
)

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
