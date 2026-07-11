<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'

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
let successTimer: number | null = null
let removeRefreshListener: (() => void) | null = null
let settingsLoadToken = 0

const isMacOS = /Macintosh|Mac OS X/.test(navigator.userAgent)
const hotkeyPlaceholder = isMacOS ? 'Command+Option+T' : 'Ctrl+Alt+T'
const hotkeyHelp = isMacOS
  ? '支持 Command(⌘)、Option(⌥)、Shift 加字母、数字或 F1-F24'
  : '支持 Ctrl、Alt、Shift、Win 加字母、数字或 F1-F24'
const fontPlatformLabel = isMacOS ? 'macOS' : 'Windows'
const saveShortcutLabel = isMacOS ? '⌘' : 'Ctrl'

const activeMeta = computed(() => sections.find((section) => section.id === activeSection.value) ?? sections[0])
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

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
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
              <input v-model.trim="settings.selection.hotkey" :disabled="!settings.selection.enable" required :placeholder="hotkeyPlaceholder" />
              <small>{{ hotkeyHelp }}</small>
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
            <div class="subtitle-preview">
              <span>PREVIEW</span>
              <p :style="{fontFamily: settings.subtitle.font_family, fontSize: `${Math.min(settings.subtitle.font_size, 34)}px`}">翻译结果将在这里清晰呈现</p>
              <small>距屏幕底部 {{ settings.subtitle.bottom_offset_percent }}%</small>
            </div>
            <div class="field-grid field-grid--two">
              <label class="field"><span class="field-label">最大行数</span><input v-model.number="settings.subtitle.max_lines" max="10" min="1" type="number" /></label>
              <label class="field"><span class="field-label">背景透明度</span><input v-model.number="settings.subtitle.background_opacity" max="1" min="0" step="0.05" type="number" /></label>
            </div>
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
  --panel: #151a1c;
  --panel-soft: #1b2123;
  --line: rgba(220, 231, 226, 0.12);
  --line-strong: rgba(220, 231, 226, 0.2);
  --text: #eef2ec;
  --muted: #8f9b98;
  --amber: #f0b44b;
  --amber-soft: rgba(240, 180, 75, 0.12);
  --green: #70d6a7;
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
    linear-gradient(rgba(255,255,255,.018) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,.018) 1px, transparent 1px),
    #101416;
  background-size: 32px 32px;
}

.settings-stage::after {
  position: absolute;
  inset: 0;
  z-index: 8;
  pointer-events: none;
  content: '';
  border: 1px solid rgba(255,255,255,.08);
}

.settings-titlebar {
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px 0 22px;
  background: rgba(11, 14, 15, .96);
  border-bottom: 1px solid var(--line);
  --wails-draggable: drag;
}

