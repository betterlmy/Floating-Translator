<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'

import {runtimeBridge} from './runtime_bridge'
import type {SettingsData} from './settings_types'

type SectionID = 'model' | 'clipboard' | 'selection' | 'subtitle' | 'logging'

const settingsRefreshEvent = 'settings:refresh'

const sections: Array<{id: SectionID; index: string; label: string; description: string}> = [
  {id: 'model', index: '01', label: '模型服务', description: '接口、模型与超时'},
  {id: 'clipboard', index: '02', label: '剪贴板', description: '监听与内容过滤'},
  {id: 'selection', index: '03', label: '划词翻译', description: '全局快捷键'},
  {id: 'subtitle', index: '04', label: '字幕外观', description: '位置、字号与动画'},
  {id: 'logging', index: '05', label: '日志', description: '级别与轮转'},
]

const activeSection = ref<SectionID>('model')
const settings = ref<SettingsData | null>(null)
const loading = ref(true)
const saving = ref(false)
const temperatureEnabled = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const fontFamilies = ref<string[]>([])
const subtitlePreviewElement = ref<HTMLElement | null>(null)
const nativeSubtitlePreview = ref('')
const shortcutCaptureMessage = ref('')
let successTimer: number | null = null
let removeRefreshListener: (() => void) | null = null
let settingsLoadToken = 0
let nativePreviewTimer: number | null = null
let nativePreviewRequest = 0
let previewResizeObserver: ResizeObserver | null = null

const isMacOS = /Macintosh|Mac OS X/.test(navigator.userAgent)
const hotkeyPlaceholder = isMacOS ? 'Command+Option+T' : 'Ctrl+Alt+T'
const hotkeyHelp = isMacOS
  ? '点击后直接按下组合键；支持 Command(⌘)、Option(⌥)、Shift 加字母、数字或 F1-F24'
  : '点击后直接按下组合键；支持 Ctrl、Alt、Shift、Win 加字母、数字或 F1-F24'
const fontPlatformLabel = isMacOS ? 'macOS' : 'Windows'
const saveShortcutLabel = isMacOS ? '⌘' : 'Ctrl'

const activeMeta = computed(() => sections.find((section) => section.id === activeSection.value) ?? sections[0])
const subtitlePreviewStyle = computed(() => {
  const subtitle = settings.value?.subtitle
  if (!subtitle) {
    return {}
  }
  return {
    color: subtitle.text_color,
    fontFamily: subtitle.font_family,
    fontSize: `${Math.min(subtitle.font_size, 34)}px`,
    WebkitTextStroke: `${subtitle.outline_width}px ${subtitle.outline_color}`,
    textShadow: `0 ${subtitle.shadow_offset_y}px ${subtitle.shadow_blur}px rgb(0 0 0 / ${subtitle.shadow_opacity})`,
  }
})
const apiKeyPlaceholder = computed(() => {
  if (settings.value?.llm.api_key_configured) {
    return '已安全配置，未修改时留空保持不变'
  }
  return '输入 API Key'
})

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  return String(error || '未知错误')
}

async function loadSettings(): Promise<void> {
  const loadToken = ++settingsLoadToken
  loading.value = true
  errorMessage.value = ''
  try {
    const loaded = await runtimeBridge.getSettings()
    if (loadToken !== settingsLoadToken) {
      return
    }
    settings.value = loaded
    temperatureEnabled.value = loaded.llm.temperature !== null && loaded.llm.temperature !== undefined
    shortcutCaptureMessage.value = ''
    await nextTick()
    observeSubtitlePreview()
    scheduleNativeSubtitlePreview()
  } catch (error) {
    if (loadToken !== settingsLoadToken) {
      return
    }
    errorMessage.value = `加载设置失败：${errorText(error)}`
  } finally {
    if (loadToken === settingsLoadToken) {
      loading.value = false
    }
  }
}

async function loadFonts(): Promise<void> {
  try {
    fontFamilies.value = await runtimeBridge.getAvailableFonts()
  } catch {
    fontFamilies.value = []
  }
}

function observeSubtitlePreview(): void {
  if (isMacOS || !previewResizeObserver || !subtitlePreviewElement.value) {
    return
  }
  previewResizeObserver.disconnect()
  previewResizeObserver.observe(subtitlePreviewElement.value)
}

function scheduleNativeSubtitlePreview(): void {
  if (isMacOS || !settings.value || !subtitlePreviewElement.value) {
    return
  }
  if (nativePreviewTimer !== null) {
    window.clearTimeout(nativePreviewTimer)
  }
  nativePreviewTimer = window.setTimeout(() => {
    nativePreviewTimer = null
    void renderNativeSubtitlePreview()
  }, 120)
}

