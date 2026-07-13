//go:build windows

package subtitle

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"floating-translator/internal/config"
	"floating-translator/internal/processor"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

const (
	nativeSubtitleClassName         = "FloatingTranslatorNativeSubtitle"
	nativeSubtitleSupersample       = 2
	nativeSubtitleMaxRasterPixels   = 40_000_000
	nativeSubtitleMaxFailures       = 3
	nativeSubtitleInitialBackoff    = 100 * time.Millisecond
	nativeSubtitleMaxBackoff        = 2 * time.Second
	nativeSubtitleScrollTopPause    = time.Second
	nativeSubtitleScrollBottomPause = 1200 * time.Millisecond
	nativeSubtitleScrollSpeed       = 24
)

// nativeSubtitleWindow is a dedicated layered Win32 window. Unlike a WebView
// window, UpdateLayeredWindow owns every pixel's alpha channel, so the area
// outside the subtitle text remains genuinely transparent.
type nativeSubtitleWindow struct {
	commands            chan nativeSubtitleCommand
	done                chan struct{}
	close               sync.Once
	renderErrorReporter func(error)
	lastRenderErrorAt   time.Time
}

type nativeSubtitleCommand struct {
	bounds *Bounds
	config *config.SubtitleConfig
	event  *processor.Event
	close  bool
	reply  chan error
}

type nativeSubtitleState struct {
	hwnd           w32.HWND
	dpi            uint
	bounds         Bounds
	config         config.SubtitleConfig
	text           string
	requestID      uint64
	startedAt      time.Time
	hovering       bool
	visible        bool
	persistent     bool
	lastAlpha      float64
	textBounds     w32.RECT
	renderFailures int
	nextRenderAt   time.Time
}

// NewWindow 创建 Windows 原生分层字幕窗口。
func NewWindow(renderErrorReporter func(error)) (Controller, error) {
	window := &nativeSubtitleWindow{
		commands:            make(chan nativeSubtitleCommand),
		done:                make(chan struct{}),
		renderErrorReporter: renderErrorReporter,
	}
	ready := make(chan error, 1)
	go window.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return window, nil
}

func (w *nativeSubtitleWindow) Configure(bounds Bounds, cfg config.SubtitleConfig) error {
	reply := make(chan error, 1)
	command := nativeSubtitleCommand{bounds: &bounds, config: &cfg, reply: reply}
	select {
	case w.commands <- command:
		return <-reply
	case <-w.done:
		return fmt.Errorf("字幕窗口已关闭")
	}
}

func (w *nativeSubtitleWindow) Display(event processor.Event) {
	copy := event
	select {
	case w.commands <- nativeSubtitleCommand{event: &copy}:
	case <-w.done:
	}
}

func (w *nativeSubtitleWindow) SetContentBounds(int, int, int, int, bool) {}

func (w *nativeSubtitleWindow) Close() {
	w.close.Do(func() {
		reply := make(chan error, 1)
		select {
		case w.commands <- nativeSubtitleCommand{close: true, reply: reply}:
			<-reply
		case <-w.done:
		}
	})
}

func (w *nativeSubtitleWindow) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)

	hwnd, err := createNativeSubtitleWindow()
	if err != nil {
		ready <- err
		return
	}
	defer w32.DestroyWindow(hwnd)
	ready <- nil

	state := nativeSubtitleState{hwnd: hwnd, config: config.Default().Subtitle, lastAlpha: -1}
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	for {
		select {
		case command := <-w.commands:
			if command.close {
				w32.ShowWindow(hwnd, w32.SW_HIDE)
				if command.reply != nil {
					command.reply <- nil
				}
				return
			}
			err := state.apply(command)
			if err != nil {
				w.handleRenderError(&state, err)
			} else {
				state.clearRenderFailure()
			}
			if command.reply != nil {
				command.reply <- err
			}
		case <-ticker.C:
			now := time.Now()
			if state.visible && (state.nextRenderAt.IsZero() || !now.Before(state.nextRenderAt)) {
				if err := state.renderCurrentFrame(); err != nil {
					w.handleRenderError(&state, err)
				} else {
					state.clearRenderFailure()
				}
			}
		}
		pumpNativeSubtitleMessages()
	}
}

