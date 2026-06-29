<script setup lang="ts">
import { ref, computed, defineAsyncComponent, onMounted, onUnmounted, watch } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NTabs, NTabPane,
} from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import LibraryDisplayOrderPanel from '@/components/libraries/LibraryDisplayOrderPanel.vue'
import LibraryGridPanel from '@/components/libraries/LibraryGridPanel.vue'
import LibraryScanPanel from '@/components/libraries/LibraryScanPanel.vue'
import PlatformLibrariesPanel from '@/components/libraries/PlatformLibrariesPanel.vue'
import { AppIcons } from '@/icons/appIcons'
import {
  getLibraries, refreshLibrary, forceLibraryRescanOptions,
  getSystemConfig, updateSystemConfig,
  listCoverStyles, generateAllLibraryCovers, type CoverStyle,
} from '@/api/client'
import { useDisplayOrder } from '@/composables/libraries/useDisplayOrder'
import { useLibraryCardOrder } from '@/composables/libraries/useLibraryCardOrder'
import { useLibraryCreate } from '@/composables/libraries/useLibraryCreate'
import { usePlatformLibraries } from '@/composables/libraries/usePlatformLibraries'
import { useLibraryScanState } from '@/composables/useLibraryScanState'
import { useVisibleInterval } from '@/composables/useVisibleInterval'

const { showToast } = useToast()
const LibraryEditModal = defineAsyncComponent(() => import('@/components/LibraryEditModal.vue'))
const LibraryCoverBatchModal = defineAsyncComponent(() => import('@/components/libraries/LibraryCoverBatchModal.vue'))
const LibraryCreateModals = defineAsyncComponent(() => import('@/components/libraries/LibraryCreateModals.vue'))
const PlatformLibraryModals = defineAsyncComponent(() => import('@/components/libraries/PlatformLibraryModals.vue'))

const scanThreadsOptions = [1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20].map((n) => ({ label: String(n), value: String(n) }))
const libTypeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
  { label: '混合', value: 'mixed' },
]

const libraries = ref<any[]>([])

const { snapshots, scanProgress } = useLibraryScanState()
const scanThreads = ref('3')
const fileWatcherEnabled = ref(true)
const scanning = ref(false)
const savingConfig = ref(false)
const activeView = ref<'libraries' | 'scan' | 'platforms' | 'order'>('libraries')

const {
  draggingLibraryId,
  dragOverLibraryId,
  savingLibraryOrder,
  handleLibraryDragStart,
  handleLibraryDragOver,
  handleLibraryDrop,
  handleLibraryDragEnd,
  onLibraryDragHandleKeydown,
} = useLibraryCardOrder(libraries, showToast)

const {
  orderList,
  savingOrder,
  draggingOrderKey,
  dragOverOrderKey,
  handleOrderDragStart,
  handleOrderDragOver,
  handleOrderDrop,
  handleOrderDragEnd,
  onOrderDragHandleKeydown,
} = useDisplayOrder(activeView, showToast)

const coverStyles = ref<CoverStyle[]>([])
const coverStylesLoaded = ref(false)
const showGenerateAllCovers = ref(false)
const generatingAllCovers = ref(false)
const batchCoverStyle = ref('')
const batchShowcaseIcon = ref('auto')
const batchShowcaseShowPosterTitles = ref(true)
const batchShowcaseShowCount = ref(true)
const batchCoverResult = ref<any>(null)
const showcaseIconOptions = [
  { label: '自动', value: 'auto' },
  { label: '电影', value: 'movie' },
  { label: '电视', value: 'tv' },
  { label: '音乐', value: 'music' },
  { label: '动漫', value: 'anime' },
  { label: '纪录片', value: 'documentary' },
  { label: '少儿', value: 'kids' },
  { label: '媒体', value: 'media' },
]
const coverStyleOptions = computed(() => coverStyles.value.map((s) => ({ label: s.label, value: s.name })))
const batchIsShowcase = computed(() => batchCoverStyle.value === 'showcase')
const canGenerateAllCovers = computed(() => libraries.value.length > 0 && coverStyles.value.length > 0 && !!batchCoverStyle.value && !generatingAllCovers.value)
const batchCoverIssues = computed(() => batchCoverResult.value?.Items?.filter((it: any) => it.Status !== 'success') || [])

