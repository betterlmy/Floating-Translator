//go:build windows

package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestSubtitleWindowOptionsUseTransparentCompositionHosting(t *testing.T) {
	options := subtitleWindowOptions()

	if !options.Frameless {
		t.Error("字幕窗口必须无边框")
	}
	if !options.AlwaysOnTop {
		t.Error("字幕窗口必须置顶")
	}
	if !options.IgnoreMouseEvents {
		t.Error("字幕窗口必须允许鼠标穿透")
	}
	if options.BackgroundType != application.BackgroundTypeTransparent {
		t.Fatalf("字幕窗口背景类型 = %v，期望透明背景", options.BackgroundType)
	}
	if options.BackgroundColour.Alpha != 0 {
		t.Fatalf("字幕窗口背景透明度 = %d，期望 0", options.BackgroundColour.Alpha)
	}
	if !options.Windows.HiddenOnTaskbar {
		t.Error("字幕窗口不应显示在任务栏")
	}
	if !options.Windows.WebView2CompositionHosting {
		t.Error("字幕窗口必须使用 WebView2 Composition Hosting，避免透明窗口显示为白底")
	}
}
