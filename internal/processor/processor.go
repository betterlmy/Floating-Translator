// Package processor 负责剪切板文本去重、过滤和最新请求优先调度。
package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"floating-translator/internal/filter"
	"floating-translator/internal/logger"
	"floating-translator/internal/translator"
)

// Event 是发送给字幕前端的翻译事件。
type Event struct {
	RequestID   uint64 `json:"request_id"`
	Text        string `json:"text"`
	Source      string `json:"source"`
	TimestampMS int64  `json:"timestamp_ms"`
}

// Emitter 接收最新翻译结果。
type Emitter func(Event)

// Processor 管理过滤器、翻译器和进行中的请求。
type Processor struct {
	rootContext context.Context
	logger      *logger.Logger
	emit        Emitter

	mutex             sync.Mutex
	filter            *filter.TextFilter
	translator        translator.Translator
	timeout           time.Duration
	includeSourceText bool
	enabled           bool
	sequence          uint64
	lastObservedHash  string
	currentCancel     context.CancelFunc
	currentSource     string
}

// New 创建翻译处理器。
func New(ctx context.Context, appLogger *logger.Logger, emit Emitter) *Processor {
	return &Processor{rootContext: ctx, logger: appLogger, emit: emit}
}

// Configure 原子替换过滤和翻译配置，并取消旧请求。
func (p *Processor) Configure(
	textFilter *filter.TextFilter,
	textTranslator translator.Translator,
	timeout time.Duration,
	includeSourceText bool,
	enabled bool,
) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.cancelCurrentLocked()
	p.sequence++
	p.filter = textFilter
	p.translator = textTranslator
	p.timeout = timeout
	p.includeSourceText = includeSourceText
	p.enabled = enabled
	p.lastObservedHash = ""
}

// SetEnabled 暂停或恢复翻译处理。
func (p *Processor) SetEnabled(enabled bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.enabled == enabled {
		return
	}
	p.enabled = enabled
	if p.currentSource == "clipboard" {
		p.sequence++
		p.cancelCurrentLocked()
	}
}

// Handle 处理一条最新剪切板文本。
func (p *Processor) Handle(rawText string) {
	normalizedText := filter.Normalize(rawText)
	textHash := hashText(normalizedText)

	p.mutex.Lock()
	if textHash == p.lastObservedHash {
		p.mutex.Unlock()
		p.logger.Debug("跳过重复剪切板文本", logger.String("reason", string(filter.ReasonDuplicate)), logger.String("text_hash", shortHash(textHash)))
		return
	}
	p.lastObservedHash = textHash
	if p.currentSource == "selection" {
		p.mutex.Unlock()
		p.logger.Debug("划词翻译进行中，跳过剪切板文本", logger.String("text_hash", shortHash(textHash)))
		return
	}
	p.sequence++
	requestID := p.sequence
	p.cancelCurrentLocked()
	textFilter := p.filter
	textTranslator := p.translator
	timeout := p.timeout
	includeSourceText := p.includeSourceText
	enabled := p.enabled
	p.mutex.Unlock()

	if !enabled || textFilter == nil || textTranslator == nil {
		return
	}
	result := textFilter.Evaluate(normalizedText)
	logFields := []logger.Field{
		logger.Uint64("request_id", requestID),
		logger.String("text_hash", shortHash(textHash)),
		logger.Int("text_length", len([]rune(normalizedText))),
		logger.String("reason", string(result.Reason)),
	}
	if includeSourceText {
		logFields = append(logFields, logger.String("source_text", filter.RedactSensitive(normalizedText)))
	}
	if !result.ShouldTranslate {
		p.logger.Info("跳过剪切板文本", logFields...)
		return
	}

	requestContext, cancel := context.WithTimeout(p.rootContext, timeout)
	p.mutex.Lock()
	if requestID != p.sequence || !p.enabled {
		p.mutex.Unlock()
		cancel()
		return
	}
	p.currentCancel = cancel
	p.currentSource = "clipboard"
	p.mutex.Unlock()

	p.logger.Info("开始翻译", logFields...)
	go p.translate(requestContext, cancel, requestID, textHash, result.Text, textTranslator, "clipboard", false)
}

