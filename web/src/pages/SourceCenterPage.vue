<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSpin } from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import SourceCenterOverview from '@/components/source-center/SourceCenterOverview.vue'
import SourceConfigListPanel from '@/components/source-center/SourceConfigListPanel.vue'
import SourceFederatedSearchPanel from '@/components/source-center/SourceFederatedSearchPanel.vue'
import SourceImportPanel from '@/components/source-center/SourceImportPanel.vue'
import SourceParserPanel from '@/components/source-center/SourceParserPanel.vue'
import SourceProviderPanel from '@/components/source-center/SourceProviderPanel.vue'
import SourceRuntimeAuditPanel from '@/components/source-center/SourceRuntimeAuditPanel.vue'
import SourceViewsPanel from '@/components/source-center/SourceViewsPanel.vue'
import { AppIcons } from '@/icons/appIcons'
import { useSourceCenter } from '@/composables/useSourceCenter'
import { useToast } from '@/composables/useToast'

type SourceCenterTab = 'overview' | 'configs' | 'providers' | 'views' | 'parsers' | 'audit'

const route = useRoute()
const router = useRouter()
const { showToast } = useToast()
const source = useSourceCenter(showToast)

const tabs: Array<{ key: SourceCenterTab; label: string; helper: string }> = [
  { key: 'overview', label: '总览', helper: '状态摘要' },
  { key: 'configs', label: '配置', helper: '导入包' },
  { key: 'providers', label: 'Provider', helper: '站点管理' },
  { key: 'views', label: '在线库', helper: 'Emby 视图' },
  { key: 'parsers', label: '解析器', helper: '全局策略' },
  { key: 'audit', label: '审计', helper: '调用与 artifact' },
]

const activeTab = computed<SourceCenterTab>(() => {
  const tab = route.query.tab
  const value = Array.isArray(tab) ? tab[0] : tab
  return tabs.some((item) => item.key === value) ? value as SourceCenterTab : 'overview'
})

const tabTitle = computed(() => tabs.find((tab) => tab.key === activeTab.value)?.label || '总览')

const {
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
  importKind,
  importFormat,
  lastImport,
  configDeleteTarget,
  configDeleteImpact,
  configDeleteLoading,
  activeProviderId,
  selectedProviderIds,
  providerSearchKeyword,
  providerSearchResult,
  providerCategories,
  providerAction,
  configAction,
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
  viewPreview,
  previewLoading,
  viewMatchValueError,
} = source

async function switchTab(tab: SourceCenterTab) {
  await router.replace({
    query: {
      ...route.query,
      tab: tab === 'overview' ? undefined : tab,
    },
  })
}

async function updateWorkbenchQuery(next: Record<string, string | number | null | undefined>) {
  const query = { ...route.query }
  for (const [key, value] of Object.entries(next)) {
    if (value === null || value === undefined || value === '') delete query[key]
    else query[key] = String(value)
  }
  await router.replace({ query })
}

async function refreshSourceCenter() {
  await source.refreshAll()
  await source.refreshRuntimeData()
  await source.loadSourceSearchConfig()
}

onMounted(() => {
  const providerId = Number(route.query.provider)
  if (Number.isFinite(providerId) && providerId > 0) activeProviderId.value = providerId

  const providerKeyword = Array.isArray(route.query.provider_keyword) ? route.query.provider_keyword[0] : route.query.provider_keyword
  if (providerKeyword) providerSearchKeyword.value = providerKeyword

  const federatedQuery = Array.isArray(route.query.search) ? route.query.search[0] : route.query.search
  if (federatedQuery) federatedKeyword.value = federatedQuery

  void refreshSourceCenter()
})

watch(activeProviderId, (value) => {
  void updateWorkbenchQuery({ provider: value || undefined })
})

watch(providerSearchKeyword, (value) => {
  void updateWorkbenchQuery({ provider_keyword: value || undefined })
})

watch(federatedKeyword, (value) => {
  void updateWorkbenchQuery({ search: value || undefined })
})
</script>

