// Package logger 提供应用统一的结构化日志封装。
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"floating-translator/internal/filter"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const maxSanitizedErrorRunes = 512

var errorURLQueryPattern = regexp.MustCompile(`(?i)(https?://[^\s?]+)\?[^\s]+`)

// Field 是结构化日志字段。
type Field = zap.Field

// Logger 是应用统一日志器。
type Logger struct {
	base   *zap.Logger
	level  zap.AtomicLevel
	writer *rotatingWriter
}

type rotatingWriter struct {
	mutex  sync.RWMutex
	logger *lumberjack.Logger
}

func (w *rotatingWriter) Write(data []byte) (int, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.logger.Write(data)
}

func (w *rotatingWriter) Sync() error { return nil }

func (w *rotatingWriter) Reconfigure(maxSizeMB int, maxBackups int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.logger.MaxSize = maxSizeMB
	w.logger.MaxBackups = maxBackups
}

// New 创建写入滚动文件的日志器。
func New(path string, levelName string, maxSizeMB int, maxBackups int) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	level, err := parseLevel(levelName)
	if err != nil {
		return nil, err
	}
	atomicLevel := zap.NewAtomicLevelAt(level)
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	rotatingWriter := &rotatingWriter{logger: &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     7,
		Compress:   true,
	}}
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(rotatingWriter), atomicLevel)
	return &Logger{base: zap.New(core, zap.AddCaller()), level: atomicLevel, writer: rotatingWriter}, nil
}

// NewNop 创建不会输出内容的日志器，供测试和启动降级使用。
func NewNop() *Logger {
	return &Logger{base: zap.NewNop(), level: zap.NewAtomicLevelAt(zapcore.InfoLevel)}
}

// SetLevel 动态修改日志级别。
func (l *Logger) SetLevel(levelName string) error {
	level, err := parseLevel(levelName)
	if err != nil {
		return err
	}
	l.level.SetLevel(level)
	return nil
}

// Reconfigure 更新日志轮转参数，不需要替换调用方持有的 Logger 指针。
func (l *Logger) Reconfigure(maxSizeMB int, maxBackups int) error {
	if maxSizeMB <= 0 || maxBackups < 0 {
		return fmt.Errorf("无效日志轮转参数: size=%dMB backups=%d", maxSizeMB, maxBackups)
	}
	if l.writer != nil {
		l.writer.Reconfigure(maxSizeMB, maxBackups)
	}
	return nil
}

// Debug 记录调试日志。
func (l *Logger) Debug(message string, fields ...Field) {
	l.base.Debug(message, fields...)
}

// Info 记录信息日志。
func (l *Logger) Info(message string, fields ...Field) {
	l.base.Info(message, fields...)
}

// Warn 记录警告日志。
func (l *Logger) Warn(message string, fields ...Field) {
	l.base.Warn(message, fields...)
}

// Error 记录错误日志。
func (l *Logger) Error(message string, fields ...Field) {
	l.base.Error(message, fields...)
}

// Sync 刷新日志缓冲区。
func (l *Logger) Sync() error {
	return l.base.Sync()
}

// String 创建字符串字段。
func String(key string, value string) Field { return zap.String(key, value) }

// Int 创建整数型字段。
func Int(key string, value int) Field { return zap.Int(key, value) }

// Int64 创建 int64 字段。
func Int64(key string, value int64) Field { return zap.Int64(key, value) }

// Uint64 创建 uint64 字段。
func Uint64(key string, value uint64) Field { return zap.Uint64(key, value) }

// Bool 创建布尔字段。
func Bool(key string, value bool) Field { return zap.Bool(key, value) }

// ErrorField 创建经过脱敏和长度限制的错误字段。
func ErrorField(err error) Field { return zap.String("error", SanitizeError(err)) }

// SanitizeError 清理第三方错误中的凭据、URL 查询参数和过长诊断内容。
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := filter.RedactSensitive(err.Error())
	message = errorURLQueryPattern.ReplaceAllString(message, "$1?[REDACTED]")
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > maxSanitizedErrorRunes {
		return string(runes[:maxSanitizedErrorRunes]) + "…"
	}
	return message
}

// Any 创建任意类型字段。
func Any(key string, value any) Field { return zap.Any(key, value) }

func parseLevel(value string) (zapcore.Level, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return zapcore.InfoLevel, fmt.Errorf("无效日志级别 %q: %w", value, err)
	}
	return level, nil
}