func (s *nativeSubtitleState) apply(command nativeSubtitleCommand) error {
	if command.bounds != nil {
		s.bounds = scaleSubtitleBounds(*command.bounds, uint(w32.GetDpiForWindow(s.hwnd)))
	}
	if command.config != nil {
		s.config = *command.config
	}
	if command.event != nil {
		if command.event.RequestID < s.requestID || strings.TrimSpace(command.event.Text) == "" {
			return nil
		}
		s.requestID = command.event.RequestID
		s.text = command.event.Text
		s.persistent = command.event.Persistent
		s.startedAt = time.Now()
		s.hovering = false
		s.visible = true
		s.lastAlpha = -1
		s.renderFailures = 0
		s.nextRenderAt = time.Time{}
	}
	if s.visible {
		return s.renderCurrentFrame()
	}
	return nil
}

func (w *nativeSubtitleWindow) handleRenderError(state *nativeSubtitleState, err error) {
	now := time.Now()
	if w.renderErrorReporter != nil && (w.lastRenderErrorAt.IsZero() || now.Sub(w.lastRenderErrorAt) >= time.Second) {
		w.renderErrorReporter(err)
		w.lastRenderErrorAt = now
	}
	state.renderFailures++
	if state.renderFailures >= nativeSubtitleMaxFailures {
		state.visible = false
		state.text = ""
		state.nextRenderAt = time.Time{}
		w32.ShowWindow(state.hwnd, w32.SW_HIDE)
		return
	}
	state.nextRenderAt = now.Add(subtitleRenderBackoff(state.renderFailures))
}

func (s *nativeSubtitleState) clearRenderFailure() {
	s.renderFailures = 0
	s.nextRenderAt = time.Time{}
}

func subtitleRenderBackoff(failures int) time.Duration {
	if failures <= 0 {
		return nativeSubtitleInitialBackoff
	}
	backoff := nativeSubtitleInitialBackoff
	for attempt := 1; attempt < failures && backoff < nativeSubtitleMaxBackoff; attempt++ {
		backoff *= 2
	}
	if backoff > nativeSubtitleMaxBackoff {
		return nativeSubtitleMaxBackoff
	}
	return backoff
}

// Wails reports screen work areas in device-independent pixels. A layered
// Win32 window, however, is positioned in physical pixels. Without this
// conversion the subtitle is both too small and shifted toward the top-left
// on scaled displays.
func scaleSubtitleBounds(bounds Bounds, dpi uint) Bounds {
	scale := subtitleDPIScale(dpi)
	return Bounds{
		X:      int(math.Round(float64(bounds.X) * scale)),
		Y:      int(math.Round(float64(bounds.Y) * scale)),
		Width:  int(math.Round(float64(bounds.Width) * scale)),
		Height: int(math.Round(float64(bounds.Height) * scale)),
	}
}

func subtitleDPIScale(dpi uint) float64 {
	if dpi == 0 {
		dpi = 96
	}
	return float64(dpi) / 96
}

func scaleSubtitlePixel(value int, dpi uint) int {
	return int(math.Round(float64(value) * subtitleDPIScale(dpi)))
}

func (s *nativeSubtitleState) renderCurrentFrame() error {
	now := time.Now()
	if s.updateHover(now) {
		if s.lastAlpha == 1 {
			return nil
		}
		if err := s.render(1); err != nil {
			return err
		}
		s.lastAlpha = 1
		return nil
	}
	alpha, stillVisible := s.animationAlpha(now)
	if !stillVisible {
		s.visible = false
		s.text = ""
		w32.ShowWindow(s.hwnd, w32.SW_HIDE)
		return nil
	}
	if math.Abs(alpha-s.lastAlpha) < 0.001 && alpha == 1 {
		return nil
	}
	if err := s.render(alpha); err != nil {
		return err
	}
	s.lastAlpha = alpha
	return nil
}

