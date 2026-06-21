<script setup lang="ts">
import { onMounted } from 'vue'
import { NSpin } from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import SourceFederatedSearchPanel from '@/components/source-center/SourceFederatedSearchPanel.vue'
import SourceImportPanel from '@/components/source-center/SourceImportPanel.vue'
import SourceProviderPanel from '@/components/source-center/SourceProviderPanel.vue'
import SourceViewsPanel from '@/components/source-center/SourceViewsPanel.vue'
import { AppIcons } from '@/icons/appIcons'
import { useSourceCenter } from '@/composables/useSourceCenter'
import { useToast } from '@/composables/useToast'

const { showToast } = useToast()
const source = useSourceCenter(showToast)
const {
  configs,
  providers,
  views,
  loading,
  importing,
  importName,
  importUrl,
  importJson,
  lastImport,
  activeProviderId,
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
} = source

void configs

onMounted(() => {
  void source.refreshAll()
  void source.loadSourceSearchConfig()
})
</script>

<template>
  <PageShell
    title="来源中心"
    description="导入 TVBox 配置，管理在线 Provider，并创建可暴露给 Emby 的在线虚拟库。"
    :icon="AppIcons.gateway"
  >
    <NSpin :show="loading">
      <div class="source-center">
        <SourceImportPanel
          v-model:name="importName"
          v-model:url="importUrl"
          v-model:json="importJson"
          :importing="importing"
          :last-import="lastImport"
          @submit="source.submitImport"
        />

        <SourceProviderPanel
          :providers="providers"
          :active-provider-id="activeProviderId"
          :keyword="providerSearchKeyword"
          :search-result="providerSearchResult"
          :categories="providerCategories"
          :action="providerAction"
          @update:active-provider-id="activeProviderId = $event"
          @update:keyword="providerSearchKeyword = $event"
          @toggle="source.toggleProvider"
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

        <SourceViewsPanel
          :views="views"
          :draft="viewDraft"
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
          @update:draft-collection-type="viewDraft.CollectionType = $event"
          @update:draft-enabled="viewDraft.Enabled = $event"
          @update:draft-expose="viewDraft.ExposeToEmby = $event"
          @update:discover-dimension="discoverDimension = $event"
          @update:discover-search="discoverSearch = $event"
          @update:discover-selected="discoverSelected = $event"
          @update:cover-style="coverStyle = $event"
          @edit="source.editView"
          @save="source.saveView"
          @remove="source.removeView"
          @discover="source.runDiscover"
          @apply-discover="source.applyDiscoverSelection"
          @move="source.moveView"
          @open-cover="source.openCover"
          @confirm-cover="source.confirmCover"
          @restore-cover="source.restoreCover"
        />
      </div>
    </NSpin>
  </PageShell>
</template>

<style scoped>
.source-center {
  display: grid;
  gap: 16px;
}
</style>
