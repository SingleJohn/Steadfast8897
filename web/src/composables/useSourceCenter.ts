import { computed, reactive, ref, shallowRef } from 'vue'
import { getSystemConfig, listCoverStyles, updateSystemConfig, type CoverStyle } from '@/api/client'
import {
  createSourceView,
  deleteSourceView,
  deleteSourceViewCover,
  discoverSourceViewValues,
  federatedSourceSearch,
  generateSourceViewCover,
  healthCheckSourceProvider,
  importTVBoxConfig,
  listSourceConfigs,
  listSourceParsers,
  listSourceProviderCategories,
  listSourceProviders,
  listSourceRuntimeArtifacts,
  listSourceRuntimeInvocations,
  listSourceViews,
  renameSourceView,
  searchSourceProvider,
  setSourceConfigEnabled,
  setSourceParserEnabled,
  setSourceProviderEnabled,
  trustSourceRuntimeArtifact,
  updateSourceView,
  updateSourceViewDisplayOrder,
  type DimensionValue,
  type FederatedSearchResponse,
  type SourceParser,
  type SourceConfig,
  type SourceProvider,
  type SourceRuntimeArtifact,
  type SourceRuntimeInvocation,
  type SourceView,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceCenter(showToast: ToastFn) {
  const configs = ref<SourceConfig[]>([])
  const providers = ref<SourceProvider[]>([])
  const parsers = ref<SourceParser[]>([])
  const runtimeInvocations = ref<SourceRuntimeInvocation[]>([])
  const runtimeArtifacts = ref<SourceRuntimeArtifact[]>([])
  const views = ref<SourceView[]>([])
  const coverStyles = ref<CoverStyle[]>([])
  const loading = shallowRef(false)
  const importing = shallowRef(false)
  const importName = shallowRef('')
  const importUrl = shallowRef('')
  const importJson = shallowRef('')
  const lastImport = shallowRef<any>(null)
  const activeProviderId = shallowRef<number | null>(null)
  const providerSearchKeyword = shallowRef('')
  const providerSearchResult = shallowRef<any>(null)
  const providerCategories = ref<Array<{ id: string; name: string }>>([])
  const providerAction = shallowRef('')
  const parserAction = shallowRef('')
  const runtimeAction = shallowRef('')
  const runtimeAuditLoading = shallowRef(false)
  const federatedKeyword = shallowRef('')
  const federatedLimit = shallowRef(50)
  const federatedLoading = shallowRef(false)
  const federatedResult = shallowRef<FederatedSearchResponse | null>(null)
  const embySourceSearchEnabled = shallowRef(true)
  const savingEmbySourceSearch = shallowRef(false)

  const viewDraft = reactive({
    id: null as number | null,
    Name: '',
    DisplayName: '',
    Dimension: 'normalized_kind',
    MatchValue: '',
    MatchValues: [] as string[],
    CollectionType: 'mixed',
    Enabled: true,
    ExposeToEmby: false,
  })
  const discoverDimension = shallowRef('normalized_kind')
  const discoverSearch = shallowRef('')
  const discoverValues = ref<DimensionValue[]>([])
  const discoverSelected = ref<string[]>([])
  const discoverLoading = shallowRef(false)
  const coverTargetId = shallowRef<number | null>(null)
  const coverStyle = shallowRef('')
  const generatingCover = shallowRef(false)

  const nativeProviders = computed(() => providers.value.filter((p) => p.ProviderKind === 'cms_vod' && p.RuntimeKind === 'native_cms'))
  const runtimeRequiredProviders = computed(() => providers.value.filter((p) => p.RuntimeKind !== 'native_cms'))
  const selectedProvider = computed(() => providers.value.find((p) => p.ID === activeProviderId.value) || null)
  const coverStyleOptions = computed(() => coverStyles.value.map((s) => ({ label: s.label, value: s.name })))

  async function refreshAll() {
    loading.value = true
    try {
      const [nextConfigs, nextProviders, nextViews] = await Promise.all([
        listSourceConfigs(),
        listSourceProviders(),
        listSourceViews(),
      ])
      configs.value = nextConfigs
      providers.value = nextProviders
      views.value = nextViews
      if (!activeProviderId.value && nextProviders.length > 0) activeProviderId.value = nextProviders[0].ID
    } finally {
      loading.value = false
    }
  }

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

  async function loadSourceSearchConfig() {
    try {
      const cfg: any = await getSystemConfig()
      embySourceSearchEnabled.value = String(cfg?.source_emby_search_enabled ?? 'true') !== 'false'
    } catch {
      embySourceSearchEnabled.value = true
    }
  }

  async function ensureCoverStyles() {
    if (coverStyles.value.length > 0) return
    coverStyles.value = await listCoverStyles()
    coverStyle.value = coverStyles.value.find((s) => s.name === 'showcase')?.name || coverStyles.value[0]?.name || ''
  }

  async function submitImport() {
    if (!importUrl.value.trim() && !importJson.value.trim()) {
      showToast('请填写配置 URL 或粘贴 JSON', 'info')
      return
    }
    importing.value = true
    try {
      lastImport.value = await importTVBoxConfig({
        name: importName.value.trim() || undefined,
        source_url: importUrl.value.trim() || undefined,
        raw_json: importJson.value.trim() || undefined,
      })
      showToast(`导入完成：可用 ${lastImport.value.accepted}，暂不可用 ${lastImport.value.skipped}`, 'success')
      await refreshAll()
    } catch (e: any) {
      showToast(e?.message || '导入失败', 'error')
    } finally {
      importing.value = false
    }
  }

  async function toggleConfig(id: number, enabled: boolean) {
    await setSourceConfigEnabled(id, enabled)
    await refreshAll()
  }

  async function toggleProvider(id: number, enabled: boolean) {
    await setSourceProviderEnabled(id, enabled)
    await refreshAll()
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

  async function runProviderHealth(id: number) {
    providerAction.value = `health:${id}`
    try {
      await healthCheckSourceProvider(id)
      showToast('探活完成', 'success')
      await refreshAll()
    } catch (e: any) {
      showToast(e?.message || '探活失败', 'error')
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
      await refreshAll()
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
      await refreshAll()
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

  function editView(view?: SourceView) {
    viewDraft.id = view?.Id || null
    viewDraft.Name = view?.Name || ''
    viewDraft.DisplayName = view?.CustomName || ''
    viewDraft.Dimension = view?.Dimension || 'normalized_kind'
    viewDraft.MatchValue = view?.MatchValue || ''
    viewDraft.MatchValues = [...(view?.MatchValues || [])]
    viewDraft.CollectionType = view?.CollectionType || 'mixed'
    viewDraft.Enabled = view?.Enabled ?? true
    viewDraft.ExposeToEmby = view?.ExposeToEmby ?? false
  }

  async function saveView() {
    if (!viewDraft.MatchValue.trim()) {
      showToast('请填写匹配值', 'info')
      return
    }
    const payload = {
      Name: viewDraft.Name || viewDraft.MatchValue,
      DisplayName: viewDraft.DisplayName || undefined,
      Dimension: viewDraft.Dimension,
      MatchValue: viewDraft.MatchValue,
      MatchValues: viewDraft.MatchValues.length > 0 ? viewDraft.MatchValues : [viewDraft.MatchValue],
      CollectionType: viewDraft.CollectionType,
      Enabled: viewDraft.Enabled,
      ExposeToEmby: viewDraft.ExposeToEmby,
    }
    if (viewDraft.id) await updateSourceView(viewDraft.id, payload)
    else await createSourceView(payload)
    showToast('在线库已保存', 'success')
    editView()
    await refreshAll()
  }

  async function removeView(id: number) {
    await deleteSourceView(id)
    showToast('在线库已删除', 'success')
    await refreshAll()
  }

  async function renameView(id: number, name: string) {
    await renameSourceView(id, name)
    await refreshAll()
  }

  async function runDiscover() {
    discoverLoading.value = true
    discoverSelected.value = []
    try {
      discoverValues.value = await discoverSourceViewValues(discoverDimension.value, discoverSearch.value.trim(), 1)
    } catch (e: any) {
      showToast(e?.message || '维度发现失败', 'error')
    } finally {
      discoverLoading.value = false
    }
  }

  function applyDiscoverSelection() {
    if (discoverSelected.value.length === 0) return
    viewDraft.Dimension = discoverDimension.value
    viewDraft.MatchValue = discoverSelected.value[0]
    viewDraft.MatchValues = [...discoverSelected.value]
    if (!viewDraft.Name) viewDraft.Name = discoverSelected.value[0]
  }

  async function moveView(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= views.value.length) return
    const ids = views.value.map((v) => v.Id)
    ;[ids[index], ids[target]] = [ids[target], ids[index]]
    await updateSourceViewDisplayOrder(ids)
    await refreshAll()
  }

  async function openCover(id: number) {
    coverTargetId.value = id
    await ensureCoverStyles()
  }

  async function confirmCover() {
    if (!coverTargetId.value || !coverStyle.value) return
    generatingCover.value = true
    try {
      await generateSourceViewCover(coverTargetId.value, coverStyle.value)
      coverTargetId.value = null
      showToast('封面已生成', 'success')
      await refreshAll()
    } catch (e: any) {
      showToast(e?.message || '生成失败', 'error')
    } finally {
      generatingCover.value = false
    }
  }

  async function restoreCover(id: number) {
    await deleteSourceViewCover(id)
    await refreshAll()
  }

  return {
    configs,
    providers,
    parsers,
    runtimeInvocations,
    runtimeArtifacts,
    views,
    loading,
    importing,
    importName,
    importUrl,
    importJson,
    lastImport,
    activeProviderId,
    selectedProvider,
    nativeProviders,
    runtimeRequiredProviders,
    providerSearchKeyword,
    providerSearchResult,
    providerCategories,
    providerAction,
    parserAction,
    runtimeAction,
    runtimeAuditLoading,
    federatedKeyword,
    federatedLimit,
    federatedLoading,
    federatedResult,
    embySourceSearchEnabled,
    savingEmbySourceSearch,
    viewDraft,
    discoverDimension,
    discoverSearch,
    discoverValues,
    discoverSelected,
    discoverLoading,
    coverTargetId,
    coverStyle,
    coverStyleOptions,
    generatingCover,
    refreshAll,
    refreshRuntimeData,
    loadSourceSearchConfig,
    submitImport,
    toggleConfig,
    toggleProvider,
    toggleParser,
    trustRuntimeArtifact,
    runProviderHealth,
    runProviderSearch,
    loadProviderCategories,
    runFederatedSearch,
    updateEmbySourceSearchEnabled,
    editView,
    saveView,
    removeView,
    renameView,
    runDiscover,
    applyDiscoverSelection,
    moveView,
    openCover,
    confirmCover,
    restoreCover,
  }
}
