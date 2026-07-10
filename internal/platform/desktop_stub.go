//go:build !windows

package platform

import (
	"context"
	"time"

	"floating-translator/internal/hotkey"
)

type unsupportedDesktop struct{}

// NewDesktop 创建当前平台的桌面实现。
func NewDesktop() Desktop { return &unsupportedDesktop{} }

func (d *unsupportedDesktop) Start(context.Context, Callbacks) error { return ErrUnsupported }

func (d *unsupportedDesktop) SetListening(bool) {}

func (d *unsupportedDesktop) SetDebounce(time.Duration) {}

func (d *unsupportedDesktop) SetTrayStatus(TrayStatus) {}

func (d *unsupportedDesktop) SetSelectionStatus(bool, string) {}

func (d *unsupportedDesktop) SetSelectionHotkey(*hotkey.Shortcut) error { return ErrUnsupported }

func (d *unsupportedDesktop) SelectedText(context.Context, int) (string, error) {
	return "", ErrUnsupported
}

func (d *unsupportedDesktop) CompatibleSelectedText(context.Context, int) (string, error) {
	return "", ErrUnsupported
}

func (d *unsupportedDesktop) ApplyOverlay(OverlayOptions) error { return ErrUnsupported }

func (d *unsupportedDesktop) ApplySettingsWindow(WindowOptions) error { return ErrUnsupported }

func (d *unsupportedDesktop) OpenPath(string) error { return ErrUnsupported }

func (d *unsupportedDesktop) Stop() error { return nil }
