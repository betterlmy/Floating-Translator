//go:build !windows

package main

import "floating-translator/internal/config"

func renderSubtitlePreview(config.SubtitleConfig, int, int, float64) (string, error) {
	return "", nil
}
