import { useLocalStorage, usePreferredDark } from '@vueuse/core'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

export type ColorMode = 'light' | 'dark' | 'auto'

export const useUiStore = defineStore('ui', () => {
  const preferredDark = usePreferredDark()

  const mode = useLocalStorage<ColorMode>('ui.mode', 'auto')
  const primaryColor = useLocalStorage('ui.primaryColor', '#18a058')
  const radius = useLocalStorage('ui.radius', 10)
  const glassBlur = useLocalStorage('ui.glassBlur', 18)
  const siderCollapsed = useLocalStorage('ui.siderCollapsed', false)

  // 前台 MediaLayout 挂载期间强制暗色(cinematic 风格)。非持久化。
  const forceDark = ref(false)

  const isDark = computed(() => {
    if (forceDark.value) return true
    if (mode.value === 'dark') return true
    if (mode.value === 'light') return false
    return preferredDark.value
  })

  const naiveThemeOverrides = computed(() => {
    const isDarkValue = isDark.value
    return {
      common: {
        primaryColor: primaryColor.value,
        borderRadius: `${radius.value}px`,
        bodyColor: isDarkValue ? '#020617' : '#f8fafc',
        cardColor: isDarkValue ? '#0f172a' : '#ffffff',
      },
    }
  })

  return {
    mode,
    primaryColor,
    radius,
    glassBlur,
    siderCollapsed,
    forceDark,
    isDark,
    naiveThemeOverrides,
  }
})