async function renderNativeSubtitlePreview(): Promise<void> {
  const element = subtitlePreviewElement.value
  const subtitle = settings.value?.subtitle
  if (isMacOS || !element || !subtitle) {
    return
  }
  const bounds = element.getBoundingClientRect()
  const width = Math.round(bounds.width)
  const height = Math.round(bounds.height)
  if (width <= 0 || height <= 0) {
    return
  }
  const request = ++nativePreviewRequest
  try {
    const image = await runtimeBridge.renderSubtitlePreview(subtitle, width, height, window.devicePixelRatio || 1)
    if (request === nativePreviewRequest) {
      nativeSubtitlePreview.value = image
    }
  } catch {
    // 原生预览失败时保留 CSS 预览，不能影响设置编辑。
  }
}

function markAPIKeyChanged(): void {
  if (settings.value) {
    settings.value.llm.api_key_changed = true
  }
}

function toggleTemperature(): void {
  if (!settings.value) {
    return
  }
  if (temperatureEnabled.value && settings.value.llm.temperature === null) {
    settings.value.llm.temperature = 1
  }
  if (!temperatureEnabled.value) {
    settings.value.llm.temperature = null
  }
}

function shortcutKey(event: KeyboardEvent): string | null {
  const code = event.code
  if (/^Key[A-Z]$/.test(code)) {
    return code.slice(3)
  }
  if (/^Digit[0-9]$/.test(code)) {
    return code.slice(5)
  }
  if (/^F(?:[1-9]|1[0-9]|2[0-4])$/.test(code)) {
    return code
  }

  const key = event.key.toUpperCase()
  if (/^[A-Z0-9]$/.test(key)) {
    return key
  }
  if (/^F(?:[1-9]|1[0-9]|2[0-4])$/.test(key)) {
    return key
  }
  return null
}

function captureShortcut(event: KeyboardEvent): void {
  if (!settings.value?.selection.enable) {
    return
  }

  const key = shortcutKey(event)
  if (key === null) {
    if (['Control', 'Alt', 'Shift', 'Meta'].includes(event.key)) {
      event.preventDefault()
      shortcutCaptureMessage.value = '继续按下字母、数字或 F1-F24'
    }
    return
  }

  const modifiers: string[] = []
  if (isMacOS && event.metaKey) {
    modifiers.push('Command')
  }
  if (!isMacOS && event.ctrlKey) {
    modifiers.push('Ctrl')
  }
  if (event.altKey) {
    modifiers.push(isMacOS ? 'Option' : 'Alt')
  }
  if (event.shiftKey) {
    modifiers.push('Shift')
  }
  if (isMacOS && event.ctrlKey) {
    modifiers.push('Ctrl')
  }
  if (!isMacOS && event.metaKey) {
    modifiers.push('Win')
  }

  event.preventDefault()
  if (modifiers.length === 0) {
    shortcutCaptureMessage.value = '请同时按下至少一个修饰键'
    return
  }

  settings.value.selection.hotkey = [...modifiers, key].join('+')
  shortcutCaptureMessage.value = `已录入 ${settings.value.selection.hotkey}`
}

async function saveSettings(): Promise<void> {
  if (!settings.value || saving.value) {
    return
  }
  errorMessage.value = ''
  successMessage.value = ''
  saving.value = true
  try {
    if (!temperatureEnabled.value) {
      settings.value.llm.temperature = null
    }
    const payload = JSON.parse(JSON.stringify(settings.value)) as SettingsData
    const apiKeyChanged = settings.value.llm.api_key_changed
    const apiKeyConfigured = settings.value.llm.api_key.trim() !== ''
    await runtimeBridge.saveSettings(payload)
    if (apiKeyChanged) {
      settings.value.llm.api_key_configured = apiKeyConfigured
    }
    settings.value.llm.api_key = ''
    settings.value.llm.api_key_changed = false
    successMessage.value = '设置已保存并立即生效'
    if (successTimer !== null) {
      window.clearTimeout(successTimer)
    }
    successTimer = window.setTimeout(() => {
      successMessage.value = ''
      successTimer = null
    }, 2800)
  } catch (error) {
    errorMessage.value = errorText(error)
  } finally {
    saving.value = false
  }
}

async function closeSettings(): Promise<void> {
  if (!saving.value) {
    try {
      await runtimeBridge.closeSettings()
    } catch (error) {
      errorMessage.value = `关闭设置失败：${errorText(error)}`
    }
  }
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    void closeSettings()
  }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    void saveSettings()
  }
}

