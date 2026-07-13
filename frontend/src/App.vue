<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue'

import {runtimeBridge} from './runtime_bridge'
import SettingsView from './SettingsView.vue'

interface TranslationEvent {
  request_id: number
  text: string
  source: string
  persistent?: boolean
  timestamp_ms: number
}

interface SubtitleConfig {
  width_percent: number
  bottom_offset_percent: number
  font_family: string
  font_size: number
  max_lines: number
  background_opacity: number
  text_color: string
  outline_width: number
  outline_color: string
  shadow_offset_y: number
  shadow_blur: number
  shadow_opacity: number
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
  text_color: '#F8FAFC',
  outline_width: 1,
  outline_color: '#000000',
  shadow_offset_y: 3,
  shadow_blur: 8,
  shadow_opacity: 0.88,
  fade_in_ms: 200,
  display_ms: 6000,
  fade_out_ms: 800,
}

const subtitleConfig = ref<SubtitleConfig>(defaultConfig)
const subtitleText = ref('')
const subtitleElement = ref<HTMLElement | null>(null)
const subtitleViewportElement = ref<HTMLElement | null>(null)
const subtitleTextElement = ref<HTMLElement | null>(null)
const subtitleScrollOffset = ref(0)
const persistentSubtitle = ref(false)
const phase = ref<SubtitlePhase>('hidden')
const latestRequestID = ref(0)
const view = new URLSearchParams(window.location.search).get('view')
const isSettingsWindow = view === 'settings'

let animationFrame: number | null = null
let scrollAnimationFrame: number | null = null
let leaveTimer: number | null = null
let hideTimer: number | null = null
let removeTranslationListener: (() => void) | null = null
let removeConfigListener: (() => void) | null = null
let removeHoverListener: (() => void) | null = null
let resizeObserver: ResizeObserver | null = null

const subtitleStyle = computed(() => ({
  '--subtitle-font-family': subtitleConfig.value.font_family,
  '--subtitle-font-size': `${subtitleConfig.value.font_size}px`,
  '--subtitle-text-color': subtitleConfig.value.text_color,
  '--subtitle-outline-width': `${subtitleConfig.value.outline_width}px`,
  '--subtitle-outline-color': subtitleConfig.value.outline_color,
  '--subtitle-shadow-y': `${subtitleConfig.value.shadow_offset_y}px`,
  '--subtitle-shadow-blur': `${subtitleConfig.value.shadow_blur}px`,
  '--subtitle-shadow-opacity': String(subtitleConfig.value.shadow_opacity),
  '--subtitle-fade-in': `${subtitleConfig.value.fade_in_ms}ms`,
  '--subtitle-fade-out': `${subtitleConfig.value.fade_out_ms}ms`,
  '--subtitle-width': '100%',
  '--subtitle-max-lines': String(subtitleConfig.value.max_lines),
  '--subtitle-viewport-height': `${subtitleConfig.value.font_size * 1.6 * subtitleConfig.value.max_lines}px`,
}))

