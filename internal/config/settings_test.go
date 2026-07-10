package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsPreserveSecretCommentsAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	content := strings.Replace(DefaultTemplate, `api_key: "${LLM_API_KEY}"`, `api_key: "literal-secret"`, 1)
	content = strings.Replace(content, `model: ""`, `model: "test-model"`, 1)
	content = strings.Replace(content, "subtitle:\n", "# 字幕位置说明\nsubtitle:\n", 1)
	content += "\nfuture_feature:\n  enabled: true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatalf("LoadSettingsFile() error = %v", err)
	}
	if settings.LLM.APIKey != "" || !settings.LLM.APIKeyConfigured {
		t.Fatalf("API Key 状态泄露或错误: %+v", settings.LLM)
	}
	settings.Subtitle.BottomOffsetPercent = 9
	if err := SaveSettingsFile(path, settings); err != nil {
		t.Fatalf("SaveSettingsFile() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	text := string(updated)
	for _, expected := range []string{"literal-secret", "# 字幕位置说明", "future_feature:", "bottom_offset_percent: 9"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("保存结果未保留 %q:\n%s", expected, text)
		}
	}
}

func TestSettingsReplaceChangedAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	content := strings.Replace(DefaultTemplate, `api_key: "${LLM_API_KEY}"`, `api_key: "old-secret"`, 1)
	content = strings.Replace(content, `model: ""`, `model: "test-model"`, 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatalf("LoadSettingsFile() error = %v", err)
	}
	settings.LLM.APIKey = "new-secret"
	settings.LLM.APIKeyChanged = true
	if err := SaveSettingsFile(path, settings); err != nil {
		t.Fatalf("SaveSettingsFile() error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if strings.Contains(string(updated), "old-secret") || !strings.Contains(string(updated), "new-secret") {
		t.Fatalf("API Key 未正确替换:\n%s", updated)
	}
}

func TestSettingsSaveBackfillsCurrentFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	incompleteConfig := `app:
  log_level: info
llm:
  provider: openai_compatible
  base_url: https://api.openai.com/v1
  api_key: test-key
  model: test-model
  timeout_seconds: 20
`
	if err := os.WriteFile(path, []byte(incompleteConfig), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatalf("LoadSettingsFile() error = %v", err)
	}
	if err := SaveSettingsFile(path, settings); err != nil {
		t.Fatalf("SaveSettingsFile() error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	for _, expected := range []string{"selection:", "compatibility_mode: false", "bottom_offset_percent: 4", "temperature: null", "logging:"} {
		if !strings.Contains(string(updated), expected) {
			t.Fatalf("保存结果未补齐 %q:\n%s", expected, updated)
		}
	}
}