async function ensureCoverStylesLoaded() {
  if (coverStylesLoaded.value) return
  try {
    coverStyles.value = await listCoverStyles()
    if (!batchCoverStyle.value && coverStyles.value.length > 0) {
      batchCoverStyle.value = coverStyles.value.find((s) => s.name === 'showcase')?.name || coverStyles.value[0].name
    }
  } catch {
    coverStylesLoaded.value = false
    showToast('加载封面风格失败', 'error')
    return
  } finally {
    if (coverStyles.value.length > 0) coverStylesLoaded.value = true
  }
}

async function openGenerateAllCovers() {
  batchCoverResult.value = null
  showGenerateAllCovers.value = true
  await ensureCoverStylesLoaded()
}

function batchCoverOptionsForStyle(style: string) {
  if (style !== 'showcase') return undefined
  const options: Record<string, any> = {
    ShowPosterTitles: batchShowcaseShowPosterTitles.value,
    ShowCount: batchShowcaseShowCount.value,
  }
  if (batchShowcaseIcon.value !== 'auto') {
    options.Icon = batchShowcaseIcon.value
  }
  return options
}

async function handleGenerateAllCovers() {
  if (!canGenerateAllCovers.value) return
  generatingAllCovers.value = true
  batchCoverResult.value = null
  try {
    const res = await generateAllLibraryCovers(batchCoverStyle.value, batchCoverOptionsForStyle(batchCoverStyle.value))
    batchCoverResult.value = res
    await onLibraryUpdated()
    const parts = [`成功 ${res.Success}`]
    if (res.Skipped) parts.push(`跳过 ${res.Skipped}`)
    if (res.Failed) parts.push(`失败 ${res.Failed}`)
    showToast(`封面批量生成完成：${parts.join('，')}`, res.Failed ? 'info' : 'success')
  } catch (e: any) {
    const msg = String(e?.message || '')
    if (msg.includes('424')) showToast('字体资源缺失,请参见 internal/services/coverart/assets/fonts/ 下的 README', 'error')
    else showToast('批量生成封面失败', 'error')
  } finally {
    generatingAllCovers.value = false
  }
}

const {
  platformsData,
  newPlatformName,
  platformScanning,
  platformTask,
  platformPosition,
  showLibraryItemCount,
  showPlatformCover,
  platformCoverTargetId,
  platformCoverStyle,
  platformShowcaseIcon,
  platformShowcaseShowPosterTitles,
  platformShowcaseShowCount,
  generatingPlatformCover,
  draggingPlatformId,
  dragOverPlatformId,
  savingPlatformOrder,
  dimensionOptions,
  discoverDimension,
  discoverSearch,
  discoverMinCount,
  discoverLoading,
  discoverResults,
  discoverSelected,
  showRename,
  renameValue,
  showAlias,
  aliasTarget,
  aliasValues,
  aliasSearch,
  aliasResults,
  aliasSelected,
  aliasLoading,
  filenameScanning,
  rescraping,
  rescrapeStatus,
  loadPlatforms,
  loadTaskSummary,
  toggleGlobalPlatform,
  togglePlatform,
  handleAddPlatform,
  runDiscover,
  addSelectedDimension,
  openPlatformCover,
  confirmPlatformCover,
  handleRestoreCover,
  openRename,
  confirmRename,
  openAlias,
  runAliasDiscover,
  addAliasSelected,
  removeAlias,
  handleDeletePlatform,
  handlePlatformDragStart,
  handlePlatformDragOver,
  handlePlatformDrop,
  handlePlatformDragEnd,
  onPlatformDragHandleKeydown,
  handleScanStudios,
  handleScanFilename,
  handleRescrape,
  resumeRescrapePolling,
  clearRescrapeTimer,
} = usePlatformLibraries(showToast, ensureCoverStylesLoaded, coverStyles)

const solidModalMenuProps = { class: 'solid-modal-menu' }
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

const {
  showAddLib,
  newLibName,
  newLibType,
  newLibPaths,
  newLibPathInput,
  showDirBrowser,
  dirBrowserPath,
  dirBrowserDirs,
  dirBrowserLoading,
  loadDirBrowser,
  openDirBrowser,
  dirParentPath,
  addPathToList,
  removePathFromList,
  handleAddPathManual,
  handleAddLibrary,
} = useLibraryCreate(libraries, showToast)
const showCreateModals = computed(() => showAddLib.value || showDirBrowser.value)
const showPlatformModals = computed(() => showPlatformCover.value || showRename.value || showAlias.value)

const editLibraryId = ref<string | null>(null)