<template>
  <PageShell
    title="来源中心"
    description="导入 TVBox 配置，管理在线 Provider，并创建可暴露给 Emby 的在线虚拟库。"
    :icon="AppIcons.gateway"
  >
    <div class="source-workbench">
      <nav class="source-nav" aria-label="来源中心子导航">
        <NButton
          v-for="tab in tabs"
          :key="tab.key"
          class="source-nav-button"
          :type="activeTab === tab.key ? 'primary' : 'default'"
          :secondary="activeTab !== tab.key"
          @click="switchTab(tab.key)"
        >
          <span class="tab-label">{{ tab.label }}</span>
          <span class="tab-helper">{{ tab.helper }}</span>
        </NButton>
      </nav>

      <NSpin :show="loading && activeTab !== 'audit'">
        <main class="source-main" :aria-label="`来源中心${tabTitle}`">
          <SourceCenterOverview
            v-if="activeTab === 'overview'"
            :configs="configs"
            :providers="providers"
            :parsers="parsers"
            :views="views"
            :invocations="runtimeInvocations"
            :loading="loading || runtimeAuditLoading"
            @refresh="refreshSourceCenter"
            @navigate="switchTab"
          />

          <div v-else-if="activeTab === 'configs'" class="tab-stack">
            <SourceConfigListPanel
              :configs="configs"
              :action="configAction"
              :delete-target="configDeleteTarget"
              :delete-impact="configDeleteImpact"
              :delete-loading="configDeleteLoading"
              @toggle="source.toggleConfig"
              @inspect-delete="source.inspectDeleteConfig"
              @cancel-delete="source.cancelDeleteConfig"
              @confirm-delete="source.confirmDeleteConfig"
              @refresh="source.refreshAll"
            />

            <SourceImportPanel
              v-model:name="importName"
              v-model:url="importUrl"
              v-model:json="importJson"
              v-model:kind="importKind"
              v-model:format="importFormat"
              :importing="importing"
              :last-import="lastImport"
              @submit="source.submitImport"
            />
          </div>

          <div v-else-if="activeTab === 'providers'" class="tab-stack">
            <SourceProviderPanel
              :providers="providers"
              :active-provider-id="activeProviderId"
              :keyword="providerSearchKeyword"
              :search-result="providerSearchResult"
              :categories="providerCategories"
              :action="providerAction"
              :selected-ids="selectedProviderIds"
              @update:active-provider-id="activeProviderId = $event"
              @update:keyword="providerSearchKeyword = $event"
              @update:selected-ids="selectedProviderIds = $event"
              @toggle="source.toggleProvider"
              @batch-enable="source.batchToggleProviders(true)"
              @batch-disable="source.batchToggleProviders(false)"
              @batch-health="source.batchHealthProviders"
              @health="source.runProviderHealth"
              @categories="source.loadProviderCategories"
              @search="source.runProviderSearch"
            />

            <SourceFederatedSearchPanel
              :keyword="federatedKeyword"
              :limit="federatedLimit"
              :loading="federatedLoading"
              :result="federatedResult"
              :emby-enabled="embySourceSearchEnabled"
              :saving-emby-enabled="savingEmbySourceSearch"
              @update:keyword="federatedKeyword = $event"
              @update:limit="federatedLimit = $event"
              @update:emby-enabled="source.updateEmbySourceSearchEnabled"
              @search="source.runFederatedSearch"
            />
          </div>

          <div v-else-if="activeTab === 'views'" class="tab-stack">
            <SourceViewsPanel
              :views="views"
              :providers="providers"
              :draft="viewDraft"
              :preview="viewPreview"
              :preview-loading="previewLoading"
              :match-value-error="viewMatchValueError"
              :discover-dimension="discoverDimension"
              :discover-search="discoverSearch"
              :discover-values="discoverValues"
              :discover-selected="discoverSelected"
              :discover-loading="discoverLoading"
              :cover-target-id="coverTargetId"
              :cover-style="coverStyle"
              :cover-style-options="coverStyleOptions"
              :generating-cover="generatingCover"
              @update:draft-name="viewDraft.Name = $event"
              @update:draft-display-name="viewDraft.DisplayName = $event"
              @update:draft-dimension="viewDraft.Dimension = $event"
              @update:draft-match-value="viewDraft.MatchValue = $event"
              @update:draft-provider-ids="viewDraft.ProviderIds = $event"
              @update:draft-collection-type="viewDraft.CollectionType = $event"
              @update:draft-sort-order="viewDraft.SortOrder = $event"
              @update:draft-enabled="viewDraft.Enabled = $event"
              @update:draft-expose="viewDraft.ExposeToEmby = $event"
              @update:discover-dimension="discoverDimension = $event"
              @update:discover-search="discoverSearch = $event"
              @update:discover-selected="discoverSelected = $event"
              @update:cover-style="coverStyle = $event"
              @edit="source.editView"
              @save="source.saveView"
              @preview="source.previewView"
              @remove="source.removeView"
              @discover="source.runDiscover"
              @apply-discover="source.applyDiscoverSelection"
              @move="source.moveView"
              @open-cover="source.openCover"
              @confirm-cover="source.confirmCover"
              @restore-cover="source.restoreCover"
            />
          </div>

          <div v-else-if="activeTab === 'parsers'" class="tab-stack">
            <SourceParserPanel
              :parsers="parsers"
              :action="parserAction"
              @toggle="source.toggleParser"
              @refresh="source.refreshRuntimeData"
            />
          </div>

          <div v-else class="tab-stack">
            <SourceRuntimeAuditPanel
              :invocations="runtimeInvocations"
              :artifacts="runtimeArtifacts"
              :loading="runtimeAuditLoading"
              :action="runtimeAction"
              @refresh="source.refreshRuntimeData"
              @trust="source.trustRuntimeArtifact"
            />
          </div>
        </main>
      </NSpin>
    </div>
  </PageShell>
</template>

<style scoped>
.source-workbench {
  display: grid;
  gap: 16px;
}

.source-nav {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 8px;
}

.source-nav-button {
  min-width: 0;
  height: 58px;
}

.source-nav-button :deep(.n-button__content) {
  display: grid;
  justify-items: start;
  gap: 2px;
  width: 100%;
}

.tab-label {
  font-size: 13px;
  font-weight: 700;
}

.tab-helper {
  min-width: 0;
  overflow: hidden;
  color: var(--app-text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-main,
.tab-stack {
  display: grid;
  gap: 16px;
}

@media (max-width: 1080px) {
  .source-nav {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .source-nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
