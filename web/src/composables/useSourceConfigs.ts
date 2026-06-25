import { ref, shallowRef } from 'vue'
import {
  importCMSListConfig,
  importTVBoxConfig,
  listSourceConfigs,
  setSourceConfigEnabled,
  type SourceConfig,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceConfigs(showToast: ToastFn) {
  const configs = ref<SourceConfig[]>([])
  const loading = shallowRef(false)
  const importing = shallowRef(false)
  const importName = shallowRef('')
  const importUrl = shallowRef('')
  const importJson = shallowRef('')
  const importKind = shallowRef<'tvbox' | 'cms_list'>('tvbox')
  const importFormat = shallowRef<'auto' | 'libretv_settings' | 'csv' | 'txt' | 'json'>('auto')
  const lastImport = shallowRef<any>(null)

  async function refreshConfigs() {
    configs.value = await listSourceConfigs()
  }

  async function submitImport() {
    if (!importUrl.value.trim() && !importJson.value.trim()) {
      showToast('请填写配置 URL 或粘贴内容', 'info')
      return
    }
    importing.value = true
    try {
      const basePayload = {
        name: importName.value.trim() || undefined,
        source_url: importUrl.value.trim() || undefined,
      }
      if (importKind.value === 'cms_list') {
        lastImport.value = await importCMSListConfig({
          ...basePayload,
          raw_text: importJson.value.trim() || undefined,
          format: importFormat.value,
          default_enabled: false,
        })
        showToast(`导入完成：Provider ${lastImport.value.accepted} 个，默认禁用`, 'success')
      } else {
        lastImport.value = await importTVBoxConfig({
          ...basePayload,
          raw_json: importJson.value.trim() || undefined,
        })
        showToast(`导入完成：可用 ${lastImport.value.accepted}，暂不可用 ${lastImport.value.skipped}`, 'success')
      }
      await refreshConfigs()
    } catch (e: any) {
      showToast(e?.message || '导入失败', 'error')
    } finally {
      importing.value = false
    }
  }

  async function toggleConfig(id: number, enabled: boolean) {
    await setSourceConfigEnabled(id, enabled)
    await refreshConfigs()
  }

  return {
    configs,
    loading,
    importing,
    importName,
    importUrl,
    importJson,
    importKind,
    importFormat,
    lastImport,
    refreshConfigs,
    submitImport,
    toggleConfig,
  }
}