watch(() => settings.value?.subtitle, () => {
  void nextTick(scheduleNativeSubtitlePreview)
}, {deep: true})
watch(activeSection, () => {
  void nextTick(scheduleNativeSubtitlePreview)
})

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
  if (!isMacOS && typeof ResizeObserver !== 'undefined') {
    previewResizeObserver = new ResizeObserver(scheduleNativeSubtitlePreview)
  }
  removeRefreshListener = runtimeBridge.on(settingsRefreshEvent, () => {
    if (!saving.value) {
      void loadSettings()
    }
  })
  void loadSettings()
  void loadFonts()
})

onBeforeUnmount(() => {
  settingsLoadToken++
  window.removeEventListener('keydown', handleKeydown)
  removeRefreshListener?.()
  if (successTimer !== null) {
    window.clearTimeout(successTimer)
  }
  if (nativePreviewTimer !== null) {
    window.clearTimeout(nativePreviewTimer)
  }
  nativePreviewRequest++
  previewResizeObserver?.disconnect()
})
</script>

<template>
  <section class="settings-stage" data-testid="settings-view">
    <header class="settings-titlebar">
      <div class="brand-lockup">
        <span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span>
        <div>
          <strong>悬浮翻译器</strong>
          <span>CONTROL PANEL</span>
        </div>
      </div>
      <button class="window-close" type="button" aria-label="关闭设置" data-testid="close-settings" @click="closeSettings">
        <span></span><span></span>
      </button>
    </header>

    <div class="settings-layout">
      <aside class="settings-nav" aria-label="设置分类">
        <div class="nav-caption">CONFIGURATION</div>
        <button
          v-for="section in sections"
          :key="section.id"
          class="nav-item"
          :class="{'nav-item--active': activeSection === section.id}"
          type="button"
          @click="activeSection = section.id"
        >
          <span class="nav-index">{{ section.index }}</span>
          <span class="nav-copy">
            <strong>{{ section.label }}</strong>
            <small>{{ section.description }}</small>
          </span>
          <span class="nav-arrow">→</span>
        </button>
        <div class="nav-footnote">
          <span class="status-dot"></span>
          保存后自动重新加载
        </div>
      </aside>

      <main class="settings-content">
        <div class="section-heading">
          <div>
            <span>{{ activeMeta.index }} / SETTINGS</span>
            <h1>{{ activeMeta.label }}</h1>
          </div>
          <p>{{ activeMeta.description }}</p>
        </div>

        <div v-if="loading" class="settings-loading">
          <span></span>
          正在读取配置
        </div>

        <form v-else-if="settings" class="settings-form" @submit.prevent="saveSettings">
          <div v-show="activeSection === 'model'" class="form-section">
            <div class="field-grid field-grid--two">
              <label class="field">
                <span class="field-label">接口类型</span>
                <input v-model="settings.llm.provider" readonly />
                <small>当前支持 OpenAI-compatible 接口</small>
              </label>
              <label class="field">
                <span class="field-label">模型名称</span>
                <input v-model.trim="settings.llm.model" required placeholder="例如 gpt-5.3-codex-spark" />
              </label>
            </div>
            <label class="field">
              <span class="field-label">Base URL</span>
              <input v-model.trim="settings.llm.base_url" required type="url" placeholder="https://api.example.com/v1" />
              <small>环境变量 LLM_BASE_URL 的优先级更高</small>
            </label>
            <label class="field">
              <span class="field-label-row">
                <span class="field-label">API Key</span>
                <span v-if="settings.llm.api_key_configured" class="configured-badge">已配置</span>
              </span>
              <input
                v-model="settings.llm.api_key"
                data-testid="api-key"
                type="password"
                autocomplete="new-password"
                :placeholder="apiKeyPlaceholder"
                @input="markAPIKeyChanged"
              />
              <small>已有密钥不会返回界面；未修改时留空会保留，输入后清空并保存会移除</small>
            </label>
            <div class="field-grid field-grid--two">
              <label class="field">
                <span class="field-label">请求超时</span>
                <span class="input-with-unit"><input v-model.number="settings.llm.timeout_seconds" max="300" min="1" required type="number" /><b>秒</b></span>
              </label>
              <div class="field">
                <span class="field-label">采样温度</span>
                <label class="inline-switch">
                  <input v-model="temperatureEnabled" type="checkbox" @change="toggleTemperature" />
                  <span class="switch-track"><i></i></span>
                  <span>显式发送 temperature</span>
                </label>
                <input
                  v-if="temperatureEnabled"
                  v-model.number="settings.llm.temperature"
                  class="temperature-input"
                  max="2"
                  min="0"
                  step="0.1"
                  type="number"
                />
                <small>Codex 和推理模型建议关闭</small>
              </div>
            </div>
          </div>

          <div v-show="activeSection === 'clipboard'" class="form-section">
            <label class="setting-row setting-row--primary">
              <span><strong>监听剪贴板</strong><small>复制文本后自动翻译</small></span>
              <input v-model="settings.clipboard.enable" class="native-toggle" type="checkbox" />
            </label>
            <div class="field-grid field-grid--two">
              <label class="field"><span class="field-label">防抖时间</span><span class="input-with-unit"><input v-model.number="settings.clipboard.debounce_ms" max="60000" min="0" type="number" /><b>ms</b></span></label>
              <label class="field"><span class="field-label">最大文本长度</span><span class="input-with-unit"><input v-model.number="settings.clipboard.max_text_length" max="100000" min="1" type="number" /><b>字符</b></span></label>
            </div>
            <div class="setting-list">
              <label class="setting-row"><span><strong>跳过 URL</strong><small>整段内容为网页地址时不翻译</small></span><input v-model="settings.clipboard.skip_url" class="native-toggle" type="checkbox" /></label>
              <label class="setting-row"><span><strong>跳过代码</strong><small>识别到明显代码结构时不翻译</small></span><input v-model="settings.clipboard.skip_code" class="native-toggle" type="checkbox" /></label>
              <label class="setting-row"><span><strong>保护敏感内容</strong><small>阻止疑似密钥、Token 和密码</small></span><input v-model="settings.clipboard.skip_sensitive" class="native-toggle" type="checkbox" /></label>
              <label class="setting-row"><span><strong>仅翻译英文</strong><small>按 ASCII 英文字母比例和英文词汇过滤，其他拉丁文字会跳过</small></span><input v-model="settings.clipboard.only_translate_english" class="native-toggle" type="checkbox" /></label>
            </div>
            <div class="field-grid field-grid--two">
              <label class="field"><span class="field-label">英文最低比例</span><input v-model.number="settings.clipboard.english_min_ratio" max="1" min="0" step="0.05" type="number" /></label>
              <label class="field"><span class="field-label">中文最高比例</span><input v-model.number="settings.clipboard.chinese_max_ratio" max="1" min="0" step="0.05" type="number" /></label>
            </div>
          </div>

          <div v-show="activeSection === 'selection'" class="form-section">
            <label class="setting-row setting-row--primary">
              <span><strong>启用划词翻译</strong><small>选中文本后按全局快捷键发起翻译</small></span>
              <input v-model="settings.selection.enable" class="native-toggle" type="checkbox" />
            </label>
            <label class="field field--feature">
              <span class="field-label">全局快捷键</span>
              <input
                :value="settings.selection.hotkey"
                :disabled="!settings.selection.enable"
                data-testid="selection-hotkey"
                readonly
                required
                :placeholder="hotkeyPlaceholder"
                @keydown="captureShortcut"
              />
              <small>{{ shortcutCaptureMessage || hotkeyHelp }}</small>
            </label>
            <label class="setting-row setting-row--warning">
              <span><strong>强制兼容</strong><small>仅在原剪贴板只有纯文本时模拟 {{ isMacOS ? 'Command+C' : 'Ctrl+C' }} 并恢复；复杂格式或期间出现新的复制内容会取消操作</small></span>
              <input v-model="settings.selection.compatibility_mode" data-testid="selection-compatibility" :disabled="!settings.selection.enable" class="native-toggle" type="checkbox" />
            </label>
            <div class="info-card">
              <span class="info-card__key">HYBRID</span>
              <div><strong>分层读取选区</strong><p>{{ isMacOS ? '优先使用 macOS Accessibility；开启强制兼容后，仅在直接读取失败时模拟复制。' : '优先使用 Windows UI Automation 和 Win32；开启强制兼容后，仅在直接读取失败时模拟复制。' }}</p></div>
            </div>
          </div>

          <div v-show="activeSection === 'subtitle'" class="form-section">
            <div class="field-grid field-grid--three">
              <label class="field"><span class="field-label">字幕宽度</span><span class="input-with-unit"><input v-model.number="settings.subtitle.width_percent" max="100" min="20" type="number" /><b>%</b></span></label>
              <label class="field"><span class="field-label">距底部</span><span class="input-with-unit"><input v-model.number="settings.subtitle.bottom_offset_percent" data-testid="bottom-offset" max="50" min="0" type="number" /><b>%</b></span></label>
              <label class="field"><span class="field-label">字号</span><span class="input-with-unit"><input v-model.number="settings.subtitle.font_size" max="96" min="12" type="number" /><b>px</b></span></label>
            </div>
            <label class="field">
              <span class="field-label">字体</span>
              <select v-model="settings.subtitle.font_family" data-testid="font-family">
                <option v-for="font in fontFamilies" :key="font" :value="font">{{ font }}</option>
              </select>
              <small>已读取 {{ fontFamilies.length }} 个 {{ fontPlatformLabel }} 字体。</small>
            </label>
            <div class="field-grid field-grid--three">
              <label class="field"><span class="field-label">文字颜色</span><span class="color-input"><input v-model="settings.subtitle.text_color" data-testid="text-color" type="color" /><code>{{ settings.subtitle.text_color }}</code></span></label>
              <label class="field"><span class="field-label">描边宽度</span><span class="input-with-unit"><input v-model.number="settings.subtitle.outline_width" max="6" min="0" type="number" /><b>px</b></span></label>
              <label class="field"><span class="field-label">描边颜色</span><span class="color-input"><input v-model="settings.subtitle.outline_color" data-testid="outline-color" type="color" /><code>{{ settings.subtitle.outline_color }}</code></span></label>
            </div>
            <div class="field-grid field-grid--three">
              <label class="field"><span class="field-label">阴影偏移</span><span class="input-with-unit"><input v-model.number="settings.subtitle.shadow_offset_y" max="24" min="0" type="number" /><b>px</b></span></label>
              <label class="field"><span class="field-label">阴影模糊</span><span class="input-with-unit"><input v-model.number="settings.subtitle.shadow_blur" max="32" min="0" type="number" /><b>px</b></span></label>
              <label class="field"><span class="field-label">阴影透明度</span><input v-model.number="settings.subtitle.shadow_opacity" max="1" min="0" step="0.05" type="number" /></label>
            </div>
            <div ref="subtitlePreviewElement" class="subtitle-preview">
              <span>PREVIEW</span>
              <img v-if="!isMacOS && nativeSubtitlePreview" class="subtitle-preview__image" :src="nativeSubtitlePreview" alt="字幕原生渲染预览" />
              <p v-else :style="subtitlePreviewStyle">翻译结果将在这里清晰呈现</p>
              <small>距屏幕底部 {{ settings.subtitle.bottom_offset_percent }}%</small>
            </div>
            <label class="field"><span class="field-label">最大行数</span><input v-model.number="settings.subtitle.max_lines" max="10" min="1" type="number" /></label>
            <div class="field-grid field-grid--three">
              <label class="field"><span class="field-label">淡入</span><span class="input-with-unit"><input v-model.number="settings.subtitle.fade_in_ms" max="60000" min="0" type="number" /><b>ms</b></span></label>
              <label class="field"><span class="field-label">停留</span><span class="input-with-unit"><input v-model.number="settings.subtitle.display_ms" max="60000" min="0" type="number" /><b>ms</b></span></label>
              <label class="field"><span class="field-label">淡出</span><span class="input-with-unit"><input v-model.number="settings.subtitle.fade_out_ms" max="60000" min="0" type="number" /><b>ms</b></span></label>
            </div>
          </div>

          <div v-show="activeSection === 'logging'" class="form-section">
            <label class="field">
              <span class="field-label">日志级别</span>
              <select v-model="settings.app.log_level">
                <option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option>
              </select>
            </label>
            <label class="setting-row setting-row--warning">
              <span><strong>记录原始文本</strong><small>可能在日志中保留普通剪贴板内容，敏感凭据仍会脱敏</small></span>
              <input v-model="settings.logging.include_source_text" class="native-toggle" type="checkbox" />
            </label>
            <div class="field-grid field-grid--two">
              <label class="field"><span class="field-label">单个日志上限</span><span class="input-with-unit"><input v-model.number="settings.logging.max_size_mb" max="1024" min="1" type="number" /><b>MB</b></span></label>
              <label class="field"><span class="field-label">保留备份数</span><input v-model.number="settings.logging.max_backups" max="100" min="0" type="number" /></label>
            </div>
          </div>
        </form>

        <div v-if="errorMessage" class="message message--error" role="alert">{{ errorMessage }}</div>
        <div v-if="successMessage" class="message message--success" role="status">{{ successMessage }}</div>
      </main>
    </div>

    <footer class="settings-actions">
      <span><kbd>Esc</kbd> 关闭设置 <i></i> <kbd>{{ saveShortcutLabel }}</kbd> + <kbd>S</kbd> 保存</span>
      <div>
        <button class="button button--quiet" type="button" @click="closeSettings">取消</button>
        <button class="button button--save" type="button" :disabled="loading || saving || !settings" data-testid="save-settings" @click="saveSettings">
          <span v-if="saving" class="button-spinner"></span>{{ saving ? '正在保存' : '保存并应用' }}
        </button>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.settings-stage {
  --panel: #ffffff;
  --panel-soft: #f8f6f1;
  --line: #e8e3d9;
  --line-strong: #d8d0c2;
  --text: #24201a;
  --muted: #81786d;
  --amber: #d89524;
  --amber-soft: #fff1d7;
  --green: #2f9b70;
  position: relative;
  display: grid;
  grid-template-rows: 58px minmax(0, 1fr) 72px;
  width: 100%;
  height: 100%;
  overflow: hidden;
  color: var(--text);
  font-family: "Microsoft YaHei UI", "Noto Sans CJK SC", sans-serif;
  user-select: text;
  pointer-events: auto;
  background:
    radial-gradient(circle at 86% -12%, rgba(255, 204, 111, .3), transparent 31%),
    linear-gradient(135deg, #faf9f6, #f1eee7);
  background-size: auto;
}

.settings-stage::after {
  position: absolute;
  inset: 0;
  z-index: 8;
  pointer-events: none;
  content: '';
  border: 1px solid rgba(188, 174, 151, .32);
  border-radius: 18px;
}

.settings-titlebar {
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px 0 22px;
  background: rgba(255, 255, 255, .92);
  border-bottom: 1px solid var(--line);
  box-shadow: 0 3px 18px rgba(86, 72, 49, .05);
  --wails-draggable: drag;
}

.brand-lockup { display: flex; gap: 12px; align-items: center; }
.brand-lockup strong { display: block; font-size: 14px; font-weight: 650; letter-spacing: .08em; }
.brand-lockup span:not(.brand-mark) { display: block; margin-top: 2px; color: #9d917f; font-family: Bahnschrift, sans-serif; font-size: 9px; letter-spacing: .2em; }
.brand-mark { display: flex; gap: 3px; align-items: end; width: 25px; height: 24px; padding: 5px; border: 1px solid rgba(216,149,36,.55); border-radius: 8px; background: #fff8eb; }
.brand-mark i { width: 3px; background: var(--amber); }
.brand-mark i:nth-child(1) { height: 6px; opacity: .55; }
.brand-mark i:nth-child(2) { height: 13px; }
.brand-mark i:nth-child(3) { height: 9px; opacity: .75; }

.window-close { position: relative; width: 38px; height: 34px; padding: 0; background: transparent; border: 0; --wails-draggable: no-drag; }
.window-close { border-radius: 10px; }
.window-close:hover { background: #d95c55; }
.window-close span { position: absolute; top: 16px; left: 12px; width: 14px; height: 1px; background: #756c61; transform: rotate(45deg); }
.window-close span:last-child { transform: rotate(-45deg); }

.settings-layout { display: grid; grid-template-columns: 248px minmax(0, 1fr); min-height: 0; }
.settings-nav { display: flex; flex-direction: column; min-height: 0; padding: 25px 14px 18px; background: rgba(255, 255, 255, .74); border-right: 1px solid var(--line); }
.nav-caption { padding: 0 12px 13px; color: #aa9d8a; font-family: Bahnschrift, sans-serif; font-size: 9px; letter-spacing: .22em; }
.nav-item { display: grid; grid-template-columns: 30px 1fr 18px; gap: 8px; align-items: center; width: 100%; min-height: 63px; padding: 9px 12px; color: var(--muted); text-align: left; background: transparent; border: 1px solid transparent; border-radius: 12px; transition: 150ms ease; }
.nav-item:hover { color: var(--text); background: #fffaf1; }
.nav-item--active { color: #5d451c; background: var(--amber-soft); border-color: #f3d6a1; box-shadow: 0 5px 13px rgba(204, 145, 43, .12); }
.nav-index { color: #b1a694; font-family: Bahnschrift, sans-serif; font-size: 10px; }
.nav-item--active .nav-index, .nav-item--active .nav-arrow { color: var(--amber); }
.nav-copy strong, .nav-copy small { display: block; }
.nav-copy strong { font-size: 13px; font-weight: 600; letter-spacing: .03em; }
.nav-copy small { margin-top: 4px; color: #9c9286; font-size: 10px; }
.nav-arrow { color: #b5aa9c; font-size: 14px; }
.nav-footnote { display: flex; gap: 8px; align-items: center; margin-top: auto; padding: 13px 12px 0; color: #988d80; font-size: 10px; border-top: 1px solid var(--line); }
.status-dot { width: 6px; height: 6px; background: var(--green); border-radius: 50%; box-shadow: 0 0 10px rgba(47,155,112,.35); }

.settings-content { position: relative; min-width: 0; overflow-x: hidden; overflow-y: auto; padding: 28px 38px 36px; scrollbar-color: #c3b9ab transparent; scrollbar-width: thin; }
.section-heading { display: flex; align-items: end; justify-content: space-between; margin-bottom: 25px; padding-bottom: 18px; border-bottom: 1px solid var(--line); }
.section-heading span { color: var(--amber); font-family: Bahnschrift, sans-serif; font-size: 9px; letter-spacing: .2em; }
.section-heading h1 { margin: 6px 0 0; font-family: Bahnschrift, "Microsoft YaHei UI", sans-serif; font-size: 25px; font-weight: 500; letter-spacing: .04em; }
.section-heading p { margin: 0 0 3px; color: var(--muted); font-size: 11px; }
.settings-loading { display: flex; gap: 12px; align-items: center; min-height: 300px; justify-content: center; color: var(--muted); font-size: 12px; }
.settings-loading span, .button-spinner { width: 14px; height: 14px; border: 2px solid rgba(240,180,75,.25); border-top-color: var(--amber); border-radius: 50%; animation: spin .7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.settings-form, .form-section { display: grid; gap: 18px; }
.field-grid { display: grid; gap: 16px; }
.field-grid--two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.field-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.field { display: grid; gap: 8px; min-width: 0; }
.field--feature { max-width: 520px; padding-top: 8px; }
.field-label, .field-label-row { color: #4b443a; font-size: 11px; font-weight: 600; letter-spacing: .04em; }
.field-label-row { display: flex; align-items: center; justify-content: space-between; }
.field small { color: #918678; font-size: 10px; line-height: 1.5; }
.configured-badge { padding: 2px 7px; color: var(--green); font-size: 9px; font-weight: 500; background: rgba(112,214,167,.08); border: 1px solid rgba(112,214,167,.2); }

input, select { width: 100%; height: 39px; padding: 0 12px; color: #423a31; font: 12px Bahnschrift, "Microsoft YaHei UI", sans-serif; background: #ffffff; border: 1px solid var(--line-strong); border-radius: 11px; outline: none; box-shadow: 0 2px 5px rgba(77, 63, 43, .03); transition: 140ms ease; }
input:hover, select:hover { border-color: #c6ae82; }
input:focus, select:focus { border-color: var(--amber); box-shadow: 0 0 0 3px rgba(216,149,36,.13); }
input:disabled, input[readonly] { color: #a3988a; background: #f4f1eb; cursor: not-allowed; }
button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid var(--amber); outline-offset: 2px; }
.input-with-unit { display: grid; grid-template-columns: 1fr auto; align-items: center; background: #ffffff; border: 1px solid var(--line-strong); border-radius: 11px; box-shadow: 0 2px 5px rgba(77, 63, 43, .03); }
.input-with-unit:focus-within { border-color: var(--amber); box-shadow: 0 0 0 3px rgba(216,149,36,.13); }
.input-with-unit input { border: 0; box-shadow: none; }
.input-with-unit b { padding-right: 12px; color: #9c9184; font-family: Bahnschrift, sans-serif; font-size: 9px; font-weight: 400; }
.color-input { display: grid; grid-template-columns: 48px 1fr; align-items: center; height: 39px; padding: 4px 10px 4px 4px; background: #ffffff; border: 1px solid var(--line-strong); border-radius: 11px; box-shadow: 0 2px 5px rgba(77, 63, 43, .03); }
.color-input:focus-within { border-color: var(--amber); box-shadow: 0 0 0 3px rgba(216,149,36,.13); }
.color-input input { height: 29px; padding: 0; cursor: pointer; border: 0; border-radius: 7px; }
.color-input code { color: #786f63; font: 10px Bahnschrift, monospace; letter-spacing: .08em; text-align: right; }

.setting-list { display: grid; border-top: 1px solid var(--line); }
.setting-row { display: flex; align-items: center; justify-content: space-between; min-height: 62px; padding: 12px 4px; border-bottom: 1px solid var(--line); cursor: pointer; }
.setting-row--primary, .setting-row--warning { padding: 17px 18px; background: #ffffff; border: 1px solid var(--line-strong); border-radius: 14px; box-shadow: 0 5px 16px rgba(80, 65, 43, .04); }
.setting-row--primary { border-color: #f0d49f; }
.setting-row--warning { border-color: #e8cfaa; }
.setting-row strong, .setting-row small { display: block; }
.setting-row strong { color: #4a4137; font-size: 12px; font-weight: 600; }
.setting-row small { margin-top: 4px; color: #8e8376; font-size: 10px; }
.native-toggle { position: relative; width: 38px; height: 21px; padding: 0; appearance: none; background: #d5cec3; border: 0; border-radius: 10px; cursor: pointer; }
.native-toggle::after { position: absolute; top: 3px; left: 3px; width: 15px; height: 15px; content: ''; background: #ffffff; border-radius: 50%; transition: 160ms ease; }
.native-toggle:checked { background: var(--amber); }
.native-toggle:checked::after { left: 20px; background: #ffffff; }
.inline-switch { display: flex; gap: 9px; align-items: center; min-height: 39px; color: #766d62; font-size: 11px; cursor: pointer; }
.inline-switch input { position: absolute; opacity: 0; pointer-events: none; }
.switch-track { position: relative; width: 31px; height: 16px; background: #d5cec3; border-radius: 10px; }
.switch-track i { position: absolute; top: 3px; left: 3px; width: 10px; height: 10px; background: #ffffff; border-radius: 50%; transition: 150ms; }
.inline-switch input:checked + .switch-track { background: var(--amber); }
.inline-switch input:checked + .switch-track i { left: 18px; background: #ffffff; }
.temperature-input { margin-top: -4px; }

.info-card { display: grid; grid-template-columns: 70px 1fr; gap: 18px; align-items: center; margin-top: 8px; padding: 20px; background: #f4fbf7; border: 1px solid #cfe8da; border-radius: 16px; }
.info-card__key { color: var(--green); font: 10px Bahnschrift, sans-serif; letter-spacing: .18em; }
.info-card strong { font-size: 12px; }
.info-card p { margin: 5px 0 0; color: #7f897f; font-size: 10px; line-height: 1.6; }
.subtitle-preview { position: relative; display: grid; height: 158px; min-height: 158px; place-items: center; padding: 34px; overflow: hidden; background: linear-gradient(rgba(119, 108, 92, .055) 1px, transparent 1px), linear-gradient(90deg, rgba(119, 108, 92, .055) 1px, transparent 1px), #ebe8e2; background-size: 18px 18px; border: 1px solid #d5cec2; border-radius: 18px; box-shadow: inset 0 1px 0 rgba(255,255,255,.95), 0 10px 24px rgba(43, 34, 21, .08); }
.subtitle-preview::before { position: absolute; inset: 13px; z-index: 1; content: ''; border: 1px dashed rgba(111, 99, 83, .22); border-radius: 12px; }
.subtitle-preview > span { position: absolute; top: 18px; left: 20px; z-index: 3; color: #8f8475; font: 8px Bahnschrift, sans-serif; letter-spacing: .2em; }
.subtitle-preview p { z-index: 2; margin: 0; font-weight: 550; text-align: center; overflow-wrap: anywhere; }
.subtitle-preview__image { position: absolute; inset: 0; z-index: 2; display: block; width: 100%; height: 100%; object-fit: contain; pointer-events: none; }
.subtitle-preview small { position: absolute; right: 18px; bottom: 15px; z-index: 3; color: var(--amber); font-size: 9px; }

.message { position: sticky; bottom: 0; z-index: 3; margin-top: 18px; padding: 11px 14px; font-size: 11px; border: 1px solid; border-radius: 12px; backdrop-filter: blur(12px); box-shadow: 0 8px 20px rgba(70, 55, 34, .1); }
.message--error { color: #9e3631; background: #fff1ef; border-color: #f0c4c0; }
.message--success { color: #237553; background: #effaf4; border-color: #c3e5d3; }

.settings-actions { z-index: 2; display: flex; align-items: center; justify-content: space-between; padding: 0 24px 0 28px; background: rgba(255, 255, 255, .92); border-top: 1px solid var(--line); box-shadow: 0 -3px 18px rgba(86, 72, 49, .04); }
.settings-actions > span { display: flex; gap: 6px; align-items: center; color: #94897d; font-size: 9px; }
.settings-actions > span i { width: 1px; height: 12px; margin: 0 8px; background: var(--line-strong); }
kbd { padding: 3px 6px; color: #75695c; font: 9px Bahnschrift, sans-serif; background: #f5f1ea; border: 1px solid #ddd4c7; border-bottom-width: 2px; border-radius: 6px; }
.settings-actions > div { display: flex; gap: 10px; }
.button { min-width: 96px; height: 40px; padding: 0 18px; font-size: 11px; font-weight: 600; letter-spacing: .03em; border: 1px solid; border-radius: 12px; transition: 150ms ease; }
.button--quiet { color: #71675c; background: #ffffff; border-color: var(--line-strong); }
.button--quiet:hover { color: var(--text); background: #faf7f1; border-color: #cfc2ae; }
.button--save { display: flex; gap: 8px; align-items: center; justify-content: center; color: #ffffff; background: linear-gradient(135deg, #e7a432, #c98215); border-color: #cf8c20; box-shadow: 0 8px 18px rgba(201,130,21,.25); }
.button--save:hover { background: linear-gradient(135deg, #f2b342, #d9901c); transform: translateY(-1px); }
.button:disabled { opacity: .45; cursor: not-allowed; }
.button-spinner { width: 12px; height: 12px; border-color: rgba(0,0,0,.2); border-top-color: #1a1c18; }

@media (max-width: 850px) {
  .settings-layout { grid-template-columns: 205px minmax(0, 1fr); }
  .settings-content { padding-right: 24px; padding-left: 24px; }
  .field-grid--three { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { transition-duration: 1ms !important; animation-duration: 1ms !important; }
}
</style>
