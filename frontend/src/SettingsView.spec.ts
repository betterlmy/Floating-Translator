import {flushPromises, mount} from '@vue/test-utils'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {runtimeBridge} from './runtime_bridge'
import SettingsView from './SettingsView.vue'
import type {SettingsData} from './settings_types'

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

function deferred<T>(): {promise: Promise<T>; resolve: (value: T) => void} {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return {promise, resolve}
}

describe('SettingsView', () => {
  let callbacks: Map<string, () => void>

  beforeEach(() => {
    callbacks = new Map()
    vi.spyOn(runtimeBridge, 'on').mockImplementation((eventName, callback) => {
      callbacks.set(eventName, callback as () => void)
      return () => callbacks.delete(eventName)
    })
    vi.spyOn(runtimeBridge, 'renderSubtitlePreview').mockResolvedValue('')
  })

  afterEach(() => vi.restoreAllMocks())

  it('保存完整设置并标记新 API Key', async () => {
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue(['Microsoft YaHei UI', 'Segoe UI'])
    const save = vi.spyOn(runtimeBridge, 'saveSettings').mockResolvedValue()
    vi.spyOn(runtimeBridge, 'closeSettings').mockResolvedValue()
    const wrapper = mount(SettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="bottom-offset"]').setValue('12')
    await wrapper.get('[data-testid="font-family"]').setValue('Segoe UI')
    await wrapper.get('button.nav-item:nth-of-type(3)').trigger('click')
    await wrapper.get('[data-testid="selection-compatibility"]').setValue(true)
    await wrapper.get('[data-testid="api-key"]').setValue('new-secret')
    await wrapper.get('[data-testid="save-settings"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalledOnce()
    const saved = save.mock.calls[0][0]
    expect(saved.subtitle.bottom_offset_percent).toBe(12)
    expect(saved.subtitle.font_family).toBe('Segoe UI')
    expect(saved.subtitle.text_color).toBe('#F8FAFC')
    expect(saved.selection.compatibility_mode).toBe(true)
    expect(saved.llm.api_key).toBe('new-secret')
    expect(saved.llm.api_key_changed).toBe(true)
  })

  it('颜色转盘修改后实时更新字幕预览', async () => {
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    const wrapper = mount(SettingsView)
    await flushPromises()

    await wrapper.get('button.nav-item:nth-of-type(4)').trigger('click')
    await wrapper.get('[data-testid="text-color"]').setValue('#ff5500')
    await wrapper.get('[data-testid="outline-color"]').setValue('#112233')

    const preview = wrapper.get('.subtitle-preview p')
    expect(preview.attributes('style')).toContain('color: rgb(255, 85, 0)')
    expect(preview.attributes('style')).toContain('-webkit-text-stroke: 1px #112233')
  })

  it('Windows 设置页使用原生渲染器生成字幕预览', async () => {
    vi.useFakeTimers()
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    const render = vi.spyOn(runtimeBridge, 'renderSubtitlePreview').mockResolvedValue('data:image/png;base64,preview')
    const wrapper = mount(SettingsView)
    await flushPromises()
    const preview = wrapper.get('.subtitle-preview')
    vi.spyOn(preview.element, 'getBoundingClientRect').mockReturnValue({width: 640, height: 158} as DOMRect)

    await wrapper.get('button.nav-item:nth-of-type(4)').trigger('click')
    await vi.advanceTimersByTimeAsync(120)

    expect(render).toHaveBeenCalledWith(expect.objectContaining({font_size: 28}), 640, 158, window.devicePixelRatio || 1)
    expect(wrapper.get('.subtitle-preview__image').attributes('src')).toContain('data:image/png')
    vi.useRealTimers()
  })

  it('输入后清空 API Key 可以保存并清除配置状态', async () => {
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    const save = vi.spyOn(runtimeBridge, 'saveSettings').mockResolvedValue()
    const wrapper = mount(SettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="api-key"]').setValue('temporary-secret')
    await wrapper.get('[data-testid="api-key"]').setValue('')
    await wrapper.get('[data-testid="save-settings"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalledOnce()
    expect(save.mock.calls[0][0].llm.api_key).toBe('')
    expect(save.mock.calls[0][0].llm.api_key_changed).toBe(true)
    expect(wrapper.find('.configured-badge').exists()).toBe(false)
  })

  it('窗口再次显示时重新读取最新配置', async () => {
    const initial = settingsFixture()
    const updated = settingsFixture()
    updated.subtitle.bottom_offset_percent = 11
    const getSettings = vi.spyOn(runtimeBridge, 'getSettings')
      .mockResolvedValueOnce(initial)
      .mockResolvedValueOnce(updated)
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue(['Microsoft YaHei UI'])
    const wrapper = mount(SettingsView)
    await flushPromises()

    await wrapper.get('button.nav-item:nth-of-type(4)').trigger('click')
    expect((wrapper.get('[data-testid="bottom-offset"]').element as HTMLInputElement).value).toBe('4')

    callbacks.get('settings:refresh')?.()
    await flushPromises()

    expect(getSettings).toHaveBeenCalledTimes(2)
    expect((wrapper.get('[data-testid="bottom-offset"]').element as HTMLInputElement).value).toBe('11')
    wrapper.unmount()
    expect(callbacks.has('settings:refresh')).toBe(false)
  })

  it('按下键盘组合键录入全局快捷键', async () => {
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    const wrapper = mount(SettingsView)

    await flushPromises()
    await wrapper.get('button.nav-item:nth-of-type(3)').trigger('click')
    const hotkey = wrapper.get('[data-testid="selection-hotkey"]')
    await hotkey.trigger('keydown', {key: 't', code: 'KeyT', ctrlKey: true, altKey: true})

    expect((hotkey.element as HTMLInputElement).value).toBe('Ctrl+Alt+T')
    expect(wrapper.text()).toContain('已录入 Ctrl+Alt+T')
  })

  it('并发加载时只接受最后一次配置响应', async () => {
    const first = deferred<SettingsData>()
    const second = deferred<SettingsData>()
    const firstSettings = settingsFixture()
    const secondSettings = settingsFixture()
    firstSettings.subtitle.bottom_offset_percent = 3
    secondSettings.subtitle.bottom_offset_percent = 17
    const getSettings = vi.spyOn(runtimeBridge, 'getSettings')
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
    vi.spyOn(runtimeBridge, 'getAvailableFonts').mockResolvedValue([])
    const wrapper = mount(SettingsView)

    callbacks.get('settings:refresh')?.()
    second.resolve(secondSettings)
    await flushPromises()
    first.resolve(firstSettings)
    await flushPromises()

    expect(getSettings).toHaveBeenCalledTimes(2)
    expect((wrapper.get('[data-testid="bottom-offset"]').element as HTMLInputElement).value).toBe('17')
    wrapper.unmount()
  })
})