// HandleSelection 直接翻译 UI Automation 读取到的选中文本，不经过剪切板过滤规则。
func (p *Processor) HandleSelection(rawText string) {
	normalizedText := filter.Normalize(rawText)
	textHash := hashText(normalizedText)

	p.mutex.Lock()
	p.sequence++
	requestID := p.sequence
	p.cancelCurrentLocked()
	textTranslator := p.translator
	timeout := p.timeout
	includeSourceText := p.includeSourceText
	if textTranslator == nil {
		p.mutex.Unlock()
		p.EmitMessage("selection", "划词翻译不可用：翻译器尚未完成配置")
		return
	}
	requestContext, cancel := context.WithTimeout(p.rootContext, timeout)
	p.currentCancel = cancel
	p.currentSource = "selection"
	p.mutex.Unlock()

	logFields := []logger.Field{
		logger.Uint64("request_id", requestID),
		logger.String("source", "selection"),
		logger.String("text_hash", shortHash(textHash)),
		logger.Int("text_length", len([]rune(normalizedText))),
	}
	if includeSourceText {
		logFields = append(logFields, logger.String("source_text", filter.RedactSensitive(normalizedText)))
	}
	p.logger.Info("开始划词翻译", logFields...)
	go p.translate(requestContext, cancel, requestID, textHash, normalizedText, textTranslator, "selection", true)
}

// EmitMessage 发送一条具有单调请求编号的字幕状态，并取消旧请求。
func (p *Processor) EmitMessage(source string, text string) {
	p.mutex.Lock()
	p.sequence++
	requestID := p.sequence
	p.cancelCurrentLocked()
	p.mutex.Unlock()
	p.emitEvent(requestID, text, source)
}

// Stop 取消当前请求并禁用处理器。
func (p *Processor) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.enabled = false
	p.sequence++
	p.cancelCurrentLocked()
}

func (p *Processor) translate(
	ctx context.Context,
	cancel context.CancelFunc,
	requestID uint64,
	textHash string,
	text string,
	textTranslator translator.Translator,
	source string,
	alwaysEmit bool,
) {
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			p.logger.Error("翻译协程发生 panic",
				logger.Uint64("request_id", requestID),
				logger.Any("panic", recovered),
				logger.String("stack", string(debug.Stack())),
			)
			if p.markFailed(requestID, textHash, source) && alwaysEmit {
				p.emitEvent(requestID, "划词翻译失败：翻译任务异常终止", source)
			}
		}
	}()

	result, err := textTranslator.Translate(ctx, text)
	if err != nil {
		if ctx.Err() == context.Canceled {
			p.logger.Debug("翻译请求已取消", logger.Uint64("request_id", requestID))
			return
		}
		p.logger.Error("翻译请求失败",
			logger.Uint64("request_id", requestID),
			logger.String("text_hash", shortHash(textHash)),
			logger.ErrorField(err),
		)
		if p.markFailed(requestID, textHash, source) && alwaysEmit {
			message := "划词翻译失败：请检查模型配置和网络连接"
			if ctx.Err() == context.DeadlineExceeded {
				message = "划词翻译超时：请检查网络连接或增大 llm.timeout_seconds"
			}
			p.emitEvent(requestID, message, source)
		}
		return
	}

	p.mutex.Lock()
	if requestID != p.sequence || source == "clipboard" && !p.enabled {
		p.mutex.Unlock()
		p.logger.Debug("丢弃迟到的翻译结果", logger.Uint64("request_id", requestID))
		return
	}
	p.currentCancel = nil
	p.currentSource = ""
	p.mutex.Unlock()

	if alwaysEmit && filter.Normalize(result.Text) == "" {
		p.logger.Warn("划词翻译返回空结果", logger.Uint64("request_id", requestID))
		p.emitEvent(requestID, "划词翻译失败：模型返回了空结果", source)
		return
	}
	if !alwaysEmit && filter.Normalize(result.Text) == filter.Normalize(text) {
		p.logger.Info("跳过与原文相同的翻译结果",
			logger.Uint64("request_id", requestID),
			logger.String("text_hash", shortHash(textHash)),
		)
		return
	}

	p.logger.Info("翻译成功",
		logger.Uint64("request_id", requestID),
		logger.String("model", result.Model),
		logger.Int64("duration_ms", result.DurationMS),
	)
	p.emitEvent(requestID, result.Text, source)
}

func (p *Processor) markFailed(requestID uint64, textHash string, source string) bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if requestID != p.sequence {
		return false
	}
	p.currentCancel = nil
	p.currentSource = ""
	if source == "clipboard" && p.lastObservedHash == textHash {
		p.lastObservedHash = ""
	}
	return true
}

func (p *Processor) cancelCurrentLocked() {
	if p.currentCancel != nil {
		p.currentCancel()
		p.currentCancel = nil
	}
	p.currentSource = ""
}

func (p *Processor) emitEvent(requestID uint64, text string, source string) {
	if p.emit == nil {
		return
	}
	p.emit(Event{
		RequestID:   requestID,
		Text:        text,
		Source:      source,
		TimestampMS: time.Now().UnixMilli(),
	})
}

func hashText(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

func shortHash(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

// String 返回处理器诊断摘要，不包含剪切板原文。
func (p *Processor) String() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return fmt.Sprintf("enabled=%t sequence=%d", p.enabled, p.sequence)
}
