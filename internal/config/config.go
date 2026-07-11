// Package config 负责加载、校验和初始化应用配置。
package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"floating-translator/internal/hotkey"

	"gopkg.in/yaml.v3"
)

const (
	// ApplicationDirectoryName 是应用在用户配置目录下使用的文件夹名称。
	ApplicationDirectoryName = "FloatingTranslator"
	// ConfigFileName 是运行配置文件名。
	ConfigFileName = "config.yaml"

	maxDebounceMS        = 60_000
	maxTextLength        = 100_000
	maxTimeoutSeconds    = 300
	maxSubtitleAnimation = 60_000
	maxLogSizeMB         = 1_024
	maxLogBackups        = 100
)

// ErrInvalidConfig 表示配置未满足运行要求。
var ErrInvalidConfig = errors.New("配置无效")

// Paths 描述应用运行时使用的目录和文件路径。
type Paths struct {
	Root       string
	ConfigFile string
	LogDir     string
	LogFile    string
}

// Config 是应用完整配置。
type Config struct {
	App       AppConfig       `yaml:"app" json:"app"`
	Clipboard ClipboardConfig `yaml:"clipboard" json:"clipboard"`
	Selection SelectionConfig `yaml:"selection" json:"selection"`
	LLM       LLMConfig       `yaml:"llm" json:"llm"`
	Subtitle  SubtitleConfig  `yaml:"subtitle" json:"subtitle"`
	Logging   LoggingConfig   `yaml:"logging" json:"logging"`
}

// AppConfig 是应用级配置。
type AppConfig struct {
	LogLevel string `yaml:"log_level" json:"log_level"`
}

// ClipboardConfig 是剪切板监听和过滤配置。
type ClipboardConfig struct {
	Enable               bool    `yaml:"enable" json:"enable"`
	DebounceMS           int     `yaml:"debounce_ms" json:"debounce_ms"`
	MaxTextLength        int     `yaml:"max_text_length" json:"max_text_length"`
	SkipURL              bool    `yaml:"skip_url" json:"skip_url"`
	SkipCode             bool    `yaml:"skip_code" json:"skip_code"`
	SkipSensitive        bool    `yaml:"skip_sensitive" json:"skip_sensitive"`
	OnlyTranslateEnglish bool    `yaml:"only_translate_english" json:"only_translate_english"`
	EnglishMinRatio      float64 `yaml:"english_min_ratio" json:"english_min_ratio"`
	ChineseMaxRatio      float64 `yaml:"chinese_max_ratio" json:"chinese_max_ratio"`
}

// SelectionConfig 是划词翻译的全局快捷键和兼容读取配置。
type SelectionConfig struct {
	Enable            bool   `yaml:"enable" json:"enable"`
	Hotkey            string `yaml:"hotkey" json:"hotkey"`
	CompatibilityMode bool   `yaml:"compatibility_mode" json:"compatibility_mode"`
}

