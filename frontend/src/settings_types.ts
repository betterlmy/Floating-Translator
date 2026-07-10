export interface AppSettings {
  log_level: string
}

export interface ClipboardSettings {
  enable: boolean
  debounce_ms: number
  max_text_length: number
  skip_url: boolean
  skip_code: boolean
  skip_sensitive: boolean
  only_translate_english: boolean
  english_min_ratio: number
  chinese_max_ratio: number
}

export interface SelectionSettings {
  enable: boolean
  hotkey: string
  compatibility_mode: boolean
}

export interface LLMSettings {
  provider: string
  base_url: string
  api_key: string
  api_key_configured: boolean
  api_key_changed: boolean
  model: string
  temperature: number | null
  timeout_seconds: number
}

export interface SubtitleSettings {
  width_percent: number
  bottom_offset_percent: number
  font_family: string
  font_size: number
  max_lines: number
  background_opacity: number
  fade_in_ms: number
  display_ms: number
  fade_out_ms: number
}

export interface LoggingSettings {
  include_source_text: boolean
  max_size_mb: number
  max_backups: number
}

export interface SettingsData {
  app: AppSettings
  clipboard: ClipboardSettings
  selection: SelectionSettings
  llm: LLMSettings
  subtitle: SubtitleSettings
  logging: LoggingSettings
}
