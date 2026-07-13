//go:build windows

package subtitle

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"

	"floating-translator/internal/config"
)

const subtitlePreviewText = "翻译结果将在这里清晰呈现"

// RenderPreview renders the same GDI masks used by the live layered
// window, then serialises them as a transparent PNG for the settings view.
func RenderPreview(subtitle config.SubtitleConfig, width int, height int, deviceScale float64) (string, error) {
	subtitle = previewSubtitleConfig(subtitle)
	deviceScale = min(4, max(1, deviceScale))
	width = min(1920, max(160, int(math.Round(float64(width)*deviceScale))))
	height = min(600, max(100, int(math.Round(float64(height)*deviceScale))))
	state := nativeSubtitleState{
		bounds: Bounds{Width: width, Height: height},
		dpi:    uint(math.Round(96 * deviceScale)),
		config: subtitle,
		text:   subtitlePreviewText,
	}
	pixels, err := state.renderPreviewPixels()
	if err != nil {
		return "", err
	}

	preview := image.NewRGBA(image.Rect(0, 0, width, height))
	for offset := 0; offset < len(pixels); offset += 4 {
		previewOffset := offset
		preview.Pix[previewOffset] = pixels[offset+2]
		preview.Pix[previewOffset+1] = pixels[offset+1]
		preview.Pix[previewOffset+2] = pixels[offset]
		preview.Pix[previewOffset+3] = pixels[offset+3]
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, preview); err != nil {
		return "", fmt.Errorf("编码字幕预览失败: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

func previewSubtitleConfig(subtitle config.SubtitleConfig) config.SubtitleConfig {
	defaults := config.Default().Subtitle
	if subtitle.FontFamily == "" {
		subtitle.FontFamily = defaults.FontFamily
	}
	subtitle.FontSize = min(96, max(12, subtitle.FontSize))
	subtitle.MaxLines = min(10, max(1, subtitle.MaxLines))
	subtitle.OutlineWidth = min(6, max(0, subtitle.OutlineWidth))
	subtitle.ShadowOffsetY = min(24, max(0, subtitle.ShadowOffsetY))
	subtitle.ShadowBlur = min(32, max(0, subtitle.ShadowBlur))
	if subtitle.ShadowOpacity < 0 || subtitle.ShadowOpacity > 1 {
		subtitle.ShadowOpacity = defaults.ShadowOpacity
	}
	if !subtitlePreviewHexColor(subtitle.TextColor) {
		subtitle.TextColor = defaults.TextColor
	}
	if !subtitlePreviewHexColor(subtitle.OutlineColor) {
		subtitle.OutlineColor = defaults.OutlineColor
	}
	return subtitle
}

func subtitlePreviewHexColor(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}
