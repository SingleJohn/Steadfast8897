import { ref, shallowRef } from 'vue'
import {
  deleteSourceConfig,
  getSourceConfigImpact,
  importCMSListConfig,
  importTVBoxConfig,
  listSourceConfigs,
  refreshSourceConfig,
  setSourceConfigEnabled,
  type SourceConfig,
  type SourceConfigImpact,
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
  const deleteTarget = shallowRef<SourceConfig | null>(null)
  const deleteImpact = shallowRef<SourceConfigImpact | null>(null)
  const deleteLoading = shallowRef(false)
  const refreshingConfigId = shallowRef<number | null>(null)

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

  // 用已存来源 URL 重新拉取并重导，原地更新 Provider 并保留启停状态。
  // 返回是否成功，便于调用方决定是否联动刷新 Provider/虚拟库。
  async function refreshConfig(id: number): Promise<boolean> {
    refreshingConfigId.value = id
    try {
      const result: any = await refreshSourceConfig(id)
      const accepted = result?.accepted ?? result?.providers?.length ?? 0
      showToast(`配置已更新：可用 ${accepted}${typeof result?.skipped === 'number' ? `，暂不可用 ${result.skipped}` : ''}`, 'success')
      await refreshConfigs()
      return true
    } catch (e: any) {
      showToast(e?.message || '更新配置失败', 'error')
      return false
    } finally {
      refreshingConfigId.value = null
    }
  }

  async function inspectDeleteConfig(config: SourceConfig) {
    deleteTarget.value = config
    deleteImpact.value = null
    deleteLoading.value = true
    try {
      deleteImpact.value = await getSourceConfigImpact(config.ID)
    } catch (e: any) {
      deleteTarget.value = null
      showToast(e?.message || '影响预览加载失败', 'error')
    } finally {
      deleteLoading.value = false
    }
  }

  function cancelDeleteConfig() {
    deleteTarget.value = null
    deleteImpact.value = null
    deleteLoading.value = false
  }

  async function confirmDeleteConfig() {
    if (!deleteTarget.value) return
    deleteLoading.value = true
    try {
      await deleteSourceConfig(deleteTarget.value.ID)
      showToast('配置已删除，关联 Provider/Parser 已清理', 'success')
      cancelDeleteConfig()
      await refreshConfigs()
    } catch (e: any) {
      showToast(e?.message || '删除配置失败', 'error')
    } finally {
      deleteLoading.value = false
    }
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
    deleteTarget,
    deleteImpact,
    deleteLoading,
    refreshingConfigId,
    refreshConfigs,
    submitImport,
    toggleConfig,
    refreshConfig,
    inspectDeleteConfig,
    cancelDeleteConfig,
    confirmDeleteConfig,
  }
}
