package config

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPreparePathsCreatesTemplate(t *testing.T) {
	baseDir := t.TempDir()
	paths, created, err := preparePaths(baseDir)
	if err != nil {
		t.Fatalf("PreparePaths() error = %v", err)
	}
	if !created {
		t.Fatal("首次调用应创建配置模板")
	}
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		t.Fatalf("配置模板不存在: %v", err)
	}
	if _, err := os.Stat(paths.LogDir); err != nil {
		t.Fatalf("日志目录不存在: %v", err)
	}

	_, created, err = preparePaths(baseDir)
	if err != nil {
		t.Fatalf("第二次 PreparePaths() error = %v", err)
	}
	if created {
		t.Fatal("已有配置时不应覆盖模板")
	}
}

func TestLoadFileEnvironmentKeyHasPriority(t *testing.T) {
	t.Setenv("LLM_API_KEY", "environment-key")
	t.Setenv("LLM_BASE_URL", "https://env.example.com/v1")
	cfg := Default()
	cfg.LLM.APIKey = "config-key"
	cfg.LLM.Model = "test-model"
	path := writeConfig(t, cfg)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if loaded.LLM.APIKey != "environment-key" {
		t.Fatalf("APIKey = %q, want environment-key", loaded.LLM.APIKey)
	}
	if loaded.LLM.BaseURL != "https://env.example.com/v1" {
		t.Fatalf("BaseURL = %q, want environment base URL", loaded.LLM.BaseURL)
	}
}

func TestLoadFileRejectsMissingSecret(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_BASE_URL", "")
	cfg := Default()
	cfg.LLM.APIKey = "${LLM_API_KEY}"
	cfg.LLM.Model = "test-model"
	path := writeConfig(t, cfg)

	_, err := LoadFile(path)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadFile() error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateRejectsNonFiniteAndUnboundedValues(t *testing.T) {
	base := Default()
	base.LLM.APIKey = "test-key"
	base.LLM.Model = "test-model"

	tests := []struct {
		name  string
		setup func(*Config)
	}{
		{name: "temperature NaN", setup: func(cfg *Config) {
			value := float32(math.NaN())
			cfg.LLM.Temperature = &value
		}},
		{name: "temperature positive infinity", setup: func(cfg *Config) {
			value := float32(math.Inf(1))
			cfg.LLM.Temperature = &value
		}},
		{name: "language ratio NaN", setup: func(cfg *Config) {
			cfg.Clipboard.EnglishMinRatio = math.NaN()
		}},
		{name: "timeout too large", setup: func(cfg *Config) {
			cfg.LLM.TimeoutSeconds = maxTimeoutSeconds + 1
		}},
		{name: "text length too large", setup: func(cfg *Config) {
			cfg.Clipboard.MaxTextLength = maxTextLength + 1
		}},
		{name: "animation too large", setup: func(cfg *Config) {
			cfg.Subtitle.DisplayMS = maxSubtitleAnimation + 1
		}},
		{name: "log size too large", setup: func(cfg *Config) {
			cfg.Logging.MaxSizeMB = maxLogSizeMB + 1
		}},
		{name: "log backups too large", setup: func(cfg *Config) {
			cfg.Logging.MaxBackups = maxLogBackups + 1
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.setup(&cfg)
			if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestValidateRejectsUnsupportedProvider(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "test-model"
	cfg.LLM.Provider = "ollama"

	if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateRejectsUnsupportedLogLevel(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "test-model"
	cfg.App.LogLevel = "trace"

	if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
	}
}

func TestDefaultIncludesSelectionAndBottomOffset(t *testing.T) {
	cfg := Default()
	if !cfg.Selection.Enable || cfg.Selection.Hotkey != "Ctrl+Alt+T" {
		t.Fatalf("Selection = %+v", cfg.Selection)
	}
	if cfg.Selection.CompatibilityMode {
		t.Fatal("CompatibilityMode = true, want false")
	}
	if cfg.Subtitle.BottomOffsetPercent != 4 {
		t.Fatalf("BottomOffsetPercent = %d, want 4", cfg.Subtitle.BottomOffsetPercent)
	}
	if cfg.LLM.Temperature != nil {
		t.Fatalf("Temperature = %v, want nil", *cfg.LLM.Temperature)
	}
}

func TestValidateAcceptsOptionalTemperature(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "test-model"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("nil temperature Validate() error = %v", err)
	}

	temperature := float32(0.2)
	cfg.LLM.Temperature = &temperature
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configured temperature Validate() error = %v", err)
	}

	invalidTemperature := float32(2.1)
	cfg.LLM.Temperature = &invalidTemperature
	if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid temperature Validate() error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateRejectsInvalidSelectionHotkey(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "test-model"
	cfg.Selection.Hotkey = "Ctrl+Space"

	if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateRejectsInvalidBottomOffset(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "test-model"
	cfg.Subtitle.BottomOffsetPercent = 51

	if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
	}
}

func TestSetSelectionEnabledPreservesRawConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	original := `# 保留配置注释
selection:
  enable: true
  hotkey: "Ctrl+Alt+T"
llm:
  api_key: "${LLM_API_KEY}"
`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := SetSelectionEnabled(path, false); err != nil {
		t.Fatalf("SetSelectionEnabled() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	content := string(updated)
	if !strings.Contains(content, "# 保留配置注释") {
		t.Fatalf("配置注释未保留:\n%s", content)
	}
	if !strings.Contains(content, "${LLM_API_KEY}") {
		t.Fatalf("环境变量占位符未保留:\n%s", content)
	}
	var decoded struct {
		Selection SelectionConfig `yaml:"selection"`
	}
	if err := yaml.Unmarshal(updated, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if decoded.Selection.Enable {
		t.Fatal("selection.enable = true, want false")
	}
	if decoded.Selection.Hotkey != "Ctrl+Alt+T" {
		t.Fatalf("selection.hotkey = %q", decoded.Selection.Hotkey)
	}
	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat() error = %v", err)
		}
		if fileInfo.Mode().Perm() != 0o640 {
			t.Fatalf("配置权限 = %o, want 640", fileInfo.Mode().Perm())
		}
	}
}

func TestSetSelectionEnabledAddsMissingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	if err := os.WriteFile(path, []byte("app:\n  log_level: info\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := SetSelectionEnabled(path, false); err != nil {
		t.Fatalf("SetSelectionEnabled() error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	var decoded struct {
		Selection map[string]bool `yaml:"selection"`
	}
	if err := yaml.Unmarshal(updated, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if decoded.Selection == nil {
		t.Fatal("未新增 selection 配置段")
	}
	if decoded.Selection["enable"] {
		t.Fatal("selection.enable = true, want false")
	}
}

func writeConfig(t *testing.T, cfg Config) string {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), ConfigFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
