//go:build windows

package main

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
	nativeSubtitleClassName  = "FloatingTranslatorNativeSubtitle"
	nativeSubtitlePaddingX   = 24
	nativeSubtitlePaddingY   = 14
	nativeSubtitleRadius     = 12
	nativeSubtitleShadowY    = 14
	nativeSubtitleShadowBlur = 30
)

// nativeSubtitleWindow is a dedicated layered Win32 window. Unlike a WebView
// window, UpdateLayeredWindow owns every pixel's alpha channel, so the area
// outside the subtitle card remains genuinely transparent.
type nativeSubtitleWindow struct {
	commands chan nativeSubtitleCommand
	done     chan struct{}
	close    sync.Once
}

type nativeSubtitleCommand struct {
	bounds *windowBounds
	config *config.SubtitleConfig
	event  *processor.Event
	close  bool
	reply  chan error
}

type nativeSubtitleState struct {
	hwnd         w32.HWND
	bounds       windowBounds
	config       config.SubtitleConfig
	text         string
	requestID    uint64
	startedAt    time.Time
	pausedFor    time.Duration
	hoverStarted time.Time
	hovering     bool
	visible      bool
	lastAlpha    float64
	card         w32.RECT
}

func newNativeSubtitleWindow() (*nativeSubtitleWindow, error) {
	window := &nativeSubtitleWindow{
		commands: make(chan nativeSubtitleCommand),
		done:     make(chan struct{}),
	}
	ready := make(chan error, 1)
	go window.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return window, nil
}

func (w *nativeSubtitleWindow) Configure(bounds windowBounds, cfg config.SubtitleConfig) error {
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
			if command.reply != nil {
				command.reply <- err
			}
		case <-ticker.C:
			if state.visible {
				_ = state.renderCurrentFrame()
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
		s.startedAt = time.Now()
		s.pausedFor = 0
		s.hoverStarted = time.Time{}
		s.hovering = false
		s.visible = true
		s.lastAlpha = -1
	}
	if s.visible {
		return s.renderCurrentFrame()
	}
	return nil
}

