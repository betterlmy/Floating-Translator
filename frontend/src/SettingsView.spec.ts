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
    subtitle: {width_percent: 70, bottom_offset_percent: 4, font_size: 28, max_lines: 4, background_opacity: 0.38, fade_in_ms: 200, display_ms: 6000, fade_out_ms: 800},
    logging: {include_source_text: false, max_size_mb: 10, max_backups: 3},
  }
}

describe('SettingsView', () => {
  let callbacks: Map<string, () => void>

  beforeEach(() => {
    callbacks = new Map()
    vi.spyOn(runtimeBridge, 'on').mockImplementation((eventName, callback) => {
      callbacks.set(eventName, callback as () => void)
      return () => callbacks.delete(eventName)
    })
  })

  afterEach(() => vi.restoreAllMocks())

  it('保存完整设置并标记新 API Key', async () => {
    vi.spyOn(runtimeBridge, 'getSettings').mockResolvedValue(settingsFixture())
    const save = vi.spyOn(runtimeBridge, 'saveSettings').mockResolvedValue()
    vi.spyOn(runtimeBridge, 'closeSettings').mockResolvedValue()
    const wrapper = mount(SettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="bottom-offset"]').setValue('12')
    await wrapper.get('button.nav-item:nth-of-type(3)').trigger('click')
    await wrapper.get('[data-testid="selection-compatibility"]').setValue(true)
    await wrapper.get('[data-testid="api-key"]').setValue('new-secret')
    await wrapper.get('[data-testid="save-settings"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalledOnce()
    const saved = save.mock.calls[0][0]
    expect(saved.subtitle.bottom_offset_percent).toBe(12)
    expect(saved.selection.compatibility_mode).toBe(true)
    expect(saved.llm.api_key).toBe('new-secret')
    expect(saved.llm.api_key_changed).toBe(true)
  })

  it('窗口再次显示时重新读取最新配置', async () => {
    const initial = settingsFixture()
    const updated = settingsFixture()
    updated.subtitle.bottom_offset_percent = 11
    const getSettings = vi.spyOn(runtimeBridge, 'getSettings')
      .mockResolvedValueOnce(initial)
      .mockResolvedValueOnce(updated)
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
})