func (s *nativeSubtitleState) animationAlpha(now time.Time) (float64, bool) {
	elapsed := now.Sub(s.startedAt)
	fadeIn := time.Duration(s.config.FadeInMS) * time.Millisecond
	display := time.Duration(s.config.DisplayMS) * time.Millisecond
	fadeOut := time.Duration(s.config.FadeOutMS) * time.Millisecond
	if elapsed < fadeIn && fadeIn > 0 {
		return float64(elapsed) / float64(fadeIn), true
	}
	if s.persistent {
		return 1, true
	}
	if elapsed < fadeIn+display {
		return 1, true
	}
	if elapsed < fadeIn+display+fadeOut && fadeOut > 0 {
		return 1 - float64(elapsed-fadeIn-display)/float64(fadeOut), true
	}
	return 0, false
}

func (s *nativeSubtitleState) updateHover(now time.Time) bool {
	x, y, ok := w32.GetCursorPos()
	hovered := ok && s.textBounds.Right > s.textBounds.Left &&
		x >= s.bounds.X+int(s.textBounds.Left) && x < s.bounds.X+int(s.textBounds.Right) &&
		y >= s.bounds.Y+int(s.textBounds.Top) && y < s.bounds.Y+int(s.textBounds.Bottom)
	if hovered == s.hovering {
		return hovered
	}
	s.hovering = hovered
	if !hovered {
		s.restartDisplayAfterHover(now)
	}
	return hovered
}

func (s *nativeSubtitleState) restartDisplayAfterHover(now time.Time) {
	// 移开文字后不恢复旧倒计时，而是从完整停留时间重新计算淡出。
	fadeIn := time.Duration(s.config.FadeInMS) * time.Millisecond
	s.startedAt = now.Add(-fadeIn)
	s.lastAlpha = -1
}

func (s *nativeSubtitleState) render(alpha float64) error {
	pixels, textBounds, err := s.rasterize(alpha, nativeSubtitleSupersample)
	if err != nil {
		return err
	}
	s.textBounds = downsampleSubtitleRect(textBounds, nativeSubtitleSupersample)
	return s.updateLayeredPixels(pixels)
}

func (s *nativeSubtitleState) renderPreviewPixels() ([]byte, error) {
	pixels, _, err := s.rasterize(1, nativeSubtitleSupersample)
	return pixels, err
}

func (s *nativeSubtitleState) rasterize(alpha float64, supersample int) ([]byte, w32.RECT, error) {
	if s.bounds.Width <= 0 || s.bounds.Height <= 0 {
		return nil, w32.RECT{}, fmt.Errorf("字幕窗口尺寸无效: %dx%d", s.bounds.Width, s.bounds.Height)
	}
	if supersample <= 0 {
		supersample = 1
	}
	raster := *s
	raster.bounds.Width *= supersample
	raster.bounds.Height *= supersample
	raster.dpi = s.renderDPI() * uint(supersample)
	pixelCount := int64(raster.bounds.Width) * int64(raster.bounds.Height)
	if pixelCount > nativeSubtitleMaxRasterPixels {
		return nil, w32.RECT{}, fmt.Errorf("字幕光栅尺寸过大: %dx%d", raster.bounds.Width, raster.bounds.Height)
	}

	bmi := w32.BITMAPINFO{BmiHeader: w32.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(w32.BITMAPINFOHEADER{})),
		BiWidth:       int32(raster.bounds.Width),
		BiHeight:      -int32(raster.bounds.Height),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: w32.BI_RGB,
	}}
	var bits unsafe.Pointer
	bitmap := w32.CreateDIBSection(0, &bmi, w32.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 {
		return nil, w32.RECT{}, fmt.Errorf("创建字幕位图失败: %w", windows.GetLastError())
	}
	defer w32.DeleteObject(w32.HGDIOBJ(bitmap))

	dc := w32.CreateCompatibleDC(0)
	defer w32.DeleteDC(dc)
	oldBitmap := w32.SelectObject(dc, w32.HGDIOBJ(bitmap))
	defer w32.SelectObject(dc, oldBitmap)

	sourcePixels := unsafe.Slice((*byte)(bits), int(pixelCount*4))
	clear(sourcePixels)
	textBounds, font := raster.drawText(dc)
	raster.textBounds = textBounds
	defer w32.DeleteObject(w32.HGDIOBJ(font))
	raster.applyAlpha(sourcePixels, alpha)
	return downsampleSubtitlePixels(sourcePixels, raster.bounds.Width, raster.bounds.Height, supersample), textBounds, nil
}

