//go:build !windows

package subtitle

import "floating-translator/internal/config"

// RenderPreview 在不支持原生预览的平台返回明确错误。
func RenderPreview(config.SubtitleConfig, int, int, float64) (string, error) {
	return "", nil
}