function openEditModal(libId: string) {
  editLibraryId.value = libId
}

function closeEditModal() {
  editLibraryId.value = null
}

async function onLibraryUpdated() {
  libraries.value = await getLibraries()
}

async function onLibraryDeleted() {
  editLibraryId.value = null
  libraries.value = await getLibraries()
}

// cleanup snapshot 的子任务每个库一个 child,Message 字段带 "library=<id>"。
// 把 running → succeeded/failed 的转变转成 toast 通知管理员。
const cleanupStatusMap = new Map<string, string>()
watch(
  () => snapshots.cleanup?.children ?? [],
  (children) => {
    const seen = new Set<string>()
    for (const c of children) {
      const libId = (c.message ?? '').replace(/^library=/, '')
      if (!libId) continue
      seen.add(libId)
      const prev = cleanupStatusMap.get(libId)
      cleanupStatusMap.set(libId, c.status)
      if (prev && prev !== c.status) {
        const name = c.current || c.phase || libId
        if (c.status === 'succeeded') {
          showToast(`「${name}」已清理完成`, 'success')
        } else if (c.status === 'failed') {
          showToast(`「${name}」清理失败:${c.error || '未知错误'}`, 'error')
        }
      }
    }
    // 丢掉已不在 children 里的条目,避免内存泄漏。
    for (const id of Array.from(cleanupStatusMap.keys())) {
      if (!seen.has(id)) cleanupStatusMap.delete(id)
    }
  },
  { deep: true },
)

function scanProgForLib(libId: string) {
  return scanProgress.value.find((s: any) => s.LibraryId === libId)
}