func (s *nativeSubtitleState) updateLayeredPixels(pixels []byte) error {
	pixelCount := int64(s.bounds.Width) * int64(s.bounds.Height)
	bmi := w32.BITMAPINFO{BmiHeader: w32.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(w32.BITMAPINFOHEADER{})),
		BiWidth:       int32(s.bounds.Width),
		BiHeight:      -int32(s.bounds.Height),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: w32.BI_RGB,
	}}
	var bits unsafe.Pointer
	bitmap := w32.CreateDIBSection(0, &bmi, w32.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 {
		return fmt.Errorf("创建字幕输出位图失败: %w", windows.GetLastError())
	}
	defer w32.DeleteObject(w32.HGDIOBJ(bitmap))

	dc := w32.CreateCompatibleDC(0)
	defer w32.DeleteDC(dc)
	oldBitmap := w32.SelectObject(dc, w32.HGDIOBJ(bitmap))
	defer w32.SelectObject(dc, oldBitmap)
	copy(unsafe.Slice((*byte)(bits), int(pixelCount*4)), pixels)

	screenDC := w32.GetDC(0)
	defer w32.ReleaseDC(0, screenDC)
	destination := w32.POINT{X: int32(s.bounds.X), Y: int32(s.bounds.Y)}
	size := w32.SIZE{CX: int32(s.bounds.Width), CY: int32(s.bounds.Height)}
	source := w32.POINT{}
	blend := w32.BLENDFUNCTION{BlendOp: w32.AC_SRC_OVER, SourceConstantAlpha: 255, AlphaFormat: w32.AC_SRC_ALPHA}
	w32.SetWindowPos(s.hwnd, w32.HWND_TOPMOST, s.bounds.X, s.bounds.Y, s.bounds.Width, s.bounds.Height, w32.SWP_NOACTIVATE|w32.SWP_SHOWWINDOW)
	if !w32.UpdateLayeredWindow(s.hwnd, screenDC, &destination, &size, dc, &source, 0, &blend, w32.ULW_ALPHA) {
		return fmt.Errorf("更新透明字幕窗口失败: %w", windows.GetLastError())
	}
	return nil
}

func (s *nativeSubtitleState) renderDPI() uint {
	if s.dpi != 0 {
		return s.dpi
	}
	return uint(w32.GetDpiForWindow(s.hwnd))
}

func downsampleSubtitlePixels(source []byte, sourceWidth int, sourceHeight int, factor int) []byte {
	if factor <= 1 {
		return append([]byte(nil), source...)
	}
	width := sourceWidth / factor
	height := sourceHeight / factor
	pixels := make([]byte, width*height*4)
	area := factor * factor
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var blue, green, red, alpha int
			for offsetY := 0; offsetY < factor; offsetY++ {
				for offsetX := 0; offsetX < factor; offsetX++ {
					sourceOffset := ((y*factor+offsetY)*sourceWidth + x*factor + offsetX) * 4
					blue += int(source[sourceOffset])
					green += int(source[sourceOffset+1])
					red += int(source[sourceOffset+2])
					alpha += int(source[sourceOffset+3])
				}
			}
			targetOffset := (y*width + x) * 4
			pixels[targetOffset] = byte(blue / area)
			pixels[targetOffset+1] = byte(green / area)
			pixels[targetOffset+2] = byte(red / area)
			pixels[targetOffset+3] = byte(alpha / area)
		}
	}
	return pixels
}

