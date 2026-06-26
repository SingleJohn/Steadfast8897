import { ref, shallowRef } from 'vue'
import {
  listSourceParsers,
  listSourceRuntimeArtifacts,
  listSourceRuntimeInvocations,
  setSourceParserEnabled,
  trustSourceRuntimeArtifact,
  type SourceParser,
  type SourceRuntimeArtifact,
  type SourceRuntimeInvocation,
  type SourceRuntimeInvocationListOptions,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceRuntimeAudit(showToast: ToastFn) {
  const parsers = ref<SourceParser[]>([])
  const runtimeInvocations = ref<SourceRuntimeInvocation[]>([])
  const runtimeArtifacts = ref<SourceRuntimeArtifact[]>([])
  const parserAction = shallowRef('')
  const runtimeAction = shallowRef('')
  const runtimeAuditLoading = shallowRef(false)
  const runtimeAuditFilters = ref<SourceRuntimeInvocationListOptions>({ limit: 100 })

  async function refreshRuntimeData() {
    runtimeAuditLoading.value = true
    try {
      const [nextParsers, nextInvocations, nextArtifacts] = await Promise.all([
        listSourceParsers(),
        listSourceRuntimeInvocations(runtimeAuditFilters.value),
        listSourceRuntimeArtifacts(),
      ])
      parsers.value = nextParsers
      runtimeInvocations.value = nextInvocations
      runtimeArtifacts.value = nextArtifacts
    } catch (e: any) {
      showToast(e?.message || '运行时数据加载失败', 'error')
    } finally {
      runtimeAuditLoading.value = false
    }
  }

  async function updateRuntimeAuditFilters(filters: SourceRuntimeInvocationListOptions) {
    runtimeAuditFilters.value = {
      limit: filters.limit || 100,
      provider_id: filters.provider_id || undefined,
      method: filters.method || undefined,
      status: filters.status || undefined,
      error_type: filters.error_type || undefined,
      runtime_kind: filters.runtime_kind || undefined,
      start_time: filters.start_time || undefined,
      end_time: filters.end_time || undefined,
    }
    await refreshRuntimeData()
  }

  async function toggleParser(id: number, enabled: boolean) {
    parserAction.value = `toggle:${id}`
    try {
      await setSourceParserEnabled(id, enabled)
      showToast(enabled ? '解析器已启用' : '解析器已停用', 'success')
      await refreshRuntimeData()
    } catch (e: any) {
      showToast(e?.message || '解析器启停失败', 'error')
    } finally {
      parserAction.value = ''
    }
  }

  async function trustRuntimeArtifact(id: number) {
    runtimeAction.value = `trust-artifact:${id}`
    try {
      await trustSourceRuntimeArtifact(id)
      showToast('artifact 已确认信任', 'success')
      await refreshRuntimeData()
    } catch (e: any) {
      showToast(e?.message || 'artifact 信任确认失败', 'error')
    } finally {
      runtimeAction.value = ''
    }
  }

  return {
    parsers,
    runtimeInvocations,
    runtimeArtifacts,
    runtimeAuditFilters,
    parserAction,
    runtimeAction,
    runtimeAuditLoading,
    refreshRuntimeData,
    updateRuntimeAuditFilters,
    toggleParser,
    trustRuntimeArtifact,
  }
}