.brand-lockup { display: flex; gap: 12px; align-items: center; }
.brand-lockup strong { display: block; font-size: 14px; font-weight: 650; letter-spacing: .08em; }
.brand-lockup span:not(.brand-mark) { display: block; margin-top: 2px; color: #697471; font-family: Bahnschrift, sans-serif; font-size: 9px; letter-spacing: .2em; }
.brand-mark { display: flex; gap: 3px; align-items: end; width: 25px; height: 24px; padding: 5px; border: 1px solid rgba(240,180,75,.5); }
.brand-mark i { width: 3px; background: var(--amber); }
.brand-mark i:nth-child(1) { height: 6px; opacity: .55; }
.brand-mark i:nth-child(2) { height: 13px; }
.brand-mark i:nth-child(3) { height: 9px; opacity: .75; }

.window-close { position: relative; width: 38px; height: 34px; padding: 0; background: transparent; border: 0; --wails-draggable: no-drag; }
.window-close:hover { background: #b43b3b; }
.window-close span { position: absolute; top: 16px; left: 12px; width: 14px; height: 1px; background: #cad1ce; transform: rotate(45deg); }
.window-close span:last-child { transform: rotate(-45deg); }

.settings-layout { display: grid; grid-template-columns: 248px minmax(0, 1fr); min-height: 0; }
.settings-nav { display: flex; flex-direction: column; min-height: 0; padding: 25px 14px 18px; background: rgba(17, 21, 23, .94); border-right: 1px solid var(--line); }
.nav-caption { padding: 0 12px 13px; color: #596360; font-family: Bahnschrift, sans-serif; font-size: 9px; letter-spacing: .22em; }
.nav-item { display: grid; grid-template-columns: 30px 1fr 18px; gap: 8px; align-items: center; width: 100%; min-height: 63px; padding: 9px 10px; color: var(--muted); text-align: left; background: transparent; border: 1px solid transparent; border-radius: 2px; transition: 150ms ease; }
.nav-item:hover { color: var(--text); background: rgba(255,255,255,.025); }
.nav-item--active { color: var(--text); background: var(--amber-soft); border-color: rgba(240,180,75,.25); box-shadow: inset 3px 0 0 var(--amber); }
.nav-index { color: #58615f; font-family: Bahnschrift, sans-serif; font-size: 10px; }
.nav-item--active .nav-index, .nav-item--active .nav-arrow { color: var(--amber); }
.nav-copy strong, .nav-copy small { display: block; }
.nav-copy strong { font-size: 13px; font-weight: 600; letter-spacing: .03em; }
.nav-copy small { margin-top: 4px; color: #687370; font-size: 10px; }
.nav-arrow { color: #46504d; font-size: 14px; }
.nav-footnote { display: flex; gap: 8px; align-items: center; margin-top: auto; padding: 13px 12px 0; color: #65706d; font-size: 10px; border-top: 1px solid var(--line); }
.status-dot { width: 6px; height: 6px; background: var(--green); border-radius: 50%; box-shadow: 0 0 10px rgba(112,214,167,.5); }

.settings-content { position: relative; min-width: 0; overflow-x: hidden; overflow-y: auto; padding: 28px 38px 36px; scrollbar-color: #3c4643 transparent; scrollbar-width: thin; }
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
.field-label, .field-label-row { color: #c9d0cd; font-size: 11px; font-weight: 600; letter-spacing: .04em; }
.field-label-row { display: flex; align-items: center; justify-content: space-between; }
.field small { color: #6f7a77; font-size: 10px; line-height: 1.5; }
.configured-badge { padding: 2px 7px; color: var(--green); font-size: 9px; font-weight: 500; background: rgba(112,214,167,.08); border: 1px solid rgba(112,214,167,.2); }

input, select { width: 100%; height: 39px; padding: 0 12px; color: #edf1ec; font: 12px Bahnschrift, "Microsoft YaHei UI", sans-serif; background: #0d1112; border: 1px solid var(--line-strong); border-radius: 2px; outline: none; transition: 140ms ease; }
input:hover, select:hover { border-color: rgba(220,231,226,.3); }
input:focus, select:focus { border-color: var(--amber); box-shadow: 0 0 0 2px rgba(240,180,75,.08); }
input:disabled, input[readonly] { color: #77817e; background: #121617; cursor: not-allowed; }
button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid var(--amber); outline-offset: 2px; }
.input-with-unit { display: grid; grid-template-columns: 1fr auto; align-items: center; background: #0d1112; border: 1px solid var(--line-strong); }
.input-with-unit:focus-within { border-color: var(--amber); box-shadow: 0 0 0 2px rgba(240,180,75,.08); }
.input-with-unit input { border: 0; box-shadow: none; }
.input-with-unit b { padding-right: 12px; color: #65706d; font-family: Bahnschrift, sans-serif; font-size: 9px; font-weight: 400; }

.setting-list { display: grid; border-top: 1px solid var(--line); }
.setting-row { display: flex; align-items: center; justify-content: space-between; min-height: 62px; padding: 12px 4px; border-bottom: 1px solid var(--line); cursor: pointer; }
.setting-row--primary, .setting-row--warning { padding: 17px 18px; background: rgba(255,255,255,.018); border: 1px solid var(--line-strong); }
.setting-row--primary { box-shadow: inset 3px 0 var(--amber); }
.setting-row--warning { box-shadow: inset 3px 0 #b98645; }
.setting-row strong, .setting-row small { display: block; }
.setting-row strong { color: #dce2de; font-size: 12px; font-weight: 600; }
.setting-row small { margin-top: 4px; color: #6f7976; font-size: 10px; }
.native-toggle { position: relative; width: 36px; height: 19px; padding: 0; appearance: none; background: #303836; border: 0; border-radius: 10px; cursor: pointer; }
.native-toggle::after { position: absolute; top: 3px; left: 3px; width: 13px; height: 13px; content: ''; background: #9aa4a1; border-radius: 50%; transition: 160ms ease; }
.native-toggle:checked { background: var(--amber); }
.native-toggle:checked::after { left: 20px; background: #171a19; }
.inline-switch { display: flex; gap: 9px; align-items: center; min-height: 39px; color: #8f9996; font-size: 11px; cursor: pointer; }
.inline-switch input { position: absolute; opacity: 0; pointer-events: none; }
.switch-track { position: relative; width: 31px; height: 16px; background: #303836; border-radius: 10px; }
.switch-track i { position: absolute; top: 3px; left: 3px; width: 10px; height: 10px; background: #9aa4a1; border-radius: 50%; transition: 150ms; }
.inline-switch input:checked + .switch-track { background: var(--amber); }
.inline-switch input:checked + .switch-track i { left: 18px; background: #111514; }
.temperature-input { margin-top: -4px; }

.info-card { display: grid; grid-template-columns: 70px 1fr; gap: 18px; align-items: center; margin-top: 8px; padding: 20px; background: rgba(112,214,167,.035); border: 1px solid rgba(112,214,167,.16); }
.info-card__key { color: var(--green); font: 10px Bahnschrift, sans-serif; letter-spacing: .18em; }
.info-card strong { font-size: 12px; }
.info-card p { margin: 5px 0 0; color: #77827e; font-size: 10px; line-height: 1.6; }
.subtitle-preview { position: relative; display: grid; min-height: 145px; place-items: center; padding: 30px; overflow: hidden; background: radial-gradient(circle at 50% 120%, rgba(240,180,75,.12), transparent 55%), #0b0f10; border: 1px solid var(--line-strong); }
.subtitle-preview::before { position: absolute; inset: 12px; content: ''; border: 1px dashed rgba(255,255,255,.07); }
.subtitle-preview > span { position: absolute; top: 18px; left: 20px; color: #58615f; font: 8px Bahnschrift, sans-serif; letter-spacing: .2em; }
.subtitle-preview p { z-index: 1; margin: 0; color: #f5f6f1; font-weight: 550; text-align: center; text-shadow: 0 2px 10px #000; }
.subtitle-preview small { position: absolute; right: 18px; bottom: 15px; color: var(--amber); font-size: 9px; }

.message { position: sticky; bottom: 0; z-index: 3; margin-top: 18px; padding: 11px 14px; font-size: 11px; border: 1px solid; backdrop-filter: blur(12px); }
.message--error { color: #ffc2b8; background: rgba(91,30,27,.92); border-color: rgba(255,130,110,.25); }
.message--success { color: #baf3d5; background: rgba(19,68,48,.92); border-color: rgba(112,214,167,.25); }

.settings-actions { z-index: 2; display: flex; align-items: center; justify-content: space-between; padding: 0 24px 0 28px; background: #0b0f10; border-top: 1px solid var(--line); }
.settings-actions > span { display: flex; gap: 6px; align-items: center; color: #626d69; font-size: 9px; }
.settings-actions > span i { width: 1px; height: 12px; margin: 0 8px; background: var(--line-strong); }
kbd { padding: 2px 5px; color: #919b98; font: 9px Bahnschrift, sans-serif; background: #181d1f; border: 1px solid #303736; border-bottom-width: 2px; border-radius: 2px; }
.settings-actions > div { display: flex; gap: 10px; }
.button { min-width: 92px; height: 38px; padding: 0 17px; font-size: 11px; font-weight: 600; letter-spacing: .03em; border: 1px solid; border-radius: 2px; }
.button--quiet { color: #9ca6a3; background: transparent; border-color: var(--line-strong); }
.button--quiet:hover { color: var(--text); background: rgba(255,255,255,.035); }
.button--save { display: flex; gap: 8px; align-items: center; justify-content: center; color: #171914; background: var(--amber); border-color: var(--amber); box-shadow: 0 6px 20px rgba(240,180,75,.12); }
.button--save:hover { background: #ffc766; }
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
