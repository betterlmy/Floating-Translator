//go:build darwin

package main

import (
	"fmt"
	"sync"

	"floating-translator/internal/config"
	"floating-translator/internal/processor"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// darwinSubtitleWindow uses a transparent, click-through Wails WebView. The
// shared Vue overlay provides the same animation and styling as the Windows
// native renderer while AppKit owns the transparent always-on-top surface.
type darwinSubtitleWindow struct {
	window    *application.WebviewWindow
	closeOnce sync.Once
}

func (w *darwinSubtitleWindow) Configure(bounds windowBounds, cfg config.SubtitleConfig) error {
	if w.window == nil {
		return fmt.Errorf("字幕窗口尚未初始化")
	}
	w.window.SetSize(bounds.Width, bounds.Height)
	w.window.SetPosition(bounds.X, bounds.Y)
	w.window.SetAlwaysOnTop(true)
	w.window.SetIgnoreMouseEvents(true)
	w.window.EmitEvent(subtitleConfigEvent, cfg)
	return nil
}

func (w *darwinSubtitleWindow) Display(event processor.Event) {
	if w.window == nil {
		return
	}
	w.window.EmitEvent(translationResultEvent, event)
	w.window.Show()
}

func (w *darwinSubtitleWindow) Close() {
	w.closeOnce.Do(func() {
		if w.window != nil {
			w.window.Close()
		}
	})
}
