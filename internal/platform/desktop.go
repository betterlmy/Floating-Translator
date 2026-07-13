// Package platform 封装桌面能力，并提供不支持平台的编译占位实现。
package platform

import (
	"context"
	"errors"
	"time"

	"floating-translator/internal/hotkey"
)

// ErrUnsupported 表示当前操作系统不支持桌面集成。
var ErrUnsupported = errors.New("当前操作系统不支持桌面集成")

// ErrSelectionUnsupported 表示当前焦点控件不支持辅助功能文本选区。
var ErrSelectionUnsupported = errors.New("当前应用不支持读取选中文本")

// ErrNoSelectedText 表示当前没有非空文本选区。
var ErrNoSelectedText = errors.New("未获取到选中文本")

// ErrSelectedTextTooLong 表示选区超过配置的安全长度上限。
var ErrSelectedTextTooLong = errors.New("选中文本超过长度上限")

// ErrSelectionBusy 表示已有一次选区读取尚未返回，避免阻塞的 UI Automation 调用无限堆积。
var ErrSelectionBusy = errors.New("选区读取正在进行")

// ErrClipboardUnsafe 表示原剪贴板包含不能安全快照和恢复的复杂格式。
var ErrClipboardUnsafe = errors.New("原剪贴板包含不支持安全快照的复杂格式")

// ErrClipboardChangedDuringCopy 表示兼容复制期间检测到用户或其他程序更新了剪贴板。
var ErrClipboardChangedDuringCopy = errors.New("兼容复制期间剪贴板发生并发更新")

// TrayStatus 表示托盘菜单展示的应用状态。
type TrayStatus string

const (
	// TrayStatusRunning 表示配置有效且监听已启用。
	TrayStatusRunning TrayStatus = "running"
	// TrayStatusPaused 表示用户已暂停监听。
	TrayStatusPaused TrayStatus = "paused"
	// TrayStatusConfigError 表示配置无效，监听不可用。
	TrayStatusConfigError TrayStatus = "config_error"
)

// Callbacks 是原生桌面事件回调。
type Callbacks struct {
	OnClipboardText      func(text string)
	OnSelectionTranslate func()
	OnToggleSelection    func()
	OnToggleListening    func()
	OnOpenSettings       func()
	OnOpenLogs           func()
	OnQuit               func()
}

// OverlayOptions 是透明字幕窗口的原生布局参数。
type OverlayOptions struct {
	WindowClassName     string
	WindowTitle         string
	WidthPercent        int
	BottomOffsetPercent int
}

// WindowOptions 是按类名和标题定位 Wails 窗口的参数。
type WindowOptions struct {
	WindowClassName string
	WindowTitle     string
	Width           int
	Height          int
}

// Desktop 定义剪贴板、托盘、快捷键和窗口能力。
type Desktop interface {
	SetApplicationIcon(icon []byte)
	Start(ctx context.Context, callbacks Callbacks) error
	SetListening(enabled bool)
	SetDebounce(duration time.Duration)
	SetTrayStatus(status TrayStatus)
	SetSelectionStatus(enabled bool, shortcut string)
	SetSelectionHotkey(shortcut *hotkey.Shortcut) error
	SelectedText(ctx context.Context, maxLength int) (string, error)
	CompatibleSelectedText(ctx context.Context, maxLength int) (string, error)
	ApplyOverlay(options OverlayOptions) error
	ApplySettingsWindow(options WindowOptions) error
	CursorPosition() (x int, y int, ok bool)
	OpenPath(path string) error
	Stop() error
}
