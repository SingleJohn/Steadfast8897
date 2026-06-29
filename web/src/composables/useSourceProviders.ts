import { computed, ref, shallowRef } from 'vue'
import { getSystemConfig, updateSystemConfig } from '@/api/client'
import {
  batchDeleteSourceProviders,
  batchFetchProviderCatalog as batchFetchProviderCatalogApi,
  batchHealthCheckSourceProviders,
  batchSetSourceProvidersEnabled,
  diagnoseSourceProvider,
  fetchProviderCatalog as fetchProviderCatalogApi,
  getSourceProviderHomeProfile,
  healthCheckSourceProvider,
  listSourceProviderCategories,
  listSourceProviders,
  openFederatedSourceSearchStream,
  searchSourceProvider,
  setSourceProviderEnabled,
  type FederatedSearchItem,
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
  // 测试模式：只测连通/命中，不写入 source_items（dry-run），管理端默认走快速搜索。
  const federatedDryRun = shallowRef(true)
  const embySourceSearchEnabled = shallowRef(true)
  const savingEmbySourceSearch = shallowRef(false)
  // Emby 同步直搜：客户端搜索时实时跑一次跨源聚合搜索预热缓存（默认关闭）
  const embyLiveSearchEnabled = shallowRef(false)
  const savingEmbyLiveSearch = shallowRef(false)
  const sourceRefreshSchedulerEnabled = shallowRef(false)
  const savingSourceRefreshScheduler = shallowRef(false)
  const autoDisableSearchEnabled = shallowRef(false)
  const savingAutoDisableSearch = shallowRef(false)
  const autoDisablePlayEnabled = shallowRef(false)
  const savingAutoDisablePlay = shallowRef(false)
  const selectedProviderIds = ref<number[]>([])
  const providerHealthFilters = ref<SourceProviderListOptions>({})
  const includeHiddenProviders = shallowRef(false)
  let federatedSearchStream: EventSource | null = null

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
      embyLiveSearchEnabled.value = String(cfg?.source_emby_live_search_enabled ?? 'false') === 'true'
      sourceRefreshSchedulerEnabled.value = String(cfg?.source_refresh_scheduler_enabled ?? 'false') === 'true'
      autoDisableSearchEnabled.value = String(cfg?.source_auto_disable_search_failed_enabled ?? 'false') === 'true'
      autoDisablePlayEnabled.value = String(cfg?.source_auto_disable_play_failed_enabled ?? 'false') === 'true'
    } catch {
      embySourceSearchEnabled.value = true
      embyLiveSearchEnabled.value = false
      sourceRefreshSchedulerEnabled.value = false
      autoDisableSearchEnabled.value = false
      autoDisablePlayEnabled.value = false
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
      // 只清除真正被设置成功的项，失败的保留在选中里便于重试
      const succeeded = new Set((result.items || []).map((item) => item.ID))
      const cleared = succeeded.size > 0 ? succeeded : new Set(targetIds)
      const failed = targetIds.filter((id) => !cleared.has(id)).length
      const tip = `${enabled ? '启用' : '停用'}完成：${result.count} 个 Provider${failed > 0 ? `，${failed} 个未生效已保留选中` : ''}`
      showToast(tip, failed > 0 ? 'info' : 'success')
      selectedProviderIds.value = selectedProviderIds.value.filter((id) => !cleared.has(id))
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

  // 手动触发：把站点的分类内容抓取入队，后台填充虚拟库（不阻塞，稍后内容陆续入库）。
  async function fetchProviderCatalog(id: number) {
    providerAction.value = `catalog:${id}`
    try {
      await fetchProviderCatalogApi(id)
      showToast('已加入抓取队列，后台填充中（稍后刷新查看库内容）', 'success')
    } catch (e: any) {
      showToast(e?.message || '加入抓取队列失败', 'error')
    } finally {
      providerAction.value = ''
    }
  }

  async function batchFetchProviderCatalog(ids = selectedProviderIds.value) {
    const targetIds = [...ids]
    if (targetIds.length === 0) {
      showToast('请先选择 Provider', 'info')
      return
    }
    providerAction.value = 'batch-catalog'
    try {
      const result = await batchFetchProviderCatalogApi(targetIds)
      showToast(`已加入抓取队列：${result.enqueued} 个站点，后台填充中`, 'success')
    } catch (e: any) {
      showToast(e?.message || '批量抓取失败', 'error')
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
      showToast(`分类已加载：${providerCategories.value.length} 个`, 'success')
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
    const dryRun = federatedDryRun.value
    federatedResult.value = emptyFederatedSearchResult(keyword, dryRun)
    federatedSearchStream?.close()
    let done = false
    federatedSearchStream = openFederatedSourceSearchStream(
      keyword,
      federatedLimit.value,
      dryRun,
      async (event) => {
        if (event.type === 'start') {
          federatedResult.value = {
            ...emptyFederatedSearchResult(keyword, dryRun),
            provider: event.provider || { total: 0, success: 0, failed: 0, concurrency: 0 },
          }
          return
        }
        if (event.type === 'provider_result') {
          if (event.item) mergeFederatedStreamItem(event.item)
          if (event.provider) updateFederatedStreamProvider(event.provider)
          return
        }
        if (event.type === 'provider_error') {
          if (event.error) appendFederatedStreamError(event.error)
          if (event.provider) updateFederatedStreamProvider(event.provider)
          if (event.auto_disable?.disabled) {
            showToast(`已自动禁用 Provider：${event.auto_disable.provider_name || event.provider_name || event.provider_id}`, 'warning')
          }
          return
        }
        if (event.type === 'done') {
          done = true
          if (event.response) federatedResult.value = event.response
          federatedLoading.value = false
          federatedSearchStream?.close()
          federatedSearchStream = null
          if (dryRun) {
            showToast('搜索测试完成（测试模式，未写入在线缓存）', 'success')
          } else {
            showToast('聚合搜索完成，命中已写入在线缓存', 'success')
            await refreshProviders()
          }
        }
      },
      (message) => {
        if (done) return
        federatedLoading.value = false
        federatedSearchStream?.close()
        federatedSearchStream = null
        showToast(message || '聚合搜索失败', 'error')
      },
    )
  }

  function emptyFederatedSearchResult(keyword: string, dryRun: boolean): FederatedSearchResponse {
    return {
      keyword,
      total: 0,
      items: [],
      provider: { total: 0, success: 0, failed: 0, concurrency: 0 },
      latency_ms: 0,
      truncated: false,
      cache_write: !dryRun,
    }
  }

  function mergeFederatedStreamItem(item: FederatedSearchItem) {
    const current = federatedResult.value || emptyFederatedSearchResult(federatedKeyword.value.trim(), federatedDryRun.value)
    const existing = current.items.findIndex((row) => row.public_uuid === item.public_uuid)
    const nextItems = existing >= 0
      ? current.items.map((row, index) => (index === existing ? item : row))
      : [...current.items, item]
    federatedResult.value = {
      ...current,
      items: nextItems,
      total: nextItems.length,
    }
  }

  function appendFederatedStreamError(error: NonNullable<FederatedSearchResponse['errors']>[number]) {
    const current = federatedResult.value || emptyFederatedSearchResult(federatedKeyword.value.trim(), federatedDryRun.value)
    federatedResult.value = {
      ...current,
      errors: [...(current.errors || []), error],
    }
  }

  function updateFederatedStreamProvider(provider: FederatedSearchResponse['provider']) {
    const current = federatedResult.value || emptyFederatedSearchResult(federatedKeyword.value.trim(), federatedDryRun.value)
    federatedResult.value = {
      ...current,
      provider,
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

  async function updateEmbyLiveSearchEnabled(value: boolean) {
    savingEmbyLiveSearch.value = true
    try {
      await updateSystemConfig({ source_emby_live_search_enabled: value ? 'true' : 'false' })
      embyLiveSearchEnabled.value = value
      showToast(value ? 'Emby 同步直搜已开启' : 'Emby 同步直搜已关闭', 'success')
    } catch (e: any) {
      showToast(e?.message || '保存失败', 'error')
    } finally {
      savingEmbyLiveSearch.value = false
    }
  }

  async function updateSourceRefreshSchedulerEnabled(value: boolean) {
    savingSourceRefreshScheduler.value = true
    try {
      await updateSystemConfig({ source_refresh_scheduler_enabled: value ? 'true' : 'false' })
      sourceRefreshSchedulerEnabled.value = value
      showToast(value ? '在线源自动刷新已开启' : '在线源自动刷新已关闭', 'success')
    } catch (e: any) {
      showToast(e?.message || '保存失败', 'error')
    } finally {
      savingSourceRefreshScheduler.value = false
    }
  }

  async function updateAutoDisableSearchEnabled(value: boolean) {
    savingAutoDisableSearch.value = true
    try {
      await updateSystemConfig({ source_auto_disable_search_failed_enabled: value ? 'true' : 'false' })
      autoDisableSearchEnabled.value = value
      showToast(value ? '搜索失败自动禁用已开启' : '搜索失败自动禁用已关闭', 'success')
    } catch (e: any) {
      showToast(e?.message || '保存失败', 'error')
    } finally {
      savingAutoDisableSearch.value = false
    }
  }

  async function updateAutoDisablePlayEnabled(value: boolean) {
    savingAutoDisablePlay.value = true
    try {
      await updateSystemConfig({ source_auto_disable_play_failed_enabled: value ? 'true' : 'false' })
      autoDisablePlayEnabled.value = value
      showToast(value ? '播放失败自动禁用已开启' : '播放失败自动禁用已关闭', 'success')
    } catch (e: any) {
      showToast(e?.message || '保存失败', 'error')
    } finally {
      savingAutoDisablePlay.value = false
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
    federatedDryRun,
    embySourceSearchEnabled,
    savingEmbySourceSearch,
    embyLiveSearchEnabled,
    savingEmbyLiveSearch,
    sourceRefreshSchedulerEnabled,
    savingSourceRefreshScheduler,
    autoDisableSearchEnabled,
    savingAutoDisableSearch,
    autoDisablePlayEnabled,
    savingAutoDisablePlay,
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
    updateEmbyLiveSearchEnabled,
    updateSourceRefreshSchedulerEnabled,
    updateAutoDisableSearchEnabled,
    updateAutoDisablePlayEnabled,
    fetchProviderCatalog,
    batchFetchProviderCatalog,
  }
}
