//go:build windows

package platform

import (
	"encoding/binary"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

func TestWindowsStructLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("MVP 只验收 Windows x64")
	}
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{name: "WNDCLASSEXW", got: unsafe.Sizeof(windowClassEx{}), want: 80},
		{name: "MSG", got: unsafe.Sizeof(windowMessage{}), want: 48},
		{name: "NOTIFYICONDATAW", got: unsafe.Sizeof(notifyIconData{}), want: 976},
		{name: "INPUT", got: unsafe.Sizeof(keyboardInputEvent{}), want: 40},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("结构体大小 = %d, want %d", test.got, test.want)
			}
		})
	}
}

func TestCopyShortcutEvents(t *testing.T) {
	events := copyShortcutEvents()
	if events[0].VirtualKey != virtualKeyControl || events[1].VirtualKey != virtualKeyC {
		t.Fatalf("按键顺序错误: %+v", events)
	}
	if events[0].Flags != 0 || events[1].Flags != 0 {
		t.Fatalf("按下事件不应包含 KeyUp: %+v", events)
	}
	if events[2].VirtualKey != virtualKeyC || events[2].Flags != keyboardEventKeyUp {
		t.Fatalf("C 抬起事件错误: %+v", events[2])
	}
	if events[3].VirtualKey != virtualKeyControl || events[3].Flags != keyboardEventKeyUp {
		t.Fatalf("Ctrl 抬起事件错误: %+v", events[3])
	}
}

func TestGlobalMemoryClipboardFormats(t *testing.T) {
	for _, format := range []uint32{clipboardFormatUnicodeText, 8, 15, 16, 0xC001} {
		if !isGlobalMemoryClipboardFormat(format) {
			t.Fatalf("格式 %d 应按 HGLOBAL 复制", format)
		}
	}
	for _, format := range []uint32{clipboardFormatBitmap, clipboardFormatMetafile, clipboardFormatPalette, clipboardFormatEnhMeta, 0x0201, 0x0301} {
		if isGlobalMemoryClipboardFormat(format) {
			t.Fatalf("格式 %d 不应按 HGLOBAL 复制", format)
		}
	}
}

func TestDecodeUTF16LE(t *testing.T) {
	codeUnits := utf16.Encode([]rune("English 与中文 😀"))
	buffer := make([]byte, (len(codeUnits)+1)*2)
	for index, value := range codeUnits {
		binary.LittleEndian.PutUint16(buffer[index*2:], value)
	}
	if got, want := decodeUTF16LE(buffer, uintptr(len(buffer))), "English 与中文 😀"; got != want {
		t.Fatalf("decodeUTF16LE() = %q, want %q", got, want)
	}
}

func TestLoadExecutableIcon(t *testing.T) {
	icon, owned := loadExecutableIcon()
	if icon == 0 {
		t.Fatal("loadExecutableIcon() 返回空图标句柄")
	}
	if owned {
		procDestroyIcon.Call(icon)
	}
}

func TestOverlayExtendedStyle(t *testing.T) {
	style := overlayExtendedStyle(windowStyleAppWindow)
	for _, required := range []uintptr{
		windowStyleTransparent,
		windowStyleToolWindow,
		windowStyleLayered,
		windowStyleNoActivate,
	} {
		if style&required == 0 {
			t.Fatalf("扩展样式 0x%X 缺少 0x%X", style, required)
		}
	}
	if style&windowStyleAppWindow != 0 {
		t.Fatalf("扩展样式 0x%X 不应包含 WS_EX_APPWINDOW", style)
	}
}

func TestSettingsExtendedStyle(t *testing.T) {
	style := settingsExtendedStyle(windowStyleTransparent | windowStyleToolWindow | windowStyleNoActivate)
	for _, removed := range []uintptr{windowStyleTransparent, windowStyleToolWindow, windowStyleNoActivate} {
		if style&removed != 0 {
			t.Fatalf("扩展样式 0x%X 不应包含 0x%X", style, removed)
		}
	}
	if style&windowStyleAppWindow == 0 {
		t.Fatalf("扩展样式 0x%X 应包含 WS_EX_APPWINDOW", style)
	}
}

func TestSettingsWindowBoundsScaleWithDPI(t *testing.T) {
	workArea := rectangle{Left: 0, Top: 0, Right: 3200, Bottom: 2080}
	bounds := settingsWindowBounds(1080, 760, 168, workArea)
	if width := bounds.Right - bounds.Left; width != 1890 {
		t.Fatalf("设置窗口宽度 = %d, want 1890", width)
	}
	if height := bounds.Bottom - bounds.Top; height != 1330 {
		t.Fatalf("设置窗口高度 = %d, want 1330", height)
	}
	if bounds.Left != 655 || bounds.Top != 375 {
		t.Fatalf("设置窗口位置 = (%d,%d), want (655,375)", bounds.Left, bounds.Top)
	}
}

func TestHandleTrayCommandTogglesSelection(t *testing.T) {
	called := make(chan struct{})
	desktop := &windowsDesktop{
		callbacks: Callbacks{OnToggleSelection: func() { close(called) }},
	}
	desktop.handleTrayCommand(trayCommandToggleSelection)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("划词翻译开关回调未执行")
	}
}

func TestHandleTrayCommandOpensSettings(t *testing.T) {
	called := make(chan struct{})
	desktop := &windowsDesktop{
		callbacks: Callbacks{OnOpenSettings: func() { close(called) }},
	}
	desktop.handleTrayCommand(trayCommandSettings)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("设置窗口回调未执行")
	}
}
