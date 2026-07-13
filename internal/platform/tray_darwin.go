//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa
#include <stdlib.h>

void macosSetTrayState(int status, int listening, int selectionEnabled,
                       const char *shortcut);
*/
import "C"

import "unsafe"

func (d *darwinDesktop) updateTray() {
	d.stateMutex.RLock()
	status := d.trayStatus
	selectionEnabled := d.selectionEnabled
	selectionShortcut := d.selectionShortcut
	d.stateMutex.RUnlock()

	nativeStatus := macTrayStatusConfigError
	switch status {
	case TrayStatusRunning:
		nativeStatus = macTrayStatusRunning
	case TrayStatusPaused:
		nativeStatus = macTrayStatusPaused
	}
	var cShortcut *C.char
	if selectionShortcut != "" {
		cShortcut = C.CString(selectionShortcut)
		defer C.free(unsafe.Pointer(cShortcut))
	}
	listening := 0
	if d.listening.Load() {
		listening = 1
	}
	selection := 0
	if selectionEnabled {
		selection = 1
	}
	C.macosSetTrayState(
		C.int(nativeStatus),
		C.int(listening),
		C.int(selection),
		cShortcut,
	)
}
