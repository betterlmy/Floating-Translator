package subtitle

import (
	"floating-translator/internal/config"
	"floating-translator/internal/processor"
)

const (
	translationResultEvent = "translation:result"
	subtitleConfigEvent    = "subtitle:config"
	subtitleHoverEvent     = "subtitle:hover"
)

// Bounds 描述字幕窗口在屏幕工作区中的逻辑坐标。
type Bounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Controller 管理平台字幕窗口。
type Controller interface {
	Configure(Bounds, config.SubtitleConfig) error
	Display(processor.Event)
	SetContentBounds(x int, y int, width int, height int, visible bool)
	Close()
}
