import { shallowRef } from 'vue'
import { useSourceConfigs } from '@/composables/useSourceConfigs'
import { useSourceProviders } from '@/composables/useSourceProviders'
import { useSourceRuntimeAudit } from '@/composables/useSourceRuntimeAudit'
import { useSourceViews } from '@/composables/useSourceViews'

type ToastFn = (message: string, type?: any) => void

export function useSourceCenter(showToast: ToastFn) {
  const configs = useSourceConfigs(showToast)
  const providers = useSourceProviders(showToast)
  const runtimeAudit = useSourceRuntimeAudit(showToast)
  const views = useSourceViews(showToast)
  const loading = shallowRef(false)
  const configAction = shallowRef('')

  async function refreshAll() {
    loading.value = true
    try {
      await Promise.all([
        configs.refreshConfigs(),
        providers.refreshProviders(),
        views.refreshViews(),
        views.loadDimensionMeta(),
      ])
    } finally {
      loading.value = false
    }
  }

  async function submitImport() {
    await configs.submitImport()
    await Promise.all([
      providers.refreshProviders(),
      views.refreshViews(),
    ])
  }

  async function toggleConfig(id: number, enabled: boolean) {
    configAction.value = `toggle:${id}`
    try {
      await configs.toggleConfig(id, enabled)
      await providers.refreshProviders()
      showToast(enabled ? '配置已启用' : '配置已停用', 'success')
    } catch (e: any) {
      showToast(e?.message || '配置启停失败', 'error')
    } finally {
      configAction.value = ''
    }
  }

  async function refreshConfig(id: number) {
    const ok = await configs.refreshConfig(id)
    if (ok) {
      await Promise.all([
        providers.refreshProviders(),
        views.refreshViews(),
      ])
    }
  }

  async function toggleProvider(id: number, enabled: boolean) {
    await providers.toggleProvider(id, enabled)
    await views.refreshViews()
  }

  async function batchToggleProviders(enabled: boolean, ids?: number[]) {
    await providers.batchToggleProviders(enabled, ids)
    await views.refreshViews()
  }

  async function batchHealthProviders(ids?: number[]) {
    await providers.batchHealthProviders(ids)
    await views.refreshViews()
  }

  async function batchDeleteProviders(ids?: number[]) {
    await providers.batchDeleteProviders(ids)
    await Promise.all([
      runtimeAudit.refreshRuntimeData(),
      views.refreshViews(),
    ])
  }

  async function confirmDeleteConfig() {
    await configs.confirmDeleteConfig()
    await Promise.all([
      providers.refreshProviders(),
      runtimeAudit.refreshRuntimeData(),
      views.refreshViews(),
    ])
  }

  async function runProviderHealth(id: number) {
    await providers.runProviderHealth(id)
    await views.refreshViews()
  }

  async function runProviderSearch() {
    await providers.runProviderSearch()
    await views.refreshViews()
  }

  async function runFederatedSearch() {
    await providers.runFederatedSearch()
    await views.refreshViews()
  }

  return {
    configs: configs.configs,
    providers: providers.providers,
    parsers: runtimeAudit.parsers,
    runtimeInvocations: runtimeAudit.runtimeInvocations,
    runtimeArtifacts: runtimeAudit.runtimeArtifacts,
    views: views.views,
    loading,
    importing: configs.importing,
    importName: configs.importName,
    importUrl: configs.importUrl,
    importJson: configs.importJson,
    importKind: configs.importKind,
    importFormat: configs.importFormat,
    lastImport: configs.lastImport,
    configDeleteTarget: configs.deleteTarget,
    configDeleteImpact: configs.deleteImpact,
    configDeleteLoading: configs.deleteLoading,
    configRefreshingId: configs.refreshingConfigId,
    activeProviderId: providers.activeProviderId,
    selectedProvider: providers.selectedProvider,
    selectedProviderIds: providers.selectedProviderIds,
    selectedProviders: providers.selectedProviders,
    providerHealthFilters: providers.providerHealthFilters,
    includeHiddenProviders: providers.includeHiddenProviders,
    nativeProviders: providers.nativeProviders,
    runtimeRequiredProviders: providers.runtimeRequiredProviders,
    providerSearchKeyword: providers.providerSearchKeyword,
    providerSearchResult: providers.providerSearchResult,
    providerCategories: providers.providerCategories,
    providerDiagnosis: providers.providerDiagnosis,
    providerHomeProfile: providers.providerHomeProfile,
    providerAction: providers.providerAction,
    configAction,
    parserAction: runtimeAudit.parserAction,
    runtimeAction: runtimeAudit.runtimeAction,
    runtimeAuditFilters: runtimeAudit.runtimeAuditFilters,
    runtimeAuditLoading: runtimeAudit.runtimeAuditLoading,
    federatedKeyword: providers.federatedKeyword,
    federatedLimit: providers.federatedLimit,
    federatedLoading: providers.federatedLoading,
    federatedResult: providers.federatedResult,
    federatedDryRun: providers.federatedDryRun,
    embySourceSearchEnabled: providers.embySourceSearchEnabled,
    savingEmbySourceSearch: providers.savingEmbySourceSearch,
    embyLiveSearchEnabled: providers.embyLiveSearchEnabled,
    savingEmbyLiveSearch: providers.savingEmbyLiveSearch,
    viewDraft: views.viewDraft,
    discoverDimension: views.discoverDimension,
    discoverSearch: views.discoverSearch,
    discoverValues: views.discoverValues,
    discoverSelected: views.discoverSelected,
    discoverLoading: views.discoverLoading,
    coverTargetId: views.coverTargetId,
    coverStyle: views.coverStyle,
    coverStyleOptions: views.coverStyleOptions,
    coverStylesLoaded: views.coverStylesLoaded,
    showcaseIconOptions: views.showcaseIconOptions,
    showcaseIcon: views.showcaseIcon,
    showcaseShowPosterTitles: views.showcaseShowPosterTitles,
    showcaseShowCount: views.showcaseShowCount,
    generatingCover: views.generatingCover,
    viewPreview: views.viewPreview,
    previewLoading: views.previewLoading,
    viewMatchValueError: views.matchValueError,
    viewDimensionMeta: views.dimensionMeta,
    viewActiveDimensionMeta: views.activeDimensionMeta,
    fillViewMatchValue: views.fillMatchValue,
    refreshAll,
    refreshRuntimeData: runtimeAudit.refreshRuntimeData,
    updateRuntimeAuditFilters: runtimeAudit.updateRuntimeAuditFilters,
    loadSourceSearchConfig: providers.loadSourceSearchConfig,
    submitImport,
    toggleConfig,
    inspectDeleteConfig: configs.inspectDeleteConfig,
    cancelDeleteConfig: configs.cancelDeleteConfig,
    confirmDeleteConfig,
    refreshConfig,
    toggleProvider,
    batchToggleProviders,
    batchHealthProviders,
    batchDeleteProviders,
    updateProviderHealthFilters: providers.updateProviderHealthFilters,
    updateIncludeHiddenProviders: providers.updateIncludeHiddenProviders,
    toggleParser: runtimeAudit.toggleParser,
    trustRuntimeArtifact: runtimeAudit.trustRuntimeArtifact,
    runProviderHealth,
    fetchProviderCatalog: providers.fetchProviderCatalog,
    batchFetchProviderCatalog: providers.batchFetchProviderCatalog,
    runProviderDiagnose: providers.runProviderDiagnose,
    loadProviderHomeProfile: providers.loadProviderHomeProfile,
    runProviderSearch,
    loadProviderCategories: providers.loadProviderCategories,
    runFederatedSearch,
    updateEmbySourceSearchEnabled: providers.updateEmbySourceSearchEnabled,
    updateEmbyLiveSearchEnabled: providers.updateEmbyLiveSearchEnabled,
    editView: views.editView,
    saveView: views.saveView,
    previewView: views.previewView,
    removeView: views.removeView,
    renameView: views.renameView,
    runDiscover: views.runDiscover,
    applyDiscoverSelection: views.applyDiscoverSelection,
    moveView: views.moveView,
    openCover: views.openCover,
    closeCover: views.closeCover,
    confirmCover: views.confirmCover,
    restoreCover: views.restoreCover,
  }
}