func downsampleSubtitleRect(rect w32.RECT, factor int) w32.RECT {
	return w32.RECT{
		Left:   rect.Left / int32(factor),
		Top:    rect.Top / int32(factor),
		Right:  (rect.Right + int32(factor-1)) / int32(factor),
		Bottom: (rect.Bottom + int32(factor-1)) / int32(factor),
	}
}

func (s *nativeSubtitleState) drawText(dc w32.HDC) (w32.RECT, w32.HFONT) {
	dpi := s.renderDPI()
	fontSize := scaleSubtitlePixel(s.config.FontSize, dpi)
	font := w32.CreateFontIndirect(&w32.LOGFONT{
		Height: -int32(fontSize),
		Weight: w32.FW_MEDIUM,
		// ClearType is designed for opaque backgrounds. Grayscale antialiasing
		// keeps glyph edges sharp after the bitmap receives per-pixel alpha.
		Quality: 4, // ANTIALIASED_QUALITY
		FaceName: func() [w32.LF_FACESIZE]uint16 {
			var name [w32.LF_FACESIZE]uint16
			copy(name[:], utf16.Encode([]rune(s.config.FontFamily)))
			return name
		}(),
	})
	oldFont := w32.SelectObject(dc, w32.HGDIOBJ(font))
	defer w32.SelectObject(dc, oldFont)

	text := utf16.Encode([]rune(s.text))
	stagePadding := subtitleStagePadding(s.bounds.Height, dpi)
	maxTextWidth := max(1, s.bounds.Width-2*stagePadding)
	measure := w32.RECT{Right: int32(maxTextWidth)}
	w32.DrawText(dc, text, len(text), &measure, w32.DT_CALCRECT|w32.DT_CENTER|w32.DT_WORDBREAK|w32.DT_NOPREFIX)
	lineHeight := max(fontSize*16/10, fontSize+scaleSubtitlePixel(4, dpi))
	maxTextHeight := lineHeight * s.config.MaxLines
	fullTextHeight := max(lineHeight, int(measure.Bottom-measure.Top))
	viewportHeight := min(maxTextHeight, fullTextHeight)
	textWidth := min(maxTextWidth, max(1, int(measure.Right-measure.Left)))
	viewport := w32.RECT{
		Left:   int32((s.bounds.Width - textWidth) / 2),
		Top:    int32((s.bounds.Height - viewportHeight) / 2),
		Right:  int32((s.bounds.Width + textWidth) / 2),
		Bottom: int32((s.bounds.Height + viewportHeight) / 2),
	}
	drawBounds := viewport
	flags := uint32(w32.DT_CENTER | w32.DT_VCENTER | w32.DT_WORDBREAK | w32.DT_NOPREFIX)
	if fullTextHeight > viewportHeight {
		offset := int(math.Round(s.subtitleScrollOffset(time.Now(), fullTextHeight-viewportHeight, dpi)))
		drawBounds.Top -= int32(offset)
		drawBounds.Bottom = drawBounds.Top + int32(fullTextHeight)
		flags = w32.DT_CENTER | w32.DT_TOP | w32.DT_WORDBREAK | w32.DT_NOPREFIX
	}

	w32.SetBkMode(dc, w32.TRANSPARENT)
	s.drawTextShadow(dc, text, drawBounds, dpi, flags)
	s.drawTextOutline(dc, text, drawBounds, dpi, flags)
	// 使用纯蓝色作为文字掩码，后续 applyAlpha 按用户颜色写入预乘像素。
	w32.SetTextColor(dc, w32.COLORREF(w32.RGB(0, 0, 255)))
	w32.DrawText(dc, text, len(text), &drawBounds, flags)
	s.textBounds = viewport
	return viewport, font
}

