import {Events} from '@wailsio/runtime'

import {CloseSettings, FrontendReady, GetAvailableFonts, GetSettings, RenderSubtitlePreview, ReportSubtitleBounds, SaveSettings} from '../bindings/floating-translator/app'

import type {SettingsData} from './settings_types'

export interface RuntimeBridge {
	on<T>(eventName: string, callback: (payload: T) => void): () => void
  ready(): Promise<void>
  getAvailableFonts(): Promise<string[]>
  renderSubtitlePreview(subtitle: SettingsData['subtitle'], width: number, height: number, deviceScale: number): Promise<string>
  reportSubtitleBounds(x: number, y: number, width: number, height: number, visible: boolean): Promise<void>
	getSettings(): Promise<SettingsData>
	saveSettings(settings: SettingsData): Promise<void>
	closeSettings(): Promise<void>
}

export const runtimeBridge: RuntimeBridge = {
  on<T>(eventName: string, callback: (payload: T) => void): () => void {
    return Events.On(eventName, (event) => callback(event.data as T))
  },
	ready(): Promise<void> {
		return FrontendReady()
	},
	getAvailableFonts(): Promise<string[]> {
		return GetAvailableFonts()
	},
  renderSubtitlePreview(subtitle: SettingsData['subtitle'], width: number, height: number, deviceScale: number): Promise<string> {
    return RenderSubtitlePreview(subtitle, width, height, deviceScale)
  },
  reportSubtitleBounds(x: number, y: number, width: number, height: number, visible: boolean): Promise<void> {
    return ReportSubtitleBounds(x, y, width, height, visible)
  },
	getSettings(): Promise<SettingsData> {
		return GetSettings()
	},
	saveSettings(settings: SettingsData): Promise<void> {
		return SaveSettings(settings)
	},
	closeSettings(): Promise<void> {
		return CloseSettings()
	},
}
