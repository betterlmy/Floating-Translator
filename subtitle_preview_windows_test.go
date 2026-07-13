//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"floating-translator/internal/config"
)

func TestRenderSubtitlePreviewProducesSizedTransparentPNG(t *testing.T) {
	preview, err := renderSubtitlePreview(config.Default().Subtitle, 640, 158, 1.5)
	if err != nil {
		t.Fatalf("renderSubtitlePreview() error = %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(preview, prefix) {
		t.Fatalf("预览前缀 = %q", preview[:min(len(preview), len(prefix))])
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, prefix))
	if err != nil {
		t.Fatalf("解码 PNG 数据失败: %v", err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("解析 PNG 数据失败: %v", err)
	}
	if image.Bounds().Dx() != 960 || image.Bounds().Dy() != 237 {
		t.Fatalf("预览尺寸 = %dx%d", image.Bounds().Dx(), image.Bounds().Dy())
	}

	nonTransparent := 0
	for y := image.Bounds().Min.Y; y < image.Bounds().Max.Y; y++ {
		for x := image.Bounds().Min.X; x < image.Bounds().Max.X; x++ {
			_, _, _, alpha := image.At(x, y).RGBA()
			if alpha != 0 {
				nonTransparent++
			}
		}
	}
	if nonTransparent == 0 {
		t.Fatal("预览 PNG 不应完全透明")
	}
}