func (s *nativeSubtitleState) subtitleScrollOffset(now time.Time, overflow int, dpi uint) float64 {
	if overflow <= 0 || s.persistent {
		return 0
	}
	speed := float64(scaleSubtitlePixel(nativeSubtitleScrollSpeed, dpi))
	travel := time.Duration(float64(overflow) / speed * float64(time.Second))
	cycle := nativeSubtitleScrollTopPause + travel + nativeSubtitleScrollBottomPause
	if cycle <= 0 {
		return 0
	}
	elapsed := now.Sub(s.startedAt) % cycle
	if elapsed <= nativeSubtitleScrollTopPause {
		return 0
	}
	elapsed -= nativeSubtitleScrollTopPause
	if elapsed >= travel {
		return float64(overflow)
	}
	return float64(overflow) * float64(elapsed) / float64(travel)
}

// subtitleStagePadding mirrors the Vue overlay-stage padding:
// clamp(14px, 2.2vh, 28px), leaving a safe edge around the text.
func subtitleStagePadding(height int, dpi uint) int {
	return min(scaleSubtitlePixel(28, dpi), max(scaleSubtitlePixel(14, dpi), height*22/1000))
}

func (s *nativeSubtitleState) drawTextShadow(dc w32.HDC, text []uint16, textBounds w32.RECT, dpi uint, flags uint32) {
	shadowBounds := textBounds
	shadowOffset := int32(scaleSubtitlePixel(s.config.ShadowOffsetY, dpi))
	shadowBounds.Top += shadowOffset
	shadowBounds.Bottom += shadowOffset
	blur := scaleSubtitlePixel(s.config.ShadowBlur, dpi)
	steps := min(8, max(1, blur))
	for step := steps; step >= 0; step-- {
		distance := blur * step / steps
		intensity := byte(255 * (steps - step + 1) / (steps + 1))
		w32.SetTextColor(dc, w32.COLORREF(w32.RGB(intensity, 0, 0)))
		for _, offset := range subtitleShadowOffsets(distance) {
			rect := shadowBounds
			rect.Left += int32(offset.x)
			rect.Right += int32(offset.x)
			rect.Top += int32(offset.y)
			rect.Bottom += int32(offset.y)
			w32.DrawText(dc, text, len(text), &rect, flags)
		}
	}
}

type subtitlePixelOffset struct {
	x int
	y int
}

func subtitleShadowOffsets(distance int) []subtitlePixelOffset {
	if distance == 0 {
		return []subtitlePixelOffset{{}}
	}
	return []subtitlePixelOffset{
		{x: -distance}, {x: distance}, {y: -distance}, {y: distance},
		{x: -distance, y: -distance}, {x: distance, y: -distance},
		{x: -distance, y: distance}, {x: distance, y: distance},
	}
}

func (s *nativeSubtitleState) drawTextOutline(dc w32.HDC, text []uint16, textBounds w32.RECT, dpi uint, flags uint32) {
	width := scaleSubtitlePixel(s.config.OutlineWidth, dpi)
	if width <= 0 {
		return
	}
	innerRadius := max(0, width-2)
	w32.SetTextColor(dc, w32.COLORREF(w32.RGB(0, 255, 0)))
	for y := -width; y <= width; y++ {
		for x := -width; x <= width; x++ {
			distanceSquared := x*x + y*y
			if distanceSquared > width*width || distanceSquared < innerRadius*innerRadius {
				continue
			}
			rect := textBounds
			rect.Left += int32(x)
			rect.Right += int32(x)
			rect.Top += int32(y)
			rect.Bottom += int32(y)
			w32.DrawText(dc, text, len(text), &rect, flags)
		}
	}
}

