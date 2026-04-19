<script setup lang="ts">
import { NConfigProvider, NGlobalStyle, NMessageProvider, darkTheme } from 'naive-ui'
import { watchEffect } from 'vue'

import { useUiStore } from '@/stores/ui'

const ui = useUiStore()

function toRgbTuple(color: string): string | null {
  const raw = String(color || '').trim().replace(/^#/, '')
  if (/^[0-9a-fA-F]{6}$/.test(raw)) {
    const r = Number.parseInt(raw.slice(0, 2), 16)
    const g = Number.parseInt(raw.slice(2, 4), 16)
    const b = Number.parseInt(raw.slice(4, 6), 16)
    return `${r}, ${g}, ${b}`
  }
  if (/^[0-9a-fA-F]{3}$/.test(raw)) {
    const r = Number.parseInt(raw[0] + raw[0], 16)
    const g = Number.parseInt(raw[1] + raw[1], 16)
    const b = Number.parseInt(raw[2] + raw[2], 16)
    return `${r}, ${g}, ${b}`
  }
  return null
}

watchEffect(() => {
  const root = document.documentElement
  root.classList.toggle('app-dark', ui.isDark)

  const rgb = toRgbTuple(ui.primaryColor)
  if (rgb) {
    root.style.setProperty('--app-primary', ui.primaryColor)
    root.style.setProperty('--app-primary-rgb', rgb)
    root.style.setProperty('--app-brand-bg', `linear-gradient(135deg, ${ui.primaryColor}, #0ea5e9)`)
  }
  root.style.setProperty('--app-radius', `${ui.radius}px`)
  root.style.setProperty('--app-glass-blur', `${ui.glassBlur}px`)
})
</script>

<template>
  <n-config-provider
    :theme="ui.isDark ? darkTheme : null"
    :theme-overrides="ui.naiveThemeOverrides"
  >
    <n-message-provider>
      <n-global-style />
      <router-view />
    </n-message-provider>
  </n-config-provider>
</template>
