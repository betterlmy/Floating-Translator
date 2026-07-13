//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

func (d *windowsDesktop) addTrayIcon() error {
	if d.icon == 0 {
		d.stateMutex.RLock()
		iconData := append([]byte(nil), d.iconData...)
		d.stateMutex.RUnlock()
		if len(iconData) > 0 {
			icon, err := w32.CreateSmallHIconFromImage(iconData)
			if err == nil && icon != 0 {
				d.icon, d.iconOwned = uintptr(icon), true
			}
		}
		if d.icon == 0 {
			d.icon, d.iconOwned = loadExecutableIcon()
		}
	}
	if d.icon == 0 {
		return errors.New("加载应用托盘图标失败")
	}
	data := d.newNotifyIconData()
	result, _, callErr := procShellNotifyIconW.Call(notifyIconAdd, uintptr(unsafe.Pointer(&data)))
	if result == 0 {
		return fmt.Errorf("创建系统托盘图标失败: %w", callErr)
	}
	data.Version = notifyIconVersion4
	procShellNotifyIconW.Call(notifyIconSetVersion, uintptr(unsafe.Pointer(&data)))
	return nil
}

func loadExecutableIcon() (uintptr, bool) {
	executablePath, err := os.Executable()
	if err == nil {
		path, pathErr := windows.UTF16PtrFromString(executablePath)
		if pathErr == nil {
			var smallIcon uintptr
			count, _, _ := procExtractIconExW.Call(
				uintptr(unsafe.Pointer(path)),
				0,
				0,
				uintptr(unsafe.Pointer(&smallIcon)),
				1,
			)
			if count > 0 && smallIcon != 0 {
				return smallIcon, true
			}
		}
	}
	icon, _, _ := procLoadIconW.Call(0, 32512)
	return icon, false
}

func (d *windowsDesktop) newNotifyIconData() notifyIconData {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Window:          d.window,
		ID:              trayIconID,
		Flags:           notifyIconFlagMessage | notifyIconFlagIcon | notifyIconFlagTip,
		CallbackMessage: trayCallbackMessage,
		Icon:            d.icon,
	}
	copy(data.Tip[:], windows.StringToUTF16("悬浮翻译器"))
	return data
}

func (d *windowsDesktop) showTrayMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	d.stateMutex.RLock()
	status := d.trayStatus
	selectionEnabled := d.selectionEnabled
	selectionShortcut := d.selectionShortcut
	d.stateMutex.RUnlock()
	statusLabel := "状态：配置错误"
	switch status {
	case TrayStatusRunning:
		statusLabel = "状态：正在监听"
	case TrayStatusPaused:
		statusLabel = "状态：已暂停"
	}
	d.appendMenu(menu, menuFlagString|menuFlagGrayed, 0, statusLabel)
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	if status != TrayStatusConfigError {
		label := "暂停监听"
		if !d.listening.Load() {
			label = "恢复监听"
		}
		d.appendMenu(menu, menuFlagString, trayCommandToggle, label)
	}
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	if selectionShortcut == "" {
		selectionShortcut = "未配置"
	}
	d.appendMenu(menu, menuFlagString|menuFlagGrayed, 0, "划词翻译快捷键："+selectionShortcut)
	selectionLabel := "开启划词翻译"
	if selectionEnabled {
		selectionLabel = "关闭划词翻译"
	}
	selectionFlags := uintptr(menuFlagString)
	if status == TrayStatusConfigError {
		selectionFlags |= menuFlagGrayed
	}
	d.appendMenu(menu, selectionFlags, trayCommandToggleSelection, selectionLabel)
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	d.appendMenu(menu, menuFlagString, trayCommandSettings, "设置…")
	d.appendMenu(menu, menuFlagString, trayCommandLogs, "打开日志目录")
	d.appendMenu(menu, menuFlagSeparator, 0, "")
	d.appendMenu(menu, menuFlagString, trayCommandQuit, "退出")

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(d.window)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		trackMenuRight|trackMenuReturn,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		d.window,
		0,
	)
	if command != 0 {
		d.handleTrayCommand(command)
	}
}

func (d *windowsDesktop) appendMenu(menu uintptr, flags uintptr, id uintptr, label string) {
	if flags&menuFlagSeparator != 0 {
		procAppendMenuW.Call(menu, flags, id, 0)
		return
	}
	text, _ := windows.UTF16PtrFromString(label)
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
}

func (d *windowsDesktop) handleTrayCommand(command uintptr) {
	switch command {
	case trayCommandToggle:
		invoke(d.callbacks.OnToggleListening)
	case trayCommandToggleSelection:
		invoke(d.callbacks.OnToggleSelection)
	case trayCommandSettings:
		invoke(d.callbacks.OnOpenSettings)
	case trayCommandLogs:
		invoke(d.callbacks.OnOpenLogs)
	case trayCommandQuit:
		invoke(d.callbacks.OnQuit)
	}
}