// Wails reports screen work areas in device-independent pixels. A layered
// Win32 window, however, is positioned in physical pixels. Without this
// conversion the subtitle is both too small and shifted toward the top-left
// on scaled displays.
func scaleSubtitleBounds(bounds windowBounds, dpi uint) windowBounds {
	scale := subtitleDPIScale(dpi)
	return windowBounds{
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
	elapsed := now.Sub(s.startedAt) - s.pausedFor
	fadeIn := time.Duration(s.config.FadeInMS) * time.Millisecond
	display := time.Duration(s.config.DisplayMS) * time.Millisecond
	fadeOut := time.Duration(s.config.FadeOutMS) * time.Millisecond
	if elapsed < fadeIn && fadeIn > 0 {
		return float64(elapsed) / float64(fadeIn), true
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
	hovered := ok && s.card.Right > s.card.Left &&
		x >= s.bounds.X+int(s.card.Left) && x < s.bounds.X+int(s.card.Right) &&
		y >= s.bounds.Y+int(s.card.Top) && y < s.bounds.Y+int(s.card.Bottom)
	if hovered && !s.hovering {
		s.hovering = true
		s.hoverStarted = now
	}
	if !hovered && s.hovering {
		s.pausedFor += now.Sub(s.hoverStarted)
		s.hovering = false
		s.hoverStarted = time.Time{}
	}
	return s.hovering
}

func (s *nativeSubtitleState) render(alpha float64) error {
	if s.bounds.Width <= 0 || s.bounds.Height <= 0 {
		return fmt.Errorf("字幕窗口尺寸无效: %dx%d", s.bounds.Width, s.bounds.Height)
	}
	pixelCount := int64(s.bounds.Width) * int64(s.bounds.Height)
	if pixelCount > 40_000_000 {
		return fmt.Errorf("字幕窗口尺寸过大: %dx%d", s.bounds.Width, s.bounds.Height)
	}

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
		return fmt.Errorf("创建字幕位图失败: %w", windows.GetLastError())
	}
	defer w32.DeleteObject(w32.HGDIOBJ(bitmap))

	dc := w32.CreateCompatibleDC(0)
	defer w32.DeleteDC(dc)
	oldBitmap := w32.SelectObject(dc, w32.HGDIOBJ(bitmap))
	defer w32.SelectObject(dc, oldBitmap)

	pixels := unsafe.Slice((*byte)(bits), int(pixelCount*4))
	clear(pixels)
	card, font := s.drawCardAndText(dc)
	s.card = card
	defer w32.DeleteObject(w32.HGDIOBJ(font))
	s.applyAlpha(pixels, card, alpha)

	screenDC := w32.GetDC(0)
	defer w32.ReleaseDC(0, screenDC)
	destination := w32.POINT{X: int32(s.bounds.X), Y: int32(s.bounds.Y)}
	size := w32.SIZE{CX: int32(s.bounds.Width), CY: int32(s.bounds.Height)}
	source := w32.POINT{}
	blend := w32.BLENDFUNCTION{BlendOp: w32.AC_SRC_OVER, SourceConstantAlpha: 255, AlphaFormat: w32.AC_SRC_ALPHA}
	// Position the popup before committing its alpha bitmap. Calling
	// SetWindowPos or ShowWindow afterwards can make Windows repaint a portion
	// of the client area with the default (white) background.
	w32.SetWindowPos(s.hwnd, w32.HWND_TOPMOST, s.bounds.X, s.bounds.Y, s.bounds.Width, s.bounds.Height, w32.SWP_NOACTIVATE|w32.SWP_SHOWWINDOW)
	if !w32.UpdateLayeredWindow(s.hwnd, screenDC, &destination, &size, dc, &source, 0, &blend, w32.ULW_ALPHA) {
		return fmt.Errorf("更新透明字幕窗口失败: %w", windows.GetLastError())
	}
	return nil
}

func (s *nativeSubtitleState) drawCardAndText(dc w32.HDC) (w32.RECT, w32.HFONT) {
	dpi := uint(w32.GetDpiForWindow(s.hwnd))
	fontSize := scaleSubtitlePixel(s.config.FontSize, dpi)
	paddingX := scaleSubtitlePixel(nativeSubtitlePaddingX, dpi)
	paddingY := scaleSubtitlePixel(nativeSubtitlePaddingY, dpi)
	font := w32.CreateFontIndirect(&w32.LOGFONT{
		Height: -int32(fontSize),
		Weight: w32.FW_MEDIUM,
		// ClearType is designed for an opaque background. Grayscale antialiasing
		// keeps glyph edges sharp after this bitmap receives per-pixel alpha.
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
	cardWidth := max(1, s.bounds.Width-2*stagePadding)
	maxTextWidth := max(1, cardWidth-2*paddingX)
	measure := w32.RECT{Right: int32(maxTextWidth)}
	w32.DrawText(dc, text, len(text), &measure, w32.DT_CALCRECT|w32.DT_CENTER|w32.DT_WORDBREAK|w32.DT_NOPREFIX)
	lineHeight := max(fontSize*16/10, fontSize+scaleSubtitlePixel(4, dpi))
	maxTextHeight := lineHeight * s.config.MaxLines
	textHeight := min(maxTextHeight, max(lineHeight, int(measure.Bottom-measure.Top)))
	cardHeight := min(s.bounds.Height, textHeight+2*paddingY)
	card := w32.RECT{
		Left:   int32((s.bounds.Width - cardWidth) / 2),
		Top:    int32((s.bounds.Height - cardHeight) / 2),
		Right:  int32((s.bounds.Width + cardWidth) / 2),
		Bottom: int32((s.bounds.Height + cardHeight) / 2),
	}
	brush := w32.CreateSolidBrush(w32.COLORREF(w32.RGB(17, 22, 27)))
	w32.FillRect(dc, &card, brush)
	w32.DeleteObject(w32.HGDIOBJ(brush))

	textRect := w32.RECT{
		Left:   card.Left + int32(paddingX),
		Top:    card.Top + int32(paddingY),
		Right:  card.Right - int32(paddingX),
		Bottom: card.Bottom - int32(paddingY),
	}
	w32.SetBkMode(dc, w32.TRANSPARENT)
	w32.SetTextColor(dc, w32.COLORREF(w32.RGB(248, 250, 252)))
	w32.DrawText(dc, text, len(text), &textRect, w32.DT_CENTER|w32.DT_VCENTER|w32.DT_WORDBREAK|w32.DT_NOPREFIX)
	return card, font
}

// subtitleStagePadding mirrors the original .overlay-stage CSS padding:
// clamp(14px, 2.2vh, 28px). The card then spans the available stage width,
// rather than shrinking to the width of a short translation.
func subtitleStagePadding(height int, dpi uint) int {
	return min(scaleSubtitlePixel(28, dpi), max(scaleSubtitlePixel(14, dpi), height*22/1000))
}

// applyAlpha converts the GDI bitmap to premultiplied BGRA expected by
// UpdateLayeredWindow. GDI does not populate the alpha byte itself, so this
// is also where the native renderer recreates the web subtitle card's rounded
// corners, subtle gradient, border and shadow.
func (s *nativeSubtitleState) applyAlpha(pixels []byte, card w32.RECT, animationAlpha float64) {
	s.drawShadow(pixels, card, animationAlpha)
	radius := scaleSubtitlePixel(nativeSubtitleRadius, uint(w32.GetDpiForWindow(s.hwnd)))

	topOpacity := min(1, s.config.BackgroundOpacity+0.08)
	for y := int(card.Top); y < int(card.Bottom); y++ {
		for x := int(card.Left); x < int(card.Right); x++ {
			offset := (y*s.bounds.Width + x) * 4
			blue, green, red := pixels[offset], pixels[offset+1], pixels[offset+2]
			if roundedRectDistance(x, y, card, radius) > 0 {
				continue
			}
			if red > 80 && green > 80 && blue > 80 {
				coverage := float64(max(int(red), max(int(green), int(blue)))-17) / float64(255-17)
				coverage = min(1, max(0, coverage))
				textAlpha := byte(math.Round(coverage * animationAlpha * 255))
				pixels[offset] = byte(int(252) * int(textAlpha) / 255)
				pixels[offset+1] = byte(int(250) * int(textAlpha) / 255)
				pixels[offset+2] = byte(int(248) * int(textAlpha) / 255)
				pixels[offset+3] = textAlpha
				continue
			}

			ratio := float64(y-int(card.Top)) / float64(max(1, int(card.Bottom-card.Top)-1))
			opacity := topOpacity + (s.config.BackgroundOpacity-topOpacity)*ratio
			backgroundAlpha := opacity * animationAlpha
			redValue := 17 + (4-17)*ratio
			greenValue := 22 + (7-22)*ratio
			blueValue := 27 + (10-27)*ratio

			if roundedRectBorder(x, y, card, radius) {
				// The CSS card had a very light one-pixel outline. Composite that
				// over the gradient rather than using an opaque GDI border.
				borderAlpha := 0.09 * animationAlpha
				redValue = redValue*(1-borderAlpha) + 255*borderAlpha
				greenValue = greenValue*(1-borderAlpha) + 255*borderAlpha
				blueValue = blueValue*(1-borderAlpha) + 255*borderAlpha
				backgroundAlpha += borderAlpha * (1 - backgroundAlpha)
			}

			pixels[offset] = byte(math.Round(blueValue * backgroundAlpha))
			pixels[offset+1] = byte(math.Round(greenValue * backgroundAlpha))
			pixels[offset+2] = byte(math.Round(redValue * backgroundAlpha))
			pixels[offset+3] = byte(math.Round(backgroundAlpha * 255))
		}
	}
}

func (s *nativeSubtitleState) drawShadow(pixels []byte, card w32.RECT, animationAlpha float64) {
	dpi := uint(w32.GetDpiForWindow(s.hwnd))
	shadowY := scaleSubtitlePixel(nativeSubtitleShadowY, dpi)
	shadowBlur := scaleSubtitlePixel(nativeSubtitleShadowBlur, dpi)
	radius := scaleSubtitlePixel(nativeSubtitleRadius, dpi)
	shadow := card
	shadow.Top += int32(shadowY)
	shadow.Bottom += int32(shadowY)
	left := max(0, int(shadow.Left)-shadowBlur)
	right := min(s.bounds.Width, int(shadow.Right)+shadowBlur)
	top := max(0, int(shadow.Top)-shadowBlur)
	bottom := min(s.bounds.Height, int(shadow.Bottom)+shadowBlur)

	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			if roundedRectDistance(x, y, card, radius) == 0 {
				continue
			}
			distance := roundedRectDistance(x, y, shadow, radius)
			if distance <= 0 || distance >= float64(shadowBlur) {
				continue
			}
			strength := 1 - distance/float64(shadowBlur)
			alpha := byte(math.Round(0.32 * animationAlpha * strength * strength * 255))
			offset := (y*s.bounds.Width + x) * 4
			pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = 0, 0, 0, alpha
		}
	}
}

// roundedRectDistance returns zero inside a rounded rectangle and the pixel
// distance outside it. Coordinates deliberately use pixel centres so corners
// stay smooth when their alpha is composited by UpdateLayeredWindow.
func roundedRectDistance(x, y int, rect w32.RECT, radius int) float64 {
	left, top := float64(rect.Left), float64(rect.Top)
	right, bottom := float64(rect.Right-1), float64(rect.Bottom-1)
	px, py := float64(x), float64(y)
	r := float64(min(radius, min(int((right-left+1)/2), int((bottom-top+1)/2))))
	innerLeft, innerRight := left+r, right-r
	innerTop, innerBottom := top+r, bottom-r
	nearestX := min(max(px, innerLeft), innerRight)
	nearestY := min(max(py, innerTop), innerBottom)
	return max(0, math.Hypot(px-nearestX, py-nearestY)-r)
}

func roundedRectBorder(x, y int, rect w32.RECT, radius int) bool {
	if roundedRectDistance(x, y, rect, radius) > 0 {
		return false
	}
	inner := rect
	inner.Left++
	inner.Top++
	inner.Right--
	inner.Bottom--
	return inner.Right > inner.Left && inner.Bottom > inner.Top && roundedRectDistance(x, y, inner, max(1, radius-1)) > 0
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