async function handleScan() {
  scanning.value = true
  try {
    await refreshLibrary()
    showToast('媒体库扫描已开始，这可能需要一些时间。', 'success')
  } catch {
    showToast('启动扫描失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function handleForceScan() {
  scanning.value = true
  try {
    await refreshLibrary(forceLibraryRescanOptions)
    showToast('强制重扫已开始，扫描完成后会刷新本地元数据和图片。', 'success')
  } catch {
    showToast('启动强制重扫失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function saveLibrarySettings() {
  savingConfig.value = true
  try {
    await updateSystemConfig({
      scan_threads: scanThreads.value,
      file_watcher_enabled: String(fileWatcherEnabled.value),
      platform_libraries_position: platformPosition.value,
      library_show_item_count: String(showLibraryItemCount.value),
    })
    showToast('设置已保存', 'success')
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    savingConfig.value = false
  }
}

let taskSummaryRefreshing = false

async function refreshTaskSummary() {
  if (taskSummaryRefreshing) return
  taskSummaryRefreshing = true
  try {
    await loadTaskSummary()
  } catch {
    // 下次可见轮询会自动重试。
  } finally {
    taskSummaryRefreshing = false
  }
}

useVisibleInterval(refreshTaskSummary, 3000)

onMounted(() => {
  getLibraries().then((l) => (libraries.value = l)).catch(() => {})
  getSystemConfig().then((cfg: any) => {
    scanThreads.value = cfg.scan_threads || '3'
    fileWatcherEnabled.value = cfg.file_watcher_enabled !== 'false'
    platformPosition.value = cfg.platform_libraries_position === 'before' ? 'before' : 'after'
    showLibraryItemCount.value = cfg.library_show_item_count !== 'false'
  }).catch(() => {})
  loadPlatforms()
  refreshTaskSummary().then(resumeRescrapePolling)
})

onUnmounted(() => {
  clearRescrapeTimer()
})
</script>

<template>
  <page-shell title="媒体库" :icon="AppIcons.library" description="管理媒体库文件夹与扫描设置">
    <n-tabs
      :value="activeView"
      type="segment"
      size="large"
      class="libraries-tabs"
      @update:value="(value) => activeView = value as 'libraries' | 'scan' | 'platforms' | 'order'"
    >
      <n-tab-pane name="libraries" tab="媒体库">
        <LibraryGridPanel
          :libraries="libraries"
          :show-item-count="showLibraryItemCount"
          :saving-library-order="savingLibraryOrder"
          :dragging-library-id="draggingLibraryId"
          :drag-over-library-id="dragOverLibraryId"
          :scan-prog-for-lib="scanProgForLib"
          @add-library="showAddLib = true"
          @generate-covers="openGenerateAllCovers"
          @edit="openEditModal"
          @drag-start="handleLibraryDragStart"
          @drag-over="handleLibraryDragOver"
          @drop="handleLibraryDrop"
          @drag-end="handleLibraryDragEnd"
          @handle-keydown="onLibraryDragHandleKeydown"
        />
      </n-tab-pane>

      <n-tab-pane name="scan" tab="扫库中心">
        <LibraryScanPanel
          v-model:scan-threads="scanThreads"
          v-model:file-watcher-enabled="fileWatcherEnabled"
          :libraries="libraries"
          :scan-progress="scanProgress"
          :scan-threads-options="scanThreadsOptions"
          :scanning="scanning"
          :saving-config="savingConfig"
          @scan="handleScan"
          @force-scan="handleForceScan"
          @save-settings="saveLibrarySettings"
        />
      </n-tab-pane>

      <n-tab-pane name="platforms" tab="平台库">
        <PlatformLibrariesPanel
          :platforms-data="platformsData"
          :platform-task="platformTask"
          :platform-scanning="platformScanning"
          :filename-scanning="filenameScanning"
          :rescraping="rescraping"
          :rescrape-status="rescrapeStatus"
          :platform-position="platformPosition"
          :show-library-item-count="showLibraryItemCount"
          :saving-config="savingConfig"
          :dimension-options="dimensionOptions"
          :discover-dimension="discoverDimension"
          :discover-search="discoverSearch"
          :discover-min-count="discoverMinCount"
          :discover-loading="discoverLoading"
          :discover-results="discoverResults"
          :discover-selected="discoverSelected"
          :new-platform-name="newPlatformName"
          :saving-platform-order="savingPlatformOrder"
          :dragging-platform-id="draggingPlatformId"
          :drag-over-platform-id="dragOverPlatformId"
          @toggle-global-platform="toggleGlobalPlatform"
          @update-platform-position="(value) => platformPosition = value"
          @update-show-library-item-count="(value) => showLibraryItemCount = value"
          @save-settings="saveLibrarySettings"
          @scan-studios="handleScanStudios"
          @scan-filename="handleScanFilename"
          @rescrape="handleRescrape"
          @update-discover-dimension="(value) => discoverDimension = value"
          @update-discover-search="(value) => discoverSearch = value"
          @update-discover-min-count="(value) => discoverMinCount = value"
          @update-discover-selected="(value) => discoverSelected = value"
          @run-discover="runDiscover"
          @add-selected-dimension="addSelectedDimension"
          @open-platform-cover="openPlatformCover"
          @open-alias="openAlias"
          @open-rename="openRename"
          @restore-cover="handleRestoreCover"
          @drag-start="handlePlatformDragStart"
          @drag-over="handlePlatformDragOver"
          @drop="handlePlatformDrop"
          @drag-end="handlePlatformDragEnd"
          @handle-keydown="onPlatformDragHandleKeydown"
          @toggle-platform="togglePlatform"
          @delete-platform="handleDeletePlatform"
          @update-new-platform-name="(value) => newPlatformName = value"
          @add-platform="handleAddPlatform"
        />
      </n-tab-pane>

      <n-tab-pane name="order" tab="整体排序">
        <LibraryDisplayOrderPanel
          :order-list="orderList"
          :saving-order="savingOrder"
          :dragging-order-key="draggingOrderKey"
          :drag-over-order-key="dragOverOrderKey"
          @drag-start="handleOrderDragStart"
          @drag-over="handleOrderDragOver"
          @drop="handleOrderDrop"
          @drag-end="handleOrderDragEnd"
          @handle-keydown="onOrderDragHandleKeydown"
        />
      </n-tab-pane>
    </n-tabs>

    <LibraryCoverBatchModal
      v-if="showGenerateAllCovers"
      :show="showGenerateAllCovers"
      :batch-cover-style="batchCoverStyle"
      :cover-style-options="coverStyleOptions"
      :cover-styles-loaded="coverStylesLoaded"
      :batch-is-showcase="batchIsShowcase"
      :showcase-icon-options="showcaseIconOptions"
      :batch-showcase-icon="batchShowcaseIcon"
      :batch-showcase-show-poster-titles="batchShowcaseShowPosterTitles"
      :batch-showcase-show-count="batchShowcaseShowCount"
      :batch-cover-result="batchCoverResult"
      :batch-cover-issues="batchCoverIssues"
      :generating-all-covers="generatingAllCovers"
      :can-generate-all-covers="canGenerateAllCovers"
      :solid-modal-menu-props="solidModalMenuProps"
      :force-solid-modal-style="forceSolidModalStyle"
      @update-show="(value) => showGenerateAllCovers = value"
      @update-batch-cover-style="(value) => batchCoverStyle = value"
      @update-batch-showcase-icon="(value) => batchShowcaseIcon = value"
      @update-batch-showcase-show-poster-titles="(value) => batchShowcaseShowPosterTitles = value"
      @update-batch-showcase-show-count="(value) => batchShowcaseShowCount = value"
      @generate="handleGenerateAllCovers"
    />

    <PlatformLibraryModals
      v-if="showPlatformModals"
      :show-platform-cover="showPlatformCover"
      :platform-cover-target-id="platformCoverTargetId"
      :platform-cover-style="platformCoverStyle"
      :cover-style-options="coverStyleOptions"
      :cover-styles-loaded="coverStylesLoaded"
      :showcase-icon-options="showcaseIconOptions"
      :platform-showcase-icon="platformShowcaseIcon"
      :platform-showcase-show-poster-titles="platformShowcaseShowPosterTitles"
      :platform-showcase-show-count="platformShowcaseShowCount"
      :generating-platform-cover="generatingPlatformCover"
      :show-rename="showRename"
      :rename-value="renameValue"
      :show-alias="showAlias"
      :alias-target="aliasTarget"
      :alias-values="aliasValues"
      :alias-search="aliasSearch"
      :alias-results="aliasResults"
      :alias-selected="aliasSelected"
      :alias-loading="aliasLoading"
      :solid-modal-menu-props="solidModalMenuProps"
      :force-solid-modal-style="forceSolidModalStyle"
      @update-show-platform-cover="(value) => showPlatformCover = value"
      @update-platform-cover-style="(value) => platformCoverStyle = value"
      @update-platform-showcase-icon="(value) => platformShowcaseIcon = value"
      @update-platform-showcase-show-poster-titles="(value) => platformShowcaseShowPosterTitles = value"
      @update-platform-showcase-show-count="(value) => platformShowcaseShowCount = value"
      @confirm-platform-cover="confirmPlatformCover"
      @update-show-rename="(value) => showRename = value"
      @update-rename-value="(value) => renameValue = value"
      @confirm-rename="confirmRename"
      @update-show-alias="(value) => showAlias = value"
      @remove-alias="removeAlias"
      @update-alias-search="(value) => aliasSearch = value"
      @run-alias-discover="runAliasDiscover"
      @update-alias-selected="(value) => aliasSelected = value"
      @add-alias-selected="addAliasSelected"
    />

    <LibraryCreateModals
      v-if="showCreateModals"
      :show-add-lib="showAddLib"
      :new-lib-name="newLibName"
      :new-lib-type="newLibType"
      :lib-type-options="libTypeOptions"
      :new-lib-paths="newLibPaths"
      :new-lib-path-input="newLibPathInput"
      :show-dir-browser="showDirBrowser"
      :dir-browser-path="dirBrowserPath"
      :dir-browser-dirs="dirBrowserDirs"
      :dir-browser-loading="dirBrowserLoading"
      :solid-modal-menu-props="solidModalMenuProps"
      :force-solid-modal-style="forceSolidModalStyle"
      @update-show-add-lib="(value) => showAddLib = value"
      @update-new-lib-name="(value) => newLibName = value"
      @update-new-lib-type="(value) => newLibType = value"
      @update-new-lib-path-input="(value) => newLibPathInput = value"
      @remove-path="removePathFromList"
      @add-path-manual="handleAddPathManual"
      @submit="handleAddLibrary"
      @open-dir-browser="openDirBrowser"
      @update-show-dir-browser="(value) => showDirBrowser = value"
      @dir-parent-path="dirParentPath"
      @load-dir-browser="loadDirBrowser"
      @select-dir="addPathToList(dirBrowserPath); showDirBrowser = false"
    />

    <!-- Library Edit Modal -->
    <LibraryEditModal
      v-if="editLibraryId"
      :library-id="editLibraryId"
      @close="closeEditModal"
      @updated="onLibraryUpdated"
      @deleted="onLibraryDeleted"
    />
  </page-shell>
</template>

<style scoped>
.libraries-tabs {
  margin-top: 4px;
}

.libraries-tabs :deep(.n-tabs-nav) {
  justify-content: center;
}

.libraries-tabs :deep(.n-tab-pane) {
  padding-top: 20px;
}
</style>
