package filter

import (
	"strings"
	"testing"

	"floating-translator/internal/config"
)

func TestEvaluate(t *testing.T) {
	filter := New(config.Default().Clipboard)
	tests := []struct {
		name       string
		text       string
		wantReason Reason
		wantPass   bool
	}{
		{name: "空白", text: " \n\t", wantReason: ReasonEmpty},
		{name: "英文", text: "This is a clear English sentence.", wantReason: ReasonOK, wantPass: true},
		{name: "带重音的拉丁文字", text: "Café au lait", wantReason: ReasonNotEnglish},
		{name: "德语变音文字", text: "Über das Menü", wantReason: ReasonNotEnglish},
		{name: "纯 ASCII 法语", text: "Bonjour le monde", wantReason: ReasonNotEnglish},
		{name: "纯 ASCII 西语", text: "Hola mundo", wantReason: ReasonNotEnglish},
		{name: "中文占比过高", text: "Hello 世界中文", wantReason: ReasonNotEnglish},
		{name: "纯数字", text: "123456", wantReason: ReasonNotEnglish},
		{name: "URL", text: "https://example.com/docs?q=test", wantReason: ReasonURL},
		{name: "www URL", text: "www.example.com", wantReason: ReasonURL},
		{name: "包含 URL 的句子", text: "Open https://example.com to read the document.", wantReason: ReasonOK, wantPass: true},
		{name: "Go 代码", text: "package main\n\nfunc main() {}", wantReason: ReasonCode},
		{name: "JavaScript 代码", text: "const value = 42", wantReason: ReasonCode},
		{name: "普通技术文字", text: "Use the map value in this guide.", wantReason: ReasonOK, wantPass: true},
		{name: "OpenAI Key", text: "sk-abcdefghijklmnop", wantReason: ReasonSensitive},
		{name: "OpenAI Key 前缀", text: "sk-", wantReason: ReasonSensitive},
		{name: "Stripe Key", text: "sk_live_1234567890abcdef", wantReason: ReasonSensitive},
		{name: "AWS Key 前缀", text: "AKIA", wantReason: ReasonSensitive},
		{name: "GitHub Token", text: "ghp_" + strings.Repeat("a", 36), wantReason: ReasonSensitive},
		{name: "GitHub Fine-grained Token", text: "github_pat_" + strings.Repeat("a", 30), wantReason: ReasonSensitive},
		{name: "GitLab Token", text: "glpat-" + strings.Repeat("a", 24), wantReason: ReasonSensitive},
		{name: "Google API Key", text: "AIza" + strings.Repeat("a", 30), wantReason: ReasonSensitive},
		{name: "Slack Token", text: "xoxb-" + strings.Repeat("a", 20), wantReason: ReasonSensitive},
		{name: "npm Token", text: "npm_" + strings.Repeat("a", 36), wantReason: ReasonSensitive},
		{name: "JWT", text: "eyJ" + strings.Repeat("a", 12) + "." + strings.Repeat("b", 12) + "." + strings.Repeat("c", 12), wantReason: ReasonSensitive},
		{name: "Authorization Bearer", text: "Authorization: Bearer " + strings.Repeat("a", 24), wantReason: ReasonSensitive},
		{name: "密码赋值", text: "password=very-secret-value", wantReason: ReasonSensitive},
		{name: "空密码赋值", text: "password=", wantReason: ReasonSensitive},
		{name: "私钥", text: "-----BEGIN PRIVATE KEY-----", wantReason: ReasonSensitive},
		{name: "OpenSSH 私钥", text: "-----BEGIN OPENSSH PRIVATE KEY-----", wantReason: ReasonSensitive},
		{name: "超长文本", text: strings.Repeat("a", 3001), wantReason: ReasonTooLong},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := filter.Evaluate(test.text)
			if result.Reason != test.wantReason {
				t.Fatalf("Reason = %q, want %q", result.Reason, test.wantReason)
			}
			if result.ShouldTranslate != test.wantPass {
				t.Fatalf("ShouldTranslate = %t, want %t", result.ShouldTranslate, test.wantPass)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize(" \r\nfirst\rsecond\r\n ")
	want := "first\nsecond"
	if got != want {
		t.Fatalf("Normalize() = %q, want %q", got, want)
	}
}

func TestRedactSensitive(t *testing.T) {
	secrets := []string{
		"token=secret-token",
		"sk-abcdefghijkl",
		"ghp_" + strings.Repeat("a", 36),
		"Authorization: Bearer " + strings.Repeat("b", 24),
		"eyJ" + strings.Repeat("a", 12) + "." + strings.Repeat("b", 12) + "." + strings.Repeat("c", 12),
	}
	redacted := RedactSensitive(strings.Join(secrets, "\n"))
	for _, secret := range secrets {
		if strings.Contains(redacted, secret) {
			t.Fatalf("RedactSensitive() 未完成脱敏 %q: %q", secret, redacted)
		}
	}
}
