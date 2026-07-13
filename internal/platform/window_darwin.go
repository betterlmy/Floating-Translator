//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Cocoa
int macosCursorPosition(double *x, double *y);
*/
import "C"

func (d *darwinDesktop) ApplyOverlay(_ OverlayOptions) error {
	// The macOS subtitle window is configured through Wails' AppKit window
	// options and subtitleController, so no additional native style mutation is
	// necessary here.
	return nil
}

func (d *darwinDesktop) ApplySettingsWindow(_ WindowOptions) error {
	return nil
}

func (d *darwinDesktop) CursorPosition() (int, int, bool) {
	var x C.double
	var y C.double
	if C.macosCursorPosition(&x, &y) == 0 {
		return 0, 0, false
	}
	return int(x), int(y), true
}