// applyAlpha converts color-coded GDI masks to premultiplied BGRA expected by
// UpdateLayeredWindow. The transparent bitmap only contains text, its outline,
// and its black shadow; no card, border, or background is painted.
func (s *nativeSubtitleState) applyAlpha(pixels []byte, animationAlpha float64) {
	textRed, textGreen, textBlue := subtitleHexColor(s.config.TextColor)
	outlineRed, outlineGreen, outlineBlue := subtitleHexColor(s.config.OutlineColor)
	for offset := 0; offset < len(pixels); offset += 4 {
		pixel := offset / 4
		x := pixel % s.bounds.Width
		y := pixel / s.bounds.Width
		if x < int(s.textBounds.Left) || x >= int(s.textBounds.Right) || y < int(s.textBounds.Top) || y >= int(s.textBounds.Bottom) {
			pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = 0, 0, 0, 0
			continue
		}
		blue, green, red := pixels[offset], pixels[offset+1], pixels[offset+2]
		switch {
		case blue != 0:
			writeSubtitlePixel(pixels, offset, float64(blue)/255, animationAlpha, textRed, textGreen, textBlue)
		case green != 0:
			writeSubtitlePixel(pixels, offset, float64(green)/255, animationAlpha, outlineRed, outlineGreen, outlineBlue)
		case red != 0:
			writeSubtitlePixel(pixels, offset, float64(red)/255, animationAlpha*s.config.ShadowOpacity, 0, 0, 0)
		}
	}
}

func writeSubtitlePixel(pixels []byte, offset int, coverage float64, opacity float64, red byte, green byte, blue byte) {
	alpha := byte(math.Round(min(1, coverage*opacity) * 255))
	pixels[offset] = byte(int(blue) * int(alpha) / 255)
	pixels[offset+1] = byte(int(green) * int(alpha) / 255)
	pixels[offset+2] = byte(int(red) * int(alpha) / 255)
	pixels[offset+3] = alpha
}

func subtitleHexColor(value string) (byte, byte, byte) {
	value = strings.TrimSpace(value)
	return subtitleHexByte(value[1], value[2]), subtitleHexByte(value[3], value[4]), subtitleHexByte(value[5], value[6])
}

func subtitleHexByte(high byte, low byte) byte {
	return subtitleHexNibble(high)<<4 | subtitleHexNibble(low)
}

func subtitleHexNibble(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10
	default:
		return 0
	}
}

func createNativeSubtitleWindow() (w32.HWND, error) {
	className, _ := syscall.UTF16PtrFromString(nativeSubtitleClassName)
	class := w32.WNDCLASSEX{
		Size:      uint32(unsafe.Sizeof(w32.WNDCLASSEX{})),
		WndProc:   syscall.NewCallback(nativeSubtitleWndProc),
		Instance:  w32.GetModuleHandle(""),
		ClassName: className,
	}
	if w32.RegisterClassEx(&class) == 0 {
		return 0, fmt.Errorf("注册字幕窗口类失败: %w", windows.GetLastError())
	}
	exStyle := uint(w32.WS_EX_LAYERED | w32.WS_EX_TOOLWINDOW | w32.WS_EX_TOPMOST | w32.WS_EX_NOACTIVATE)
	hwnd := w32.CreateWindowEx(exStyle, className, className, w32.WS_POPUP, 0, 0, 1, 1, 0, 0, class.Instance, nil)
	if hwnd == 0 {
		return 0, fmt.Errorf("创建字幕窗口失败: %w", windows.GetLastError())
	}
	return hwnd, nil
}

func nativeSubtitleWndProc(hwnd w32.HWND, message uint32, wParam, lParam uintptr) uintptr {
	if message == w32.WM_NCHITTEST {
		return ^uintptr(0) // HTTRANSPARENT (-1)
	}
	return w32.DefWindowProc(hwnd, message, wParam, lParam)
}

func pumpNativeSubtitleMessages() {
	for {
		var message w32.MSG
		if !w32.PeekMessage(&message, 0, 0, 0, w32.PM_REMOVE) {
			return
		}
		w32.TranslateMessage(&message)
		w32.DispatchMessage(&message)
	}
}
