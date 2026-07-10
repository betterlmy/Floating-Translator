<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue'

import {runtimeBridge} from './runtime_bridge'
import SettingsView from './SettingsView.vue'

interface TranslationEvent {
  request_id: number
  text: string
  source: string
  timestamp_ms: number
}

interface SubtitleConfig {
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

type SubtitlePhase = 'hidden' | 'entering' | 'visible' | 'leaving'
const defaultConfig: SubtitleConfig = {
  width_percent: 70,
  bottom_offset_percent: 4,
  font_family: 'Microsoft YaHei UI',
  font_size: 28,
  max_lines: 4,
  background_opacity: 0.38,
  fade_in_ms: 200,
  display_ms: 6000,
  fade_out_ms: 800,
}

const subtitleConfig = ref<SubtitleConfig>(defaultConfig)
const subtitleText = ref('')
const phase = ref<SubtitlePhase>('hidden')
const latestRequestID = ref(0)
const isSettingsWindow = new URLSearchParams(window.location.search).get('view') === 'settings'

let animationFrame: number | null = null
let leaveTimer: number | null = null
let hideTimer: number | null = null
let removeTranslationListener: (() => void) | null = null
let removeConfigListener: (() => void) | null = null

const subtitleStyle = computed(() => ({
  '--subtitle-font-size': `${subtitleConfig.value.font_size}px`,
  '--subtitle-background-opacity': String(subtitleConfig.value.background_opacity),
  '--subtitle-fade-in': `${subtitleConfig.value.fade_in_ms}ms`,
  '--subtitle-fade-out': `${subtitleConfig.value.fade_out_ms}ms`,
  '--subtitle-width': '100%',
  '--subtitle-max-lines': String(subtitleConfig.value.max_lines),
}))

function clearAnimation(): void {
  if (animationFrame !== null) {
    cancelAnimationFrame(animationFrame)
    animationFrame = null
  }
  if (leaveTimer !== null) {
    window.clearTimeout(leaveTimer)
    leaveTimer = null
  }
  if (hideTimer !== null) {
    window.clearTimeout(hideTimer)
    hideTimer = null
  }
}

async function showSubtitle(event: TranslationEvent): Promise<void> {
  if (!event.text || event.request_id < latestRequestID.value) {
    return
  }
  latestRequestID.value = event.request_id
  clearAnimation()
  subtitleText.value = event.text
  phase.value = 'entering'

  await nextTick()
  animationFrame = requestAnimationFrame(() => {
    phase.value = 'visible'
    animationFrame = null
  })

  const visibleDuration = subtitleConfig.value.fade_in_ms + subtitleConfig.value.display_ms
  leaveTimer = window.setTimeout(() => {
    phase.value = 'leaving'
    leaveTimer = null
  }, visibleDuration)
  hideTimer = window.setTimeout(() => {
    phase.value = 'hidden'
    subtitleText.value = ''
    hideTimer = null
  }, visibleDuration + subtitleConfig.value.fade_out_ms)
}

function updateConfig(config: SubtitleConfig): void {
  subtitleConfig.value = {...defaultConfig, ...config}
}

onMounted(() => {
  if (isSettingsWindow) {
    void runtimeBridge.ready().catch(() => undefined)
    return
  }
  removeTranslationListener = runtimeBridge.on<TranslationEvent>('translation:result', (event) => {
    void showSubtitle(event)
  })
  removeConfigListener = runtimeBridge.on<SubtitleConfig>('subtitle:config', updateConfig)
  void runtimeBridge.ready().catch(() => undefined)
})

onBeforeUnmount(() => {
  clearAnimation()
  removeTranslationListener?.()
	removeConfigListener?.()
})
</script>

<template>
  <SettingsView v-if="isSettingsWindow" />
  <main v-else class="overlay-stage" aria-live="polite" aria-atomic="true">
    <section
      v-if="phase !== 'hidden'"
      class="subtitle"
      :class="`subtitle--${phase}`"
      :style="subtitleStyle"
      data-testid="subtitle"
    >
      <span class="subtitle__text">{{ subtitleText }}</span>
    </section>
  </main>
</template>
