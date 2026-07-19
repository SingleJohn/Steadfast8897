import { computed, shallowRef, watch, type Ref } from 'vue'

import { upsertLatestPlatformLibrary } from '@/api/client'

type ToastFn = (message: string, type?: any) => void

interface PlatformData {
  GlobalEnabled: boolean
  Platforms: any[]
}

export function useLatestVirtualLibrary(
  platformsData: Ref<PlatformData>,
  loadPlatforms: () => Promise<void>,
  showToast: ToastFn,
) {
  const latestLibrary = computed(() => platformsData.value.Platforms.find((item: any) => item.IsLatest) || null)
  const latestName = shallowRef('最新更新')
  const latestLimit = shallowRef(200)
  const savingLatestLibrary = shallowRef(false)

  watch(
    latestLibrary,
    (library) => {
      latestName.value = library?.DisplayName || '最新更新'
      latestLimit.value = library?.ItemLimit || 200
    },
    { immediate: true },
  )

  async function saveLatestLibrary() {
    const name = latestName.value.trim() || '最新更新'
    const limit = Math.min(2000, Math.max(1, Math.trunc(latestLimit.value || 200)))
    const existed = Boolean(latestLibrary.value)
    savingLatestLibrary.value = true
    try {
      await upsertLatestPlatformLibrary({
        Name: name,
        Limit: limit,
        Enabled: latestLibrary.value?.Enabled ?? true,
      })
      latestName.value = name
      latestLimit.value = limit
      await loadPlatforms()
      showToast(existed ? '最新媒体库设置已保存' : '最新媒体库已创建', 'success')
    } catch (error: any) {
      showToast(error?.message || '最新媒体库保存失败', 'error')
    } finally {
      savingLatestLibrary.value = false
    }
  }

  return {
    latestLibrary,
    latestName,
    latestLimit,
    savingLatestLibrary,
    saveLatestLibrary,
  }
}
