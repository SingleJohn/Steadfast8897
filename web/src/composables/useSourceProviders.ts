import { computed, ref, shallowRef } from 'vue'
import { getSystemConfig, updateSystemConfig } from '@/api/client'
import {
  batchDeleteSourceProviders,
  batchHealthCheckSourceProviders,
  batchSetSourceProvidersEnabled,
  diagnoseSourceProvider,
  federatedSourceSearch,
  getSourceProviderHomeProfile,
  healthCheckSourceProvider,
  listSourceProviderCategories,
  listSourceProviders,
  searchSourceProvider,
  setSourceProviderEnabled,
  type FederatedSearchResponse,
  type SourceProviderDeleteResult,
  type SourceProviderDiagnoseResult,
  type SourceProviderHomeProfile,
  type SourceProviderListOptions,
  type SourceProvider,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceProviders(showToast: ToastFn) {
  const providers = ref<SourceProvider[]>([])
  const activeProviderId = shallowRef<number | null>(null)
  const providerSearchKeyword = shallowRef('')
  const providerSearchResult = shallowRef<any>(null)
  const providerCategories = ref<Array<{ id: string; name: string }>>([])
  const providerDiagnosis = shallowRef<SourceProviderDiagnoseResult | null>(null)
  const providerHomeProfile = shallowRef<SourceProviderHomeProfile | null>(null)
  const providerAction = shallowRef('')
  const federatedKeyword = shallowRef('')
  const federatedLimit = shallowRef(50)
  const federatedLoading = shallowRef(false)
  const federatedResult = shallowRef<FederatedSearchResponse | null>(null)
  const embySourceSearchEnabled = shallowRef(true)
  const savingEmbySourceSearch = shallowRef(false)
  const selectedProviderIds = ref<number[]>([])
  const providerHealthFilters = ref<SourceProviderListOptions>({})
  const includeHiddenProviders = shallowRef(false)

  const nativeProviders = computed(() => providers.value.filter((p) => p.ProviderKind === 'cms_vod' && p.RuntimeKind === 'native_cms'))
  const runtimeRequiredProviders = computed(() => providers.value.filter((p) => p.RuntimeKind !== 'native_cms'))
  const selectedProvider = computed(() => providers.value.find((p) => p.ID === activeProviderId.value) || null)
  const selectedProviders = computed(() => providers.value.filter((p) => selectedProviderIds.value.includes(p.ID)))

  async function refreshProviders() {
    const nextProviders = await listSourceProviders({
      ...providerHealthFilters.value,
      include_hidden: includeHiddenProviders.value,
    })
    providers.value = nextProviders
    const available = new Set(nextProviders.map((provider) => provider.ID))
    selectedProviderIds.value = selectedProviderIds.value.filter((id) => available.has(id))
    if (!activeProviderId.value && nextProviders.length > 0) activeProviderId.value = nextProviders[0].ID
  }

  async function loadSourceSearchConfig() {
    try {
      const cfg: any = await getSystemConfig()
      embySourceSearchEnabled.value = String(cfg?.source_emby_search_enabled ?? 'true') !== 'false'
    } catch {
      embySourceSearchEnabled.value = true
    }
  }

  async function toggleProvider(id: number, enabled: boolean) {
    await setSourceProviderEnabled(id, enabled)
    await refreshProviders()
  }

  async function batchToggleProviders(enabled: boolean, ids = selectedProviderIds.value) {
    const targetIds = [...ids]
    if (targetIds.length === 0) {
      showToast('请先选择 Provider', 'info')
      return
    }
    providerAction.value = enabled ? 'batch-enable' : 'batch-disable'
    try {
      const result = await batchSetSourceProvidersEnabled(targetIds, enabled)
      showToast(`${enabled ? '启用' : '停用'}完成：${result.count} 个 Provider`, 'success')
      selectedProviderIds.value = selectedProviderIds.value.filter((id) => !targetIds.includes(id))
      await refreshProviders()
    } catch (e: any) {
      showToast(e?.message || '批量启停失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function batchHealthProviders(ids = selectedProviderIds.value) {
    const targetIds = [...ids]
    if (targetIds.length === 0) {
      showToast('请先选择 Provider', 'info')
      return
    }
    providerAction.value = 'batch-health'
    try {
      const result = await batchHealthCheckSourceProviders(targetIds)
      const failed = result.items.filter((item) => item.status !== 'ok').length
      showToast(`批量探活完成：${result.count - failed} 成功 / ${failed} 失败`, failed > 0 ? 'info' : 'success')
      await refreshProviders()
    } catch (e: any) {
      showToast(e?.message || '批量探活失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function batchDeleteProviders(ids = selectedProviderIds.value): Promise<SourceProviderDeleteResult | null> {
    const targetIds = [...ids]
    if (targetIds.length === 0) {
      showToast('请先选择 Provider', 'info')
      return null
    }
    providerAction.value = 'batch-delete'
    try {
      const result = await batchDeleteSourceProviders(targetIds)
      showToast(`删除完成：${result.count} 个 Provider`, 'success')
      selectedProviderIds.value = selectedProviderIds.value.filter((id) => !targetIds.includes(id))
      await refreshProviders()
      return result
    } catch (e: any) {
      showToast(e?.message || '批量删除失败', 'error')
      return null
    } finally {
      providerAction.value = ''
    }
  }

  async function updateProviderHealthFilters(filters: SourceProviderListOptions) {
    providerHealthFilters.value = {
      runtime_status: filters.runtime_status || undefined,
      home_status: filters.home_status || undefined,
      category_status: filters.category_status || undefined,
    }
    selectedProviderIds.value = []
    await refreshProviders()
  }

  async function updateIncludeHiddenProviders(value: boolean) {
    includeHiddenProviders.value = value
    selectedProviderIds.value = []
    await refreshProviders()
  }

  async function runProviderHealth(id: number) {
    providerAction.value = `health:${id}`
    try {
      await healthCheckSourceProvider(id)
      showToast('探活完成', 'success')
      await refreshProviders()
    } catch (e: any) {
      showToast(e?.message || '探活失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function runProviderDiagnose(id: number) {
    providerAction.value = `diagnose:${id}`
    try {
      providerDiagnosis.value = await diagnoseSourceProvider(id, {
        methods: ['home', 'homeVideo', 'category', 'search'],
        keyword: providerSearchKeyword.value.trim() || 'test',
      })
      activeProviderId.value = id
      showToast('兼容诊断完成；结果不会改变探活状态', 'success')
    } catch (e: any) {
      showToast(e?.message || '兼容诊断失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function loadProviderHomeProfile(id: number) {
    providerAction.value = `home-profile:${id}`
    try {
      providerHomeProfile.value = await getSourceProviderHomeProfile(id)
      activeProviderId.value = id
      showToast('首页画像已加载；不会写入在线缓存', 'success')
    } catch (e: any) {
      showToast(e?.message || '首页画像加载失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function runProviderSearch() {
    if (!activeProviderId.value) return
    providerAction.value = `search:${activeProviderId.value}`
    try {
      providerSearchResult.value = await searchSourceProvider(activeProviderId.value, providerSearchKeyword.value.trim(), 1)
      showToast('搜索测试完成，结果已写入在线缓存', 'success')
      await refreshProviders()
    } catch (e: any) {
      showToast(e?.message || '搜索失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function loadProviderCategories(id: number) {
    providerAction.value = `categories:${id}`
    try {
      providerCategories.value = await listSourceProviderCategories(id)
      activeProviderId.value = id
    } catch (e: any) {
      showToast(e?.message || '分类加载失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function runFederatedSearch() {
    const keyword = federatedKeyword.value.trim()
    if (!keyword) {
      showToast('请填写聚合搜索关键词', 'info')
      return
    }
    federatedLoading.value = true
    try {
      federatedResult.value = await federatedSourceSearch(keyword, federatedLimit.value)
      showToast('聚合搜索完成，命中已写入在线缓存', 'success')
      await refreshProviders()
    } catch (e: any) {
      showToast(e?.message || '聚合搜索失败', 'error')
    } finally {
      federatedLoading.value = false
    }
  }

  async function updateEmbySourceSearchEnabled(value: boolean) {
    savingEmbySourceSearch.value = true
    try {
      await updateSystemConfig({ source_emby_search_enabled: value ? 'true' : 'false' })
      embySourceSearchEnabled.value = value
      showToast(value ? 'Emby 在线搜索已开启' : 'Emby 在线搜索已关闭', 'success')
    } catch (e: any) {
      showToast(e?.message || '保存失败', 'error')
    } finally {
      savingEmbySourceSearch.value = false
    }
  }

  return {
    providers,
    activeProviderId,
    selectedProvider,
    selectedProviderIds,
    selectedProviders,
    providerHealthFilters,
    includeHiddenProviders,
    nativeProviders,
    runtimeRequiredProviders,
    providerSearchKeyword,
    providerSearchResult,
    providerCategories,
    providerDiagnosis,
    providerHomeProfile,
    providerAction,
    federatedKeyword,
    federatedLimit,
    federatedLoading,
    federatedResult,
    embySourceSearchEnabled,
    savingEmbySourceSearch,
    refreshProviders,
    loadSourceSearchConfig,
    toggleProvider,
    batchToggleProviders,
    batchHealthProviders,
    batchDeleteProviders,
    updateProviderHealthFilters,
    updateIncludeHiddenProviders,
    runProviderHealth,
    runProviderDiagnose,
    loadProviderHomeProfile,
    runProviderSearch,
    loadProviderCategories,
    runFederatedSearch,
    updateEmbySourceSearchEnabled,
  }
}
