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
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceRuntimeAudit(showToast: ToastFn) {
  const parsers = ref<SourceParser[]>([])
  const runtimeInvocations = ref<SourceRuntimeInvocation[]>([])
  const runtimeArtifacts = ref<SourceRuntimeArtifact[]>([])
  const parserAction = shallowRef('')
  const runtimeAction = shallowRef('')
  const runtimeAuditLoading = shallowRef(false)

  async function refreshRuntimeData() {
    runtimeAuditLoading.value = true
    try {
      const [nextParsers, nextInvocations, nextArtifacts] = await Promise.all([
        listSourceParsers(),
        listSourceRuntimeInvocations(100),
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
    parserAction,
    runtimeAction,
    runtimeAuditLoading,
    refreshRuntimeData,
    toggleParser,
    trustRuntimeArtifact,
  }
}
