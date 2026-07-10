// Package translator 提供文本翻译接口和 Eino 实现。
package translator

import (
	"context"
	"time"
)

// Result 是一次翻译的结果和元数据。
type Result struct {
	Text       string
	Model      string
	DurationMS int64
}

// Translator 定义文本翻译能力。
type Translator interface {
	Translate(ctx context.Context, text string) (Result, error)
}

// DurationMS 将持续时间转换为毫秒。
func DurationMS(duration time.Duration) int64 {
	return duration.Milliseconds()
}
