import { computed, ref, shallowRef } from 'vue'
import { getSystemConfig, updateSystemConfig } from '@/api/client'
import {
  federatedSourceSearch,
  healthCheckSourceProvider,
  listSourceProviderCategories,
  listSourceProviders,
  searchSourceProvider,
  setSourceProviderEnabled,
  type FederatedSearchResponse,
  type SourceProvider,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceProviders(showToast: ToastFn) {
  const providers = ref<SourceProvider[]>([])
  const activeProviderId = shallowRef<number | null>(null)
  const providerSearchKeyword = shallowRef('')
  const providerSearchResult = shallowRef<any>(null)
  const providerCategories = ref<Array<{ id: string; name: string }>>([])
  const providerAction = shallowRef('')
  const federatedKeyword = shallowRef('')
  const federatedLimit = shallowRef(50)
  const federatedLoading = shallowRef(false)
  const federatedResult = shallowRef<FederatedSearchResponse | null>(null)
  const embySourceSearchEnabled = shallowRef(true)
  const savingEmbySourceSearch = shallowRef(false)

  const nativeProviders = computed(() => providers.value.filter((p) => p.ProviderKind === 'cms_vod' && p.RuntimeKind === 'native_cms'))
  const runtimeRequiredProviders = computed(() => providers.value.filter((p) => p.RuntimeKind !== 'native_cms'))
  const selectedProvider = computed(() => providers.value.find((p) => p.ID === activeProviderId.value) || null)

  async function refreshProviders() {
    const nextProviders = await listSourceProviders()
    providers.value = nextProviders
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
    nativeProviders,
    runtimeRequiredProviders,
    providerSearchKeyword,
    providerSearchResult,
    providerCategories,
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
    runProviderHealth,
    runProviderSearch,
    loadProviderCategories,
    runFederatedSearch,
    updateEmbySourceSearchEnabled,
  }
}
