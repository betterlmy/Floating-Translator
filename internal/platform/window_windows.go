//go:build windows

package platform

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (d *windowsDesktop) ApplyOverlay(options OverlayOptions) error {
	handle, err := findWailsWindow(options.WindowClassName, options.WindowTitle)
	if err != nil {
		return err
	}

	index := ^uintptr(19) // GWL_EXSTYLE 在 Win32 API 中的值为 -20。
	style, _, _ := procGetWindowLongPtrW.Call(handle, index)
	style = overlayExtendedStyle(style)
	previousStyle, _, callErr := procSetWindowLongPtrW.Call(handle, index, style)
	if previousStyle == 0 && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("设置字幕窗口扩展样式失败: %w", callErr)
	}

	var workArea rectangle
	result, _, callErr := procSystemParametersInfoW.Call(
		systemParameterGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)
	if result == 0 {
		return fmt.Errorf("读取主屏幕工作区失败: %w", callErr)
	}
	workWidth := int(workArea.Right - workArea.Left)
	workHeight := int(workArea.Bottom - workArea.Top)
	width := workWidth * options.WidthPercent / 100
	height := workHeight * 28 / 100
	x := int(workArea.Left) + (workWidth-width)/2
	y := int(workArea.Bottom) - height - workHeight*options.BottomOffsetPercent/100

	result, _, callErr = procSetWindowPos.Call(
		handle,
		topMostWindowHandle(),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		setWindowNoActivate|setWindowFrameChanged|setWindowShow,
	)
	if result == 0 {
		return fmt.Errorf("设置字幕窗口样式失败: %w", callErr)
	}
	return nil
}

func (d *windowsDesktop) ApplySettingsWindow(options WindowOptions) error {
	previousDPIContext, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorAwareV2Context())
	if previousDPIContext != 0 {
		defer procSetThreadDpiAwarenessContext.Call(previousDPIContext)
	}
	handle, err := findWailsWindow(options.WindowClassName, options.WindowTitle)
	if err != nil {
		return err
	}
	index := ^uintptr(19)
	style, _, _ := procGetWindowLongPtrW.Call(handle, index)
	style = settingsExtendedStyle(style)
	previousStyle, _, callErr := procSetWindowLongPtrW.Call(handle, index, style)
	if previousStyle == 0 && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("设置设置窗口扩展样式失败: %w", callErr)
	}
	var workArea rectangle
	result, _, callErr := procSystemParametersInfoW.Call(
		systemParameterGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)
	if result == 0 {
		return fmt.Errorf("读取主屏幕工作区失败: %w", callErr)
	}
	dpi, _, _ := procGetDpiForWindow.Call(handle)
	if dpi == 0 {
		dpi = 96
	}
	bounds := settingsWindowBounds(options.Width, options.Height, int(dpi), workArea)
	result, _, callErr = procSetWindowPos.Call(
		handle,
		topMostWindowHandle(),
		uintptr(bounds.Left),
		uintptr(bounds.Top),
		uintptr(bounds.Right-bounds.Left),
		uintptr(bounds.Bottom-bounds.Top),
		setWindowFrameChanged|setWindowShow,
	)
	if result == 0 {
		return fmt.Errorf("刷新设置窗口样式失败: %w", callErr)
	}
	procSetForegroundWindow.Call(handle)
	return nil
}

func settingsWindowBounds(width int, height int, dpi int, workArea rectangle) rectangle {
	if dpi <= 0 {
		dpi = 96
	}
	workWidth := int(workArea.Right - workArea.Left)
	workHeight := int(workArea.Bottom - workArea.Top)
	scaledWidth := min(width*dpi/96, workWidth*92/100)
	scaledHeight := min(height*dpi/96, workHeight*92/100)
	left := int(workArea.Left) + (workWidth-scaledWidth)/2
	top := int(workArea.Top) + (workHeight-scaledHeight)/2
	return rectangle{
		Left:   int32(left),
		Top:    int32(top),
		Right:  int32(left + scaledWidth),
		Bottom: int32(top + scaledHeight),
	}
}

func findWailsWindow(classNameValue string, titleValue string) (uintptr, error) {
	className, err := windows.UTF16PtrFromString(classNameValue)
	if err != nil {
		return 0, fmt.Errorf("转换窗口类名失败: %w", err)
	}
	title, err := windows.UTF16PtrFromString(titleValue)
	if err != nil {
		return 0, fmt.Errorf("转换窗口标题失败: %w", err)
	}
	handle, _, callErr := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)))
	if handle == 0 {
		return 0, fmt.Errorf("查找 Wails 窗口失败: %w", callErr)
	}
	return handle, nil
}

func overlayExtendedStyle(style uintptr) uintptr {
	style |= windowStyleTransparent | windowStyleToolWindow | windowStyleLayered | windowStyleNoActivate
	return style &^ windowStyleAppWindow
}

func settingsExtendedStyle(style uintptr) uintptr {
	style &^= windowStyleTransparent | windowStyleToolWindow | windowStyleNoActivate
	return style | windowStyleAppWindow
}

func (d *windowsDesktop) CursorPosition() (int, int, bool) {
	var cursor point
	result, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	return int(cursor.X), int(cursor.Y), result != 0
}
