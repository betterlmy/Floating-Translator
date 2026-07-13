//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework ApplicationServices -framework Carbon
#include <stdint.h>

void macosStartTray(void);
void macosStopTray(void);
int macosRequestAccessibilityPermission(void);
int64_t macosClipboardChangeCount(void);
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
