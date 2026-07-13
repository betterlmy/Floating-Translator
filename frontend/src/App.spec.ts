import {flushPromises, mount} from '@vue/test-utils'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import App from './App.vue'
import {runtimeBridge} from './runtime_bridge'
import type {SettingsData} from './settings_types'

type EventCallback = (payload: unknown) => void

function settingsFixture(): SettingsData {
  return {
    app: {log_level: 'info'},
    clipboard: {enable: true, debounce_ms: 300, max_text_length: 3000, skip_url: true, skip_code: true, skip_sensitive: true, only_translate_english: true, english_min_ratio: 0.5, chinese_max_ratio: 0.3},
    selection: {enable: true, hotkey: 'Ctrl+Alt+T', compatibility_mode: false},
    llm: {provider: 'openai_compatible', base_url: 'https://example.com/v1', api_key: '', api_key_configured: true, api_key_changed: false, model: 'test-model', temperature: null, timeout_seconds: 20},
    subtitle: {width_percent: 70, bottom_offset_percent: 4, font_family: 'Microsoft YaHei UI', font_size: 28, max_lines: 4, background_opacity: 0.38, text_color: '#F8FAFC', outline_width: 1, outline_color: '#000000', shadow_offset_y: 3, shadow_blur: 8, shadow_opacity: 0.88, fade_in_ms: 200, display_ms: 6000, fade_out_ms: 800},
    logging: {include_source_text: false, max_size_mb: 10, max_backups: 3},
  }
}

describe('App', () => {
  let callbacks: Map<string, EventCallback>

  beforeEach(() => {
    vi.useFakeTimers()
    callbacks = new Map()
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(() => callback(0), 0))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id))
    vi.spyOn(runtimeBridge, 'on').mockImplementation((eventName, callback) => {
      callbacks.set(eventName, callback as EventCallback)
      return () => callbacks.delete(eventName)
    })
    vi.spyOn(runtimeBridge, 'ready').mockResolvedValue()
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    vi.spyOn(runtimeBridge, 'reportSubtitleBounds').mockResolvedValue()
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'saveSettings').mockResolvedValue()
    vi.spyOn(runtimeBridge, 'closeSettings').mockResolvedValue()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('按淡入、停留、淡出顺序显示字幕', async () => {
    const wrapper = mount(App)
    await flushPromises()

    callbacks.get('translation:result')?.({
      request_id: 1,
      text: '这是翻译后的字幕。',
      source: 'clipboard',
      timestamp_ms: 1,
    })
    await vi.advanceTimersByTimeAsync(1)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--visible')
    expect(wrapper.text()).toContain('这是翻译后的字幕。')

    await vi.advanceTimersByTimeAsync(6199)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--leaving')
    await vi.advanceTimersByTimeAsync(800)
    expect(wrapper.find('[data-testid="subtitle"]').exists()).toBe(false)
  })

  it('新字幕覆盖旧字幕并重新开始计时', async () => {
    const wrapper = mount(App)
    await flushPromises()
    const emitResult = callbacks.get('translation:result')

    emitResult?.({request_id: 1, text: '旧字幕', source: 'clipboard', timestamp_ms: 1})
    await vi.advanceTimersByTimeAsync(3000)
    emitResult?.({request_id: 2, text: '新字幕', source: 'clipboard', timestamp_ms: 2})
    await vi.advanceTimersByTimeAsync(1)

    expect(wrapper.text()).toContain('新字幕')
    expect(wrapper.text()).not.toContain('旧字幕')
    await vi.advanceTimersByTimeAsync(6198)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--visible')
  })

  it('旧字幕的异步 continuation 不得覆盖新字幕动画', async () => {
    const frame = vi.fn((callback: FrameRequestCallback) => {
      return window.setTimeout(() => callback(0), 0)
    })
    vi.stubGlobal('requestAnimationFrame', frame)
    const wrapper = mount(App)
    await flushPromises()
    const emitResult = callbacks.get('translation:result')

    emitResult?.({request_id: 1, text: '旧字幕', source: 'clipboard', timestamp_ms: 1})
    emitResult?.({request_id: 2, text: '新字幕', source: 'clipboard', timestamp_ms: 2})
    await vi.advanceTimersByTimeAsync(1)

    expect(frame).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('新字幕')
    expect(wrapper.text()).not.toContain('旧字幕')
  })

  it('持久翻译中提示会等待结果覆盖', async () => {
    const wrapper = mount(App)
    await flushPromises()
    const emitResult = callbacks.get('translation:result')

    emitResult?.({request_id: 1, text: '翻译中...', source: 'selection', persistent: true, timestamp_ms: 1})
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('翻译中...')
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--visible')

    emitResult?.({request_id: 2, text: '翻译完成', source: 'selection', persistent: false, timestamp_ms: 2})
    await vi.advanceTimersByTimeAsync(1)
    expect(wrapper.text()).toContain('翻译完成')
    await vi.advanceTimersByTimeAsync(6199)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--leaving')
  })

  it('悬停时保持显示，移开后重新开始完整停留时间', async () => {
    const wrapper = mount(App)
    await flushPromises()
    callbacks.get('translation:result')?.({request_id: 1, text: '悬停字幕', source: 'clipboard', timestamp_ms: 1})
    await vi.advanceTimersByTimeAsync(1)

    callbacks.get('subtitle:hover')?.(true)
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--visible')

    callbacks.get('subtitle:hover')?.(false)
    await vi.advanceTimersByTimeAsync(5999)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--visible')
    await vi.advanceTimersByTimeAsync(1)
    expect(wrapper.get('[data-testid="subtitle"]').classes()).toContain('subtitle--leaving')
    await vi.advanceTimersByTimeAsync(800)
    expect(wrapper.find('[data-testid="subtitle"]').exists()).toBe(false)
  })

  it('卸载时注销 Wails 事件', async () => {
    const wrapper = mount(App)
    await flushPromises()
    expect(callbacks.size).toBe(3)

    wrapper.unmount()
    expect(callbacks.size).toBe(0)
  })

  it('字幕窗口不再订阅设置模式事件', async () => {
    const wrapper = mount(App)
    await flushPromises()

    expect(callbacks.has('application:mode')).toBe(false)
    expect(wrapper.find('[data-testid="settings-view"]').exists()).toBe(false)
    expect(runtimeBridge.getSettings).not.toHaveBeenCalled()
  })
})
