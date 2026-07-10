package translator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floating-translator/internal/config"
)

func TestEinoTranslatorReportsHTMLResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("请求路径 = %q，want /v1/chat/completions", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("<html><body>代理登录页面</body></html>"))
	}))
	defer server.Close()

	translator := newTestTranslator(t, server.URL+"/v1")
	_, err := translator.Translate(context.Background(), "Translate this sentence.")
	if err == nil {
		t.Fatal("Translate() 应返回非 JSON 响应错误")
	}
	message := err.Error()
	for _, expected := range []string{"模型接口返回非 JSON 响应", "HTTP 200 OK", "text/html", "/v1/chat/completions", "llm.base_url"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("错误信息 %q 未包含 %q", message, expected)
		}
	}
	if strings.Contains(message, "代理登录页面") {
		t.Fatalf("错误信息不应包含响应正文: %q", message)
	}
}

func TestEinoTranslatorAcceptsJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("请求路径 = %q，want /v1/chat/completions", request.URL.Path)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}
		if _, exists := requestBody["temperature"]; exists {
			t.Fatalf("temperature 为 null 时不应发送该字段: %+v", requestBody)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
  "id": "chatcmpl-test",
  "object": "chat.completion",
  "created": 0,
  "model": "test-model",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "测试译文"},
    "finish_reason": "stop"
  }]
}`))
	}))
	defer server.Close()

	translator := newTestTranslator(t, server.URL+"/v1")
	result, err := translator.Translate(context.Background(), "Translate this sentence.")
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if result.Text != "测试译文" {
		t.Fatalf("译文 = %q，want 测试译文", result.Text)
	}
}

func newTestTranslator(t *testing.T, baseURL string) *EinoTranslator {
	t.Helper()
	cfg := config.Default().LLM
	cfg.APIKey = "test-key"
	cfg.BaseURL = baseURL
	cfg.Model = "test-model"
	translator, err := NewEino(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewEino() error = %v", err)
	}
	return translator
}