// LLMConfig 是 OpenAI-compatible 模型配置。
type LLMConfig struct {
	Provider       string   `yaml:"provider" json:"provider"`
	BaseURL        string   `yaml:"base_url" json:"base_url"`
	APIKey         string   `yaml:"api_key" json:"-"`
	Model          string   `yaml:"model" json:"model"`
	Temperature    *float32 `yaml:"temperature" json:"temperature"`
	TimeoutSeconds int      `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// SubtitleConfig 是字幕窗口和动画配置，同时会发送给前端。
type SubtitleConfig struct {
	WidthPercent        int     `yaml:"width_percent" json:"width_percent"`
	BottomOffsetPercent int     `yaml:"bottom_offset_percent" json:"bottom_offset_percent"`
	FontFamily          string  `yaml:"font_family" json:"font_family"`
	FontSize            int     `yaml:"font_size" json:"font_size"`
	MaxLines            int     `yaml:"max_lines" json:"max_lines"`
	BackgroundOpacity   float64 `yaml:"background_opacity" json:"background_opacity"`
	FadeInMS            int     `yaml:"fade_in_ms" json:"fade_in_ms"`
	DisplayMS           int     `yaml:"display_ms" json:"display_ms"`
	FadeOutMS           int     `yaml:"fade_out_ms" json:"fade_out_ms"`
}

// LoggingConfig 是日志附加配置。
type LoggingConfig struct {
	IncludeSourceText bool `yaml:"include_source_text" json:"include_source_text"`
	MaxSizeMB         int  `yaml:"max_size_mb" json:"max_size_mb"`
	MaxBackups        int  `yaml:"max_backups" json:"max_backups"`
}

// Default 返回带安全默认值的配置。
func Default() Config {
	return Config{
		App: AppConfig{LogLevel: "info"},
		Clipboard: ClipboardConfig{
			Enable:               true,
			DebounceMS:           300,
			MaxTextLength:        3000,
			SkipURL:              true,
			SkipCode:             true,
			SkipSensitive:        true,
			OnlyTranslateEnglish: true,
			EnglishMinRatio:      0.5,
			ChineseMaxRatio:      0.3,
		},
		Selection: SelectionConfig{
			Enable:            true,
			Hotkey:            defaultSelectionHotkey(),
			CompatibilityMode: false,
		},
		LLM: LLMConfig{
			Provider:       "openai_compatible",
			BaseURL:        "https://api.openai.com/v1",
			APIKey:         "${LLM_API_KEY}",
			Temperature:    nil,
			TimeoutSeconds: 20,
		},
		Subtitle: SubtitleConfig{
			WidthPercent:        70,
			BottomOffsetPercent: 4,
			FontFamily:          defaultFontFamily(),
			FontSize:            28,
			MaxLines:            4,
			BackgroundOpacity:   0.38,
			FadeInMS:            200,
			DisplayMS:           6000,
			FadeOutMS:           800,
		},
		Logging: LoggingConfig{
			IncludeSourceText: false,
			MaxSizeMB:         10,
			MaxBackups:        3,
		},
	}
}

// PreparePaths 创建用户配置目录，并在首次运行时生成配置模板。
func PreparePaths() (Paths, bool, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, false, fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	return preparePaths(baseDir)
}

func preparePaths(baseDir string) (Paths, bool, error) {
	root := filepath.Join(baseDir, ApplicationDirectoryName)
	paths := Paths{
		Root:       root,
		ConfigFile: filepath.Join(root, ConfigFileName),
		LogDir:     filepath.Join(root, "logs"),
		LogFile:    filepath.Join(root, "logs", "app.log"),
	}
	if err := os.MkdirAll(paths.LogDir, 0o700); err != nil {
		return Paths{}, false, fmt.Errorf("创建应用目录失败: %w", err)
	}

	_, err := os.Stat(paths.ConfigFile)
	if err == nil {
		return paths, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Paths{}, false, fmt.Errorf("检查配置文件失败: %w", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(defaultTemplate()), 0o600); err != nil {
		return Paths{}, false, fmt.Errorf("生成配置模板失败: %w", err)
	}
	return paths, true, nil
}

func defaultSelectionHotkey() string {
	if runtime.GOOS == "darwin" {
		return "Command+Option+T"
	}
	return "Ctrl+Alt+T"
}

func defaultFontFamily() string {
	if runtime.GOOS == "darwin" {
		return "PingFang SC"
	}
	return "Microsoft YaHei UI"
}

func defaultTemplate() string {
	if runtime.GOOS != "darwin" {
		return DefaultTemplate
	}
	template := strings.Replace(DefaultTemplate,
		`hotkey: "Ctrl+Alt+T"`,
		`hotkey: "Command+Option+T"`,
		1,
	)
	return strings.Replace(template,
		`font_family: "Microsoft YaHei UI"`,
		`font_family: "PingFang SC"`,
		1,
	)
}

func migrateLegacyPlatformDefaults(cfg *Config) {
	if runtime.GOOS != "darwin" {
		return
	}
	if cfg.Selection.Hotkey == "Ctrl+Alt+T" {
		cfg.Selection.Hotkey = defaultSelectionHotkey()
	}
	if cfg.Subtitle.FontFamily == "Microsoft YaHei UI" {
		cfg.Subtitle.FontFamily = defaultFontFamily()
	}
}

// LoadFile 从指定路径读取、解析并校验配置。
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置失败: %w", err)
	}
	migrateLegacyPlatformDefaults(&cfg)
	cfg.App.LogLevel = strings.ToLower(strings.TrimSpace(cfg.App.LogLevel))
	cfg.LLM.BaseURL = resolveBaseURL(cfg.LLM.BaseURL)
	cfg.LLM.APIKey = resolveAPIKey(cfg.LLM.APIKey)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SetSelectionEnabled 持久化划词翻译开关，同时保留配置中的注释和未解析变量。
func SetSelectionEnabled(path string, enabled bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("配置根节点必须是 YAML 映射")
	}

	root := document.Content[0]
	selection := mappingValue(root, "selection")
	if selection == nil {
		selection = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "selection"},
			selection,
		)
	}
	if selection.Kind != yaml.MappingNode {
		return fmt.Errorf("selection 必须是 YAML 映射")
	}
	enableNode := mappingValue(selection, "enable")
	if enableNode == nil {
		enableNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool"}
		selection.Content = append(selection.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enable"},
			enableNode,
		)
	}
	enableNode.Kind = yaml.ScalarNode
	enableNode.Tag = "!!bool"
	enableNode.Value = fmt.Sprintf("%t", enabled)
	enableNode.Style = 0

	updated, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("生成配置失败: %w", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取配置文件权限失败: %w", err)
	}
	if err := writeFileAtomic(path, updated, fileInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// Validate 校验全部运行参数。
func (c Config) Validate() error {
	switch c.App.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return invalid("app.log_level 仅支持 debug、info、warn、error")
	}
	if c.Clipboard.DebounceMS < 0 || c.Clipboard.DebounceMS > maxDebounceMS {
		return invalid(fmt.Sprintf("clipboard.debounce_ms 必须在 0 到 %d 之间", maxDebounceMS))
	}
	if c.Clipboard.MaxTextLength <= 0 || c.Clipboard.MaxTextLength > maxTextLength {
		return invalid(fmt.Sprintf("clipboard.max_text_length 必须在 1 到 %d 之间", maxTextLength))
	}
	if !ratioValid(c.Clipboard.EnglishMinRatio) || !ratioValid(c.Clipboard.ChineseMaxRatio) {
		return invalid("语言比例阈值必须在 0 到 1 之间")
	}
	if c.Selection.Enable {
		if _, err := hotkey.Parse(c.Selection.Hotkey); err != nil {
			return invalid("selection.hotkey 无效: " + err.Error())
		}
	}
	if c.LLM.Provider != "openai_compatible" {
		return invalid("llm.provider 仅支持 openai_compatible")
	}
	parsedURL, err := url.Parse(strings.TrimSpace(c.LLM.BaseURL))
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return invalid("llm.base_url 必须是有效的 HTTP 或 HTTPS 地址")
	}
	if strings.TrimSpace(c.LLM.APIKey) == "" {
		return invalid("缺少 API Key，请设置 LLM_API_KEY 或 llm.api_key")
	}
	if strings.TrimSpace(c.LLM.Model) == "" {
		return invalid("llm.model 不能为空")
	}
	if c.LLM.Temperature != nil && (math.IsNaN(float64(*c.LLM.Temperature)) || math.IsInf(float64(*c.LLM.Temperature), 0) || *c.LLM.Temperature < 0 || *c.LLM.Temperature > 2) {
		return invalid("llm.temperature 必须是 0 到 2 之间的有限数值")
	}
	if c.LLM.TimeoutSeconds <= 0 || c.LLM.TimeoutSeconds > maxTimeoutSeconds {
		return invalid(fmt.Sprintf("llm.timeout_seconds 必须在 1 到 %d 之间", maxTimeoutSeconds))
	}
	if c.Subtitle.WidthPercent < 20 || c.Subtitle.WidthPercent > 100 {
		return invalid("subtitle.width_percent 必须在 20 到 100 之间")
	}
	if c.Subtitle.BottomOffsetPercent < 0 || c.Subtitle.BottomOffsetPercent > 50 {
		return invalid("subtitle.bottom_offset_percent 必须在 0 到 50 之间")
	}
	if strings.TrimSpace(c.Subtitle.FontFamily) == "" {
		return invalid("subtitle.font_family 不能为空")
	}
	if c.Subtitle.FontSize < 12 || c.Subtitle.FontSize > 96 {
		return invalid("subtitle.font_size 必须在 12 到 96 之间")
	}
	if c.Subtitle.MaxLines < 1 || c.Subtitle.MaxLines > 10 {
		return invalid("subtitle.max_lines 必须在 1 到 10 之间")
	}
	if !ratioValid(c.Subtitle.BackgroundOpacity) {
		return invalid("subtitle.background_opacity 必须在 0 到 1 之间")
	}
	if c.Subtitle.FadeInMS < 0 || c.Subtitle.FadeInMS > maxSubtitleAnimation || c.Subtitle.DisplayMS < 0 || c.Subtitle.DisplayMS > maxSubtitleAnimation || c.Subtitle.FadeOutMS < 0 || c.Subtitle.FadeOutMS > maxSubtitleAnimation {
		return invalid(fmt.Sprintf("字幕动画时间必须在 0 到 %d 之间", maxSubtitleAnimation))
	}
	if c.Logging.MaxSizeMB <= 0 || c.Logging.MaxSizeMB > maxLogSizeMB || c.Logging.MaxBackups < 0 || c.Logging.MaxBackups > maxLogBackups {
		return invalid(fmt.Sprintf("日志轮转参数超出范围：大小 1 到 %d MB，备份数 0 到 %d", maxLogSizeMB, maxLogBackups))
	}
	return nil
}

func resolveAPIKey(configValue string) string {
	if value := strings.TrimSpace(os.Getenv("LLM_API_KEY")); value != "" {
		return value
	}
	return strings.TrimSpace(os.ExpandEnv(configValue))
}

func resolveBaseURL(configValue string) string {
	if value := strings.TrimSpace(os.Getenv("LLM_BASE_URL")); value != "" {
		return value
	}
	return strings.TrimSpace(os.ExpandEnv(configValue))
}

func ratioValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, message)
}

// DefaultTemplate 是首次运行时生成的配置模板，不包含真实密钥。
const DefaultTemplate = `app:
  log_level: "info"

clipboard:
  enable: true
  debounce_ms: 300
  max_text_length: 3000
  skip_url: true
  skip_code: true
  skip_sensitive: true
  only_translate_english: true
  english_min_ratio: 0.5
  chinese_max_ratio: 0.3

selection:
  enable: true
  hotkey: "Ctrl+Alt+T"
  compatibility_mode: false

llm:
  provider: "openai_compatible"
  base_url: "https://api.openai.com/v1"
  api_key: "${LLM_API_KEY}"
  model: ""
  temperature: null
  timeout_seconds: 20

subtitle:
  width_percent: 70
  bottom_offset_percent: 4
  font_family: "Microsoft YaHei UI"
  font_size: 28
  max_lines: 4
  background_opacity: 0.38
  fade_in_ms: 200
  display_ms: 6000
  fade_out_ms: 800

logging:
  include_source_text: false
  max_size_mb: 10
  max_backups: 3
`