function clearAnimation(): void {
  clearSubtitleScroll()
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

function clearSubtitleScroll(): void {
  if (scrollAnimationFrame !== null) {
    cancelAnimationFrame(scrollAnimationFrame)
    scrollAnimationFrame = null
  }
  subtitleScrollOffset.value = 0
}

function startSubtitleScroll(): void {
  clearSubtitleScroll()
  if (persistentSubtitle.value) {
    return
  }
  const viewport = subtitleViewportElement.value
  const text = subtitleTextElement.value
  if (!viewport || !text) {
    return
  }
  const overflow = text.scrollHeight - viewport.clientHeight
  if (overflow <= 0) {
    return
  }
  const topPause = 1000
  const bottomPause = 1200
  const travel = overflow / 24 * 1000
  const cycle = topPause + travel + bottomPause
  const startedAt = performance.now()
  const tick = (now: number): void => {
    const elapsed = (now - startedAt) % cycle
    if (elapsed <= topPause) {
      subtitleScrollOffset.value = 0
    } else if (elapsed >= topPause + travel) {
      subtitleScrollOffset.value = overflow
    } else {
      subtitleScrollOffset.value = overflow * (elapsed - topPause) / travel
    }
    scrollAnimationFrame = requestAnimationFrame(tick)
  }
  scrollAnimationFrame = requestAnimationFrame(tick)
}

function clearDismissTimers(): void {
  if (leaveTimer !== null) {
    window.clearTimeout(leaveTimer)
    leaveTimer = null
  }
  if (hideTimer !== null) {
    window.clearTimeout(hideTimer)
    hideTimer = null
  }
}

function reportSubtitleBounds(): void {
  const element = subtitleElement.value
  if (!element || phase.value === 'hidden') {
    void runtimeBridge.reportSubtitleBounds(0, 0, 0, 0, false).catch(() => undefined)
    return
  }
  const bounds = element.getBoundingClientRect()
  void runtimeBridge.reportSubtitleBounds(
    Math.round(bounds.left),
    Math.round(bounds.top),
    Math.round(bounds.width),
    Math.round(bounds.height),
    true,
  ).catch(() => undefined)
}

function scheduleDismiss(delay: number): void {
  clearDismissTimers()
  leaveTimer = window.setTimeout(() => {
    phase.value = 'leaving'
    leaveTimer = null
  }, delay)
  hideTimer = window.setTimeout(() => {
    phase.value = 'hidden'
    subtitleText.value = ''
    clearSubtitleScroll()
    reportSubtitleBounds()
    hideTimer = null
  }, delay + subtitleConfig.value.fade_out_ms)
}

async function showSubtitle(event: TranslationEvent): Promise<void> {
  if (!event.text || event.request_id < latestRequestID.value) {
    return
  }
  const requestID = event.request_id
  latestRequestID.value = requestID
  clearAnimation()
  subtitleText.value = event.text
  persistentSubtitle.value = Boolean(event.persistent)
  phase.value = 'entering'

  await nextTick()
  if (requestID !== latestRequestID.value) {
    return
  }
  if (subtitleElement.value && resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver.observe(subtitleElement.value)
  }
  reportSubtitleBounds()
  startSubtitleScroll()
  animationFrame = requestAnimationFrame(() => {
    phase.value = 'visible'
    animationFrame = null
  })
  if (!persistentSubtitle.value) {
    scheduleDismiss(subtitleConfig.value.fade_in_ms + subtitleConfig.value.display_ms)
  }
}

function updateHover(hovered: boolean): void {
  if (phase.value === 'hidden') {
    return
  }
  if (hovered) {
    clearDismissTimers()
    clearSubtitleScroll()
    phase.value = 'visible'
    return
  }
  if (!persistentSubtitle.value) {
    scheduleDismiss(subtitleConfig.value.display_ms)
    startSubtitleScroll()
  }
}

function updateConfig(config: SubtitleConfig): void {
  subtitleConfig.value = {...defaultConfig, ...config}
  void nextTick(() => {
    reportSubtitleBounds()
    startSubtitleScroll()
  })
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
  removeHoverListener = runtimeBridge.on<boolean>('subtitle:hover', updateHover)
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(reportSubtitleBounds)
  }
  void runtimeBridge.ready().catch(() => undefined)
})

onBeforeUnmount(() => {
  clearAnimation()
  resizeObserver?.disconnect()
  void runtimeBridge.reportSubtitleBounds(0, 0, 0, 0, false).catch(() => undefined)
  removeTranslationListener?.()
  removeConfigListener?.()
  removeHoverListener?.()
})
</script>

<template>
  <SettingsView v-if="isSettingsWindow" />
  <main v-else class="overlay-stage" aria-live="polite" aria-atomic="true">
    <section
      v-if="phase !== 'hidden'"
      ref="subtitleElement"
      class="subtitle"
      :class="`subtitle--${phase}`"
      :style="subtitleStyle"
      data-testid="subtitle"
    >
      <span ref="subtitleViewportElement" class="subtitle__viewport">
        <span ref="subtitleTextElement" class="subtitle__text" :style="{transform: `translateY(-${subtitleScrollOffset}px)`}">{{ subtitleText }}</span>
      </span>
    </section>
  </main>
</template>
