package translator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"

	"floating-translator/internal/config"
)

const systemPrompt = `你是一个专业的英文到简体中文翻译助手。

请将用户提供的英文内容翻译为自然、准确、流畅的简体中文。

要求：
1. 只输出中文译文；
2. 不要解释或总结；
3. 不要添加原文没有的信息；
4. 保留必要的英文专有名词；
5. 技术术语采用常见中文译法。`

const responseProbeLimit = 512

type responseDiagnosticContextKey struct{}

type responseDiagnostic struct {
	mutex       sync.Mutex
	status      string
	contentType string
	endpoint    string
	nonJSON     bool
}

type diagnosticTransport struct {
	base http.RoundTripper
}

type prefixedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *prefixedReadCloser) Close() error {
	return r.closer.Close()
}

// EinoTranslator 使用 Eino OpenAI ChatModel 完成翻译。
type EinoTranslator struct {
	modelName string
	template  *prompt.DefaultChatTemplate
	chatModel model.BaseChatModel
}

// NewEino 创建 OpenAI-compatible Eino 翻译器。
func NewEino(ctx context.Context, cfg config.LLMConfig) (*EinoTranslator, error) {
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		Timeout:     time.Duration(cfg.TimeoutSeconds) * time.Second,
		HTTPClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
			Transport: diagnosticTransport{
				base: http.DefaultTransport,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 Eino ChatModel 失败: %w", err)
	}

	return &EinoTranslator{
		modelName: cfg.Model,
		template: prompt.FromMessages(
			schema.FString,
			schema.SystemMessage(systemPrompt),
			schema.UserMessage("{text}"),
		),
		chatModel: chatModel,
	}, nil
}

// Translate 翻译输入文本，并只返回清理首尾空白后的模型内容。
func (t *EinoTranslator) Translate(ctx context.Context, text string) (Result, error) {
	startedAt := time.Now()
	diagnostic := &responseDiagnostic{}
	ctx = context.WithValue(ctx, responseDiagnosticContextKey{}, diagnostic)
	messages, err := t.template.Format(ctx, map[string]any{"text": text})
	if err != nil {
		return Result{}, fmt.Errorf("格式化翻译提示词失败: %w", err)
	}
	response, err := t.chatModel.Generate(ctx, messages)
	if err != nil {
		if diagnostic.isNonJSON() {
			return Result{}, diagnostic.error()
		}
		return Result{}, fmt.Errorf("调用翻译模型失败: %w", err)
	}
	translatedText := strings.TrimSpace(response.Content)
	if translatedText == "" {
		return Result{}, errors.New("翻译模型返回空内容")
	}
	return Result{
		Text:       translatedText,
		Model:      t.modelName,
		DurationMS: DurationMS(time.Since(startedAt)),
	}, nil
}

func (t diagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}

	diagnostic, ok := request.Context().Value(responseDiagnosticContextKey{}).(*responseDiagnostic)
	if !ok {
		return response, nil
	}
	body := response.Body
	prefix, readErr := io.ReadAll(io.LimitReader(body, responseProbeLimit))
	if readErr != nil {
		return response, nil
	}
	response.Body = &prefixedReadCloser{
		Reader: io.MultiReader(bytes.NewReader(prefix), body),
		closer: body,
	}
	diagnostic.record(response, prefix, request.URL.EscapedPath())
	return response, nil
}

func (d *responseDiagnostic) record(response *http.Response, prefix []byte, endpoint string) {
	contentType := response.Header.Get("Content-Type")
	if !isHTMLResponse(contentType, prefix) {
		return
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.status = response.Status
	d.contentType = contentType
	d.endpoint = endpoint
	d.nonJSON = true
}

func (d *responseDiagnostic) isNonJSON() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.nonJSON
}

func (d *responseDiagnostic) error() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	status := d.status
	if status == "" {
		status = "未知状态"
	}
	contentType := d.contentType
	if contentType == "" {
		contentType = "未提供"
	}
	endpoint := d.endpoint
	if endpoint == "" {
		endpoint = "/chat/completions"
	}
	return fmt.Errorf("模型接口返回非 JSON 响应（HTTP %s，Content-Type: %s，路径: %s）；请检查 llm.base_url 是否为 OpenAI-compatible API 根路径（通常以 /v1 结尾），以及 API Key、反向代理或登录配置", status, contentType, endpoint)
}

func isHTMLResponse(contentType string, prefix []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		mediaType = strings.ToLower(mediaType)
		if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			return true
		}
	}
	trimmed := bytes.TrimSpace(prefix)
	return len(trimmed) > 0 && trimmed[0] == '<'
}
