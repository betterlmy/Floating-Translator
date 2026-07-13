//go:build darwin

package main

import (
	"sync"
	"time"

	"floating-translator/internal/config"
	"floating-translator/internal/processor"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const subtitleHoverPollInterval = 50 * time.Millisecond

type cursorPositionProvider func() (x int, y int, ok bool)

type subtitleContentBounds struct {
	x       int
	y       int
	width   int
	height  int
	visible bool
}

// webviewSubtitleWindow 统一使用透明 Wails WebView 渲染双平台字幕。
// 鼠标悬停由原生全局坐标检测，窗口本身始终保持鼠标穿透。
type webviewSubtitleWindow struct {
	window         *application.WebviewWindow
	cursorPosition cursorPositionProvider

	emitMutex      sync.Mutex
	mutex          sync.RWMutex
	logicalBounds  windowBounds
	physicalBounds application.Rect
	contentBounds  subtitleContentBounds
	hovered        bool

	done      chan struct{}
	closeOnce sync.Once
}

func newWebviewSubtitleWindow(window *application.WebviewWindow, cursorPosition cursorPositionProvider) *webviewSubtitleWindow {
	subtitle := &webviewSubtitleWindow{
		window:         window,
		cursorPosition: cursorPosition,
		done:           make(chan struct{}),
	}
	go subtitle.monitorHover()
	return subtitle
}

func (w *webviewSubtitleWindow) Configure(bounds windowBounds, cfg config.SubtitleConfig) error {
	if w.window == nil {
		return nil
	}
	w.window.SetSize(bounds.Width, bounds.Height)
	w.window.SetPosition(bounds.X, bounds.Y)
	w.window.SetAlwaysOnTop(true)
	w.window.SetIgnoreMouseEvents(true)
	physicalBounds := w.window.PhysicalBounds()

	w.mutex.Lock()
	w.logicalBounds = bounds
	w.physicalBounds = physicalBounds
	w.mutex.Unlock()

	w.window.EmitEvent(subtitleConfigEvent, cfg)
	return nil
}

func (w *webviewSubtitleWindow) Display(event processor.Event) {
	if w.window == nil {
		return
	}
	// 强制下一次轮询重新发送悬停状态，避免鼠标停留在旧字幕上时，
	// 新字幕因收不到新的 true 事件而继续倒计时。
	w.emitMutex.Lock()
	w.mutex.Lock()
	w.hovered = false
	w.mutex.Unlock()
	w.window.EmitEvent(translationResultEvent, event)
	w.emitMutex.Unlock()
	w.window.Show()
}

func (w *webviewSubtitleWindow) SetContentBounds(x int, y int, width int, height int, visible bool) {
	if x < 0 || y < 0 || width <= 0 || height <= 0 {
		visible = false
	}
	w.mutex.Lock()
	w.contentBounds = subtitleContentBounds{x: x, y: y, width: width, height: height, visible: visible}
	wasHovered := w.hovered
	if !visible {
		w.hovered = false
	}
	w.mutex.Unlock()
	if wasHovered && !visible && w.window != nil {
		w.emitMutex.Lock()
		w.window.EmitEvent(subtitleHoverEvent, false)
		w.emitMutex.Unlock()
	}
}

func (w *webviewSubtitleWindow) Close() {
	w.closeOnce.Do(func() {
		close(w.done)
		if w.window != nil {
			w.window.Close()
		}
	})
}

func (w *webviewSubtitleWindow) monitorHover() {
	ticker := time.NewTicker(subtitleHoverPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.updateHover()
		case <-w.done:
			return
		}
	}
}

func (w *webviewSubtitleWindow) updateHover() {
	if w.cursorPosition == nil || w.window == nil {
		return
	}
	cursorX, cursorY, ok := w.cursorPosition()

	w.emitMutex.Lock()
	defer w.emitMutex.Unlock()
	w.mutex.Lock()
	content := w.contentBounds
	logical := w.logicalBounds
	physical := w.physicalBounds
	hovered := ok && content.visible && logical.Width > 0 && logical.Height > 0 &&
		pointInSubtitleContent(cursorX, cursorY, content, logical, physical)
	if hovered == w.hovered {
		w.mutex.Unlock()
		return
	}
	w.hovered = hovered
	w.mutex.Unlock()

	w.window.EmitEvent(subtitleHoverEvent, hovered)
}

func pointInSubtitleContent(cursorX int, cursorY int, content subtitleContentBounds, logical windowBounds, physical application.Rect) bool {
	left := physical.X + content.x*physical.Width/logical.Width
	top := physical.Y + content.y*physical.Height/logical.Height
	right := physical.X + (content.x+content.width)*physical.Width/logical.Width
	bottom := physical.Y + (content.y+content.height)*physical.Height/logical.Height
	return cursorX >= left && cursorX < right && cursorY >= top && cursorY < bottom
}
