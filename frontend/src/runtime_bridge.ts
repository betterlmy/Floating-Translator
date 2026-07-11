import {Events} from '@wailsio/runtime'

import {CloseSettings, FrontendReady, GetAvailableFonts, GetSettings, SaveSettings} from '../bindings/floating-translator/app'

import type {SettingsData} from './settings_types'

export interface RuntimeBridge {
	on<T>(eventName: string, callback: (payload: T) => void): () => void
  ready(): Promise<void>
  getAvailableFonts(): Promise<string[]>
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
