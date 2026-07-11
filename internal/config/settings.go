package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Settings 是设置窗口使用的完整配置，API Key 仅接收新值，不返回已有明文。
type Settings struct {
	App       AppConfig       `json:"app"`
	Clipboard ClipboardConfig `json:"clipboard"`
	Selection SelectionConfig `json:"selection"`
	LLM       LLMSettings     `json:"llm"`
	Subtitle  SubtitleConfig  `json:"subtitle"`
	Logging   LoggingConfig   `json:"logging"`
}

// LLMSettings 是设置窗口使用的模型配置。
type LLMSettings struct {
	Provider         string   `json:"provider"`
	BaseURL          string   `json:"base_url"`
	APIKey           string   `json:"api_key"`
	APIKeyConfigured bool     `json:"api_key_configured"`
	APIKeyChanged    bool     `json:"api_key_changed"`
	Model            string   `json:"model"`
	Temperature      *float32 `json:"temperature"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
}

// LoadSettingsFile 加载原始 YAML 配置并用当前默认值补齐设置字段。
func LoadSettingsFile(path string) (Settings, error) {
	rawConfig, _, _, err := loadRawConfigDocument(path)
	if err != nil {
		return Settings{}, err
	}
	configured := strings.TrimSpace(resolveAPIKey(rawConfig.LLM.APIKey)) != ""
	return settingsFromConfig(rawConfig, configured), nil
}

// SaveSettingsFile 校验并保存完整设置，保留未知字段、注释和未修改的 API Key。
func SaveSettingsFile(path string, settings Settings) error {
	currentConfig, document, fileMode, err := loadRawConfigDocument(path)
	if err != nil {
		return err
	}
	updatedConfig := settings.config()
	if !settings.LLM.APIKeyChanged {
		updatedConfig.LLM.APIKey = currentConfig.LLM.APIKey
	}
	updatedConfig.App.LogLevel = strings.ToLower(strings.TrimSpace(updatedConfig.App.LogLevel))
	updatedConfig.LLM.BaseURL = strings.TrimSpace(updatedConfig.LLM.BaseURL)
	updatedConfig.LLM.APIKey = strings.TrimSpace(updatedConfig.LLM.APIKey)

	validationConfig := updatedConfig
	validationConfig.LLM.BaseURL = resolveBaseURL(validationConfig.LLM.BaseURL)
	validationConfig.LLM.APIKey = resolveAPIKey(validationConfig.LLM.APIKey)
	if err := validationConfig.Validate(); err != nil {
		return err
	}

	desiredData, err := yaml.Marshal(updatedConfig)
	if err != nil {
		return fmt.Errorf("生成完整配置失败: %w", err)
	}
	var desiredDocument yaml.Node
	if err := yaml.Unmarshal(desiredData, &desiredDocument); err != nil {
		return fmt.Errorf("解析完整配置失败: %w", err)
	}
	mergeMapping(document.Content[0], desiredDocument.Content[0])
	mergedData, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("生成合并配置失败: %w", err)
	}
	if err := writeFileAtomic(path, mergedData, fileMode); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return nil
}

func loadRawConfigDocument(path string) (Config, *yaml.Node, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, 0, fmt.Errorf("读取配置失败: %w", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		return Config{}, nil, 0, fmt.Errorf("读取配置文件权限失败: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Config{}, nil, 0, fmt.Errorf("解析配置失败: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return Config{}, nil, 0, fmt.Errorf("配置根节点必须是 YAML 映射")
	}
	rawConfig := Default()
	if err := document.Decode(&rawConfig); err != nil {
		return Config{}, nil, 0, fmt.Errorf("解析配置失败: %w", err)
	}
	return rawConfig, &document, fileInfo.Mode().Perm(), nil
}

func settingsFromConfig(cfg Config, apiKeyConfigured bool) Settings {
	return Settings{
		App:       cfg.App,
		Clipboard: cfg.Clipboard,
		Selection: cfg.Selection,
		LLM: LLMSettings{
			Provider:         cfg.LLM.Provider,
			BaseURL:          cfg.LLM.BaseURL,
			APIKeyConfigured: apiKeyConfigured,
			Model:            cfg.LLM.Model,
			Temperature:      cfg.LLM.Temperature,
			TimeoutSeconds:   cfg.LLM.TimeoutSeconds,
		},
		Subtitle: cfg.Subtitle,
		Logging:  cfg.Logging,
	}
}

func (settings Settings) config() Config {
	return Config{
		App:       settings.App,
		Clipboard: settings.Clipboard,
		Selection: settings.Selection,
		LLM: LLMConfig{
			Provider:       settings.LLM.Provider,
			BaseURL:        settings.LLM.BaseURL,
			APIKey:         settings.LLM.APIKey,
			Model:          settings.LLM.Model,
			Temperature:    settings.LLM.Temperature,
			TimeoutSeconds: settings.LLM.TimeoutSeconds,
		},
		Subtitle: settings.Subtitle,
		Logging:  settings.Logging,
	}
}

func mergeMapping(target *yaml.Node, desired *yaml.Node) {
	for index := 0; index+1 < len(desired.Content); index += 2 {
		desiredKey := desired.Content[index]
		desiredValue := desired.Content[index+1]
		targetValue := mappingValue(target, desiredKey.Value)
		if targetValue == nil {
			target.Content = append(target.Content, desiredKey, desiredValue)
			continue
		}
		if targetValue.Kind == yaml.MappingNode && desiredValue.Kind == yaml.MappingNode {
			mergeMapping(targetValue, desiredValue)
			continue
		}
		headComment := targetValue.HeadComment
		lineComment := targetValue.LineComment
		footComment := targetValue.FootComment
		*targetValue = *desiredValue
		targetValue.HeadComment = headComment
		targetValue.LineComment = lineComment
		targetValue.FootComment = footComment
	}
}
