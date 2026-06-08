<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton, NCheckbox, NCheckboxGroup, NInput, NInputNumber, NSelect, NSwitch, NModal, NSpace, NIcon, NSpin, NScrollbar, NTabs, NTabPane, NProgress,
} from 'naive-ui'
import { FolderOutline } from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import LibraryCard from '@/components/LibraryCard.vue'
import LibraryEditModal from '@/components/LibraryEditModal.vue'
import { AppIcons } from '@/icons/appIcons'
import { getPlatformIcon } from '@/icons/PlatformIcons'
import {
  getLibraries, addLibrary, refreshLibrary,
  getSystemConfig, updateSystemConfig, browseDirectories,
  getPlatforms, addPlatformLibrary, setPlatformEnable, deletePlatformLibrary, updatePlatformSortOrder, scanPlatformStudios, scanPlatformByFilename, rescrapeMissingStudio, getTaskSummary, updateLibrarySortOrder,
  discoverPlatformDimension, addPlatformsBatch, generatePlatformCover, generateAllPlatformCovers,
  listCoverStyles, generateAllLibraryCovers, type CoverStyle,
} from '@/api/client'
import { useTaskStream } from '@/composables/useTaskStream'

const { showToast } = useToast()

const scanThreadsOptions = [1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20].map((n) => ({ label: String(n), value: String(n) }))
const libTypeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
]

const libraries = ref<any[]>([])

// 扫描进度由 SSE 流驱动（ScanAdapter 把多库聚合为 children 数组）。
// 这里把 Snapshot.children 反向映射回旧模板期望的字段名，避免改模板。
const { snapshots } = useTaskStream()
const scanProgress = computed(() => {
  const s = snapshots.scan
  if (!s || !s.children) return [] as any[]
  return s.children.map((c) => {
    const libraryId = (c.message ?? '').replace(/^library=/, '')
    const status =
      c.status === 'running'   ? 'scanning'  :
      c.status === 'succeeded' ? 'completed' :
      c.status === 'failed'    ? 'failed'    : c.status
    return {
      LibraryId: libraryId,
      LibraryName: c.phase ?? '',
      Status: status,
      TotalItems: c.total,
      ProcessedItems: c.processed,
      Percentage: c.percent,
      CurrentItem: c.current ?? undefined,
      StartedAt: c.startedAt ?? 0,
      CompletedAt: c.completedAt ?? 0,
      Error: c.error ?? undefined,
    }
  })
})
const scanThreads = ref('3')
const fileWatcherEnabled = ref(true)
const scanning = ref(false)
const savingConfig = ref(false)
const activeView = ref<'libraries' | 'scan' | 'platforms'>('libraries')

// Platform libraries
const platformsData = ref<{ GlobalEnabled: boolean; Platforms: any[] }>({ GlobalEnabled: false, Platforms: [] })
const newPlatformName = ref('')
const platformScanning = ref(false)
const platformTask = ref<any>(null)
const platformPosition = ref<'before' | 'after'>('after')
const showLibraryItemCount = ref(true)
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

async function loadPlatforms() {
  try { platformsData.value = await getPlatforms() } catch {}
}

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

async function loadTaskSummary() {
  try {
    const summary = await getTaskSummary()
    platformTask.value = summary.platform
    rescrapeStatus.value = summary.platform?.rescrape || null
  } catch {}
}

async function toggleGlobalPlatform(enabled: boolean) {
  try {
    await updateSystemConfig({ platform_libraries_enabled: String(enabled) })
    platformsData.value.GlobalEnabled = enabled
    showToast(enabled ? '平台库已启用' : '平台库已禁用', 'success')
  } catch { showToast('操作失败', 'error') }
}

async function togglePlatform(id: string, enabled: boolean) {
  try {
    await setPlatformEnable(id, enabled)
    await loadPlatforms()
  } catch { showToast('操作失败', 'error') }
}

async function handleAddPlatform() {
  const name = newPlatformName.value.trim()
  if (!name) return
  try {
    await addPlatformLibrary(name)
    newPlatformName.value = ''
    await loadPlatforms()
    showToast('平台已添加', 'success')
  } catch { showToast('添加失败', 'error') }
}

// ===== 多维度发现 =====
const dimensionOptions = [
  { label: '片商 (studio)', value: 'studio' },
  { label: '番号前缀 (num_prefix)', value: 'num_prefix' },
  { label: '演员 (actor)', value: 'actor' },
]
const discoverDimension = ref<'studio' | 'num_prefix' | 'actor'>('studio')
const discoverSearch = ref('')
const discoverMinCount = ref(1)
const discoverLoading = ref(false)
const discoverResults = ref<{ Value: string; Count: number; AlreadyAdded: boolean }[]>([])
const discoverSelected = ref<string[]>([])

async function runDiscover() {
  discoverLoading.value = true
  discoverSelected.value = []
  try {
    const res = await discoverPlatformDimension(discoverDimension.value, discoverSearch.value.trim(), discoverMinCount.value)
    discoverResults.value = res.values || []
    if (discoverResults.value.length === 0) showToast('没有扫描到可分类的值', 'info')
  } catch (e: any) {
    showToast(e?.message || '扫描失败', 'error')
  } finally {
    discoverLoading.value = false
  }
}

async function addSelectedDimension() {
  if (discoverSelected.value.length === 0) return
  try {
    const res = await addPlatformsBatch(discoverDimension.value, discoverSelected.value)
    showToast(`已添加 ${res.added} 个虚拟库(默认关闭，可在下方启用)`, 'success')
    discoverSelected.value = []
    await runDiscover()
    await loadPlatforms()
  } catch { showToast('添加失败', 'error') }
}

// ===== 封面生成 =====
const coverGenerating = ref<Record<string, boolean>>({})
const coverGeneratingAll = ref(false)

async function handleGenCover(id: string) {
  coverGenerating.value = { ...coverGenerating.value, [id]: true }
  try {
    await generatePlatformCover(id)
    await loadPlatforms()
    showToast('封面已生成', 'success')
  } catch (e: any) {
    showToast(e?.message || '生成失败(可能没有海报素材)', 'error')
  } finally {
    coverGenerating.value = { ...coverGenerating.value, [id]: false }
  }
}

async function handleGenAllCovers() {
  coverGeneratingAll.value = true
  try {
    const res = await generateAllPlatformCovers()
    showToast(`封面生成完成:成功 ${res.generated},跳过 ${res.skipped}`, 'success')
    await loadPlatforms()
  } catch { showToast('批量生成失败', 'error') }
  finally { coverGeneratingAll.value = false }
}

async function handleDeletePlatform(id: string) {
  try {
    await deletePlatformLibrary(id)
    await loadPlatforms()
    showToast('平台已删除', 'success')
  } catch { showToast('删除失败', 'error') }
}

async function movePlatform(idx: number, dir: number) {
  const list = platformsData.value?.Platforms || []
  const j = idx + dir
  if (j < 0 || j >= list.length) return
  const ids = list.map((p: any) => p.Id)
  ;[ids[idx], ids[j]] = [ids[j], ids[idx]]
  try {
    await updatePlatformSortOrder(ids)
    await loadPlatforms()
  } catch { showToast('排序失败', 'error') }
}

async function handleScanStudios() {
  platformScanning.value = true
  try {
    const res = await scanPlatformStudios()
    await loadTaskSummary()
    showToast(`正在从 TMDB 扫描 ${res.total} 个项目`, 'success')
  } catch { showToast('扫描失败', 'error') }
  setTimeout(() => { platformScanning.value = false; loadPlatforms(); void loadTaskSummary() }, 5000)
}

const filenameScanning = ref(false)
async function handleScanFilename() {
  filenameScanning.value = true
  try {
    const res = await scanPlatformByFilename()
    showToast(`从文件名识别完成，更新了 ${res.updated} 个项目`, 'success')
    await loadPlatforms()
    await loadTaskSummary()
  } catch { showToast('扫描失败', 'error') }
  filenameScanning.value = false
}

const rescraping = ref(false)
const rescrapeStatus = ref<any>(null)
let rescrapeTimer: ReturnType<typeof setInterval> | null = null

async function pollRescrapeProgress() {
  try {
    await loadTaskSummary()
    const p = rescrapeStatus.value
    if (!p.running && rescrapeTimer) {
      clearInterval(rescrapeTimer)
      rescrapeTimer = null
      rescraping.value = false
      loadPlatforms()
    }
  } catch {}
}

async function handleRescrape() {
  rescraping.value = true
  rescrapeStatus.value = null
  try {
    const res = await rescrapeMissingStudio()
    showToast(`正在重新刮削 ${res.total} 个项目`, 'success')
    if (rescrapeTimer) clearInterval(rescrapeTimer)
    rescrapeTimer = setInterval(pollRescrapeProgress, 2000)
  } catch { showToast('刮削失败', 'error'); rescraping.value = false }
}

const showAddLib = ref(false)
const newLibName = ref('')
const newLibType = ref('movies')
const newLibPaths = ref<string[]>([])
const newLibPathInput = ref('')
const solidModalMenuProps = { class: 'solid-modal-menu' }
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

const showDirBrowser = ref(false)
const dirBrowserPath = ref('/mnt')
const dirBrowserDirs = ref<{ Name: string; Path: string }[]>([])
const dirBrowserLoading = ref(false)

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

async function moveLibrary(index: number, direction: 'up' | 'down') {
  const swapIdx = direction === 'up' ? index - 1 : index + 1
  if (swapIdx < 0 || swapIdx >= libraries.value.length) return
  const arr = [...libraries.value]
  ;[arr[index], arr[swapIdx]] = [arr[swapIdx], arr[index]]
  libraries.value = arr
  const orders = arr.map((lib: any, i: number) => ({ Id: lib.ItemId, SortOrder: i }))
  try {
    await updateLibrarySortOrder(orders)
  } catch { showToast('排序失败', 'error') }
}

function scanProgForLib(libId: string) {
  return scanProgress.value.find((s: any) => s.LibraryId === libId)
}

function libNameForScan(libId: string) {
  return libraries.value.find((l) => l.ItemId === libId)?.Name || libId
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

async function loadDirBrowser(path: string) {
  dirBrowserLoading.value = true
  try {
    const res = await browseDirectories(path)
    dirBrowserPath.value = res.Path
    dirBrowserDirs.value = res.Directories || []
  } catch {
    showToast('无法读取目录', 'error')
  } finally {
    dirBrowserLoading.value = false
  }
}

function openDirBrowser() { showDirBrowser.value = true; void loadDirBrowser('/mnt') }

function dirParentPath() {
  const p = dirBrowserPath.value
  if (p === '/') return
  void loadDirBrowser(p.substring(0, p.lastIndexOf('/')) || '/')
}

function addPathToList(path: string) {
  const p = path.trim()
  if (p && !newLibPaths.value.includes(p)) newLibPaths.value = [...newLibPaths.value, p]
}

function removePathFromList(index: number) {
  newLibPaths.value = newLibPaths.value.filter((_, i) => i !== index)
}

function handleAddPathManual() {
  addPathToList(newLibPathInput.value)
  newLibPathInput.value = ''
}

async function handleAddLibrary(e?: Event) {
  e?.preventDefault?.()
  if (!newLibName.value || newLibPaths.value.length === 0) return
  try {
    await addLibrary(newLibName.value, newLibType.value, newLibPaths.value)
    showAddLib.value = false
    newLibName.value = ''
    newLibPaths.value = []
    newLibPathInput.value = ''
    showToast('媒体库添加成功', 'success')
    libraries.value = await getLibraries()
  } catch {
    showToast('添加媒体库失败', 'error')
  }
}

const timers: ReturnType<typeof setInterval>[] = []

onMounted(() => {
  getLibraries().then((l) => (libraries.value = l)).catch(() => {})
  getSystemConfig().then((cfg: any) => {
    scanThreads.value = cfg.scan_threads || '3'
    fileWatcherEnabled.value = cfg.file_watcher_enabled !== 'false'
    platformPosition.value = cfg.platform_libraries_position === 'before' ? 'before' : 'after'
    showLibraryItemCount.value = cfg.library_show_item_count !== 'false'
  }).catch(() => {})
  loadPlatforms()
  loadTaskSummary().then(() => {
    if (rescrapeStatus.value?.running) {
      rescraping.value = true
      rescrapeTimer = setInterval(pollRescrapeProgress, 2000)
    }
  }).catch(() => {})
  // platform rescrape 的 task summary 不在作业调度范围内，保留独立轮询。
  timers.push(setInterval(() => { void loadTaskSummary() }, 3000))
})

onUnmounted(() => {
  timers.forEach((t) => clearInterval(t))
  if (rescrapeTimer) clearInterval(rescrapeTimer)
})
</script>

<template>
  <page-shell title="媒体库" :icon="AppIcons.library" description="管理媒体库文件夹与扫描设置">
    <n-tabs
      :value="activeView"
      type="segment"
      size="large"
      class="libraries-tabs"
      @update:value="(value) => activeView = value as 'libraries' | 'scan' | 'platforms'"
    >
      <n-tab-pane name="libraries" tab="媒体库">
        <n-space justify="center" class="libraries-actions">
          <n-button secondary @click="showAddLib = true">+ 添加媒体库</n-button>
          <n-button secondary :disabled="libraries.length === 0" @click="openGenerateAllCovers">生成所有封面</n-button>
          <n-button type="primary" @click="handleScan" :disabled="scanning || scanProgress.some((s: any) => s.Status === 'scanning')" :loading="scanning">
            {{ scanProgress.some((s: any) => s.Status === 'scanning') ? '扫描中...' : '扫描所有媒体库' }}
          </n-button>
        </n-space>

        <div v-if="libraries.length === 0" class="lib-empty-card">
          <div class="lib-empty">尚未配置媒体库。点击"添加媒体库"开始使用。</div>
        </div>
        <div v-else class="lib-grid">
          <div v-for="(lib, idx) in libraries" :key="lib.ItemId" class="lib-card-wrapper">
            <LibraryCard
              :lib="lib"
              :scan-prog="scanProgForLib(lib.ItemId)"
              :show-item-count="showLibraryItemCount"
              @click="openEditModal"
            />
            <div class="lib-sort-btns">
              <n-button text size="tiny" :disabled="idx === 0" @click.stop="moveLibrary(idx, 'up')">&#9650;</n-button>
              <n-button text size="tiny" :disabled="idx === libraries.length - 1" @click.stop="moveLibrary(idx, 'down')">&#9660;</n-button>
            </div>
          </div>
        </div>
      </n-tab-pane>

      <n-tab-pane name="platforms" tab="平台库">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">平台媒体库</h3>
              <div class="settings-card-desc">根据 TMDB 的出品平台信息（Netflix、HBO 等）自动生成虚拟媒体库，在播放器中可见。</div>
            </div>
          </div>

          <div class="setting-row">
            <div>
              <div class="setting-label">启用平台库</div>
              <div class="setting-desc">开启后，已启用的平台将作为虚拟媒体库显示在播放器中</div>
            </div>
            <n-switch :value="platformsData.GlobalEnabled" @update:value="toggleGlobalPlatform" />
          </div>

          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">从 TMDB 获取平台</div>
              <div class="setting-desc">通过 TMDB API 获取 networks/出品公司（需已刮削，速度较慢）</div>
              <div v-if="platformTask" class="setting-desc" style="margin-top: 6px">
                当前待平台识别 {{ platformTask.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，其中可直接扫描 {{ platformTask.pending_tmdb_ready_total || 0 }} 项
              </div>
            </div>
            <n-button secondary @click="handleScanStudios" :loading="platformScanning" :disabled="platformScanning || ((platformTask?.pending_tmdb_ready_total || 0) === 0 && !platformTask?.scan_running)">
              {{ platformScanning ? '扫描中...' : 'TMDB 扫描' }}
            </n-button>
          </div>
          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">从文件名识别平台</div>
              <div class="setting-desc">分析文件名中的平台标识（NF/DSNP/ATVP/AMZN/HMAX 等），速度快覆盖广</div>
            </div>
            <n-button secondary @click="handleScanFilename" :loading="filenameScanning" :disabled="filenameScanning">
              {{ filenameScanning ? '扫描中...' : '文件名扫描' }}
            </n-button>
          </div>
          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">重新刮削无平台项目</div>
              <div class="setting-desc">对仍无平台信息的 Movie/Series 重新执行完整 TMDB 刮削（耗时较长）</div>
              <div v-if="platformTask && !rescrapeStatus?.running" class="setting-desc" style="margin-top: 6px">
                当前待重新刮削 {{ rescrapeStatus?.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，仍缺少 TMDB 的有 {{ platformTask.pending_metadata_total || 0 }} 项
              </div>
            </div>
            <n-button secondary type="warning" @click="handleRescrape" :loading="rescraping" :disabled="rescraping || (!!rescrapeStatus && !rescrapeStatus.running && (rescrapeStatus.pending_total || 0) === 0)">
              {{ rescraping ? '刮削中...' : '重新刮削' }}
            </n-button>
          </div>
          <div v-if="rescrapeStatus && rescrapeStatus.running" class="rescrape-progress">
            <n-progress type="line" :percentage="rescrapeStatus.percentage" :show-indicator="true" status="info" />
            <div class="rescrape-stats">
              已处理 {{ rescrapeStatus.processed }} / {{ rescrapeStatus.total }}
              <span style="color: #18a058; margin-left: 12px">成功 {{ rescrapeStatus.success }}</span>
              <span style="color: #f0a020; margin-left: 12px">未找到 {{ rescrapeStatus.not_found }}</span>
              <span style="color: #d03050; margin-left: 12px">请求失败 {{ rescrapeStatus.fetch_error }}</span>
            </div>
          </div>
          <div v-else-if="rescrapeStatus && !rescrapeStatus.running && rescrapeStatus.total > 0" class="rescrape-progress">
            <div class="rescrape-stats">
              刮削完成: 共 {{ rescrapeStatus.total }} 项
              <span style="color: #18a058; margin-left: 12px">成功 {{ rescrapeStatus.success }}</span>
              <span style="color: #f0a020; margin-left: 12px">TMDB未收录 {{ rescrapeStatus.not_found }}</span>
              <span style="color: #d03050; margin-left: 12px">网络错误 {{ rescrapeStatus.fetch_error }}</span>
            </div>
          </div>

          <!-- 多维度发现:扫描本地数据 → 勾选添加 -->
          <div style="margin-top: 16px; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); padding-top: 16px">
            <div class="setting-label" style="margin-bottom: 8px">扫描分类（按维度发现，勾选后添加）</div>
            <div style="display: flex; gap: 8px; flex-wrap: wrap; align-items: center">
              <n-select v-model:value="discoverDimension" :options="dimensionOptions" size="small" style="width: 180px" />
              <n-input v-model:value="discoverSearch" placeholder="搜索（可选）" size="small" style="flex: 1; min-width: 120px" @keydown.enter.prevent="runDiscover" />
              <n-input-number v-model:value="discoverMinCount" :min="1" size="small" style="width: 110px" title="最少影片数" />
              <n-button secondary size="small" :loading="discoverLoading" @click="runDiscover">扫描</n-button>
            </div>
            <div v-if="discoverResults.length > 0" style="margin-top: 10px">
              <n-checkbox-group v-model:value="discoverSelected">
                <div class="discover-grid">
                  <n-checkbox v-for="d in discoverResults" :key="d.Value" :value="d.Value" :disabled="d.AlreadyAdded">
                    {{ d.Value }} <span class="platform-count">{{ d.Count }}</span>
                    <span v-if="d.AlreadyAdded" style="color: var(--n-text-color-disabled); font-size: 11px">(已加)</span>
                  </n-checkbox>
                </div>
              </n-checkbox-group>
              <div style="margin-top: 8px; display: flex; gap: 8px; align-items: center">
                <n-button type="primary" size="small" :disabled="discoverSelected.length === 0" @click="addSelectedDimension">
                  添加所选 ({{ discoverSelected.length }})
                </n-button>
                <span class="setting-desc">共 {{ discoverResults.length }} 项 · 添加后默认关闭，需在下方启用</span>
              </div>
            </div>
          </div>

          <div style="margin-top: 16px; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); padding-top: 16px">
            <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px">
              <div class="setting-label">平台/虚拟库列表</div>
              <n-button text size="tiny" :loading="coverGeneratingAll" @click="handleGenAllCovers">一键生成封面</n-button>
            </div>
            <div v-for="(p, idx) in platformsData.Platforms" :key="p.Id" class="platform-row">
              <img v-if="p.CoverUrl" :src="p.CoverUrl" class="platform-cover-thumb" />
              <img v-else-if="p.LogoUrl" :src="p.LogoUrl" class="platform-logo-icon" />
              <n-icon v-else size="28" style="margin-right: 10px; flex-shrink: 0"><component :is="getPlatformIcon(p.PlatformName)" /></n-icon>
              <div style="flex: 1; min-width: 0">
                <span class="platform-name">{{ p.DisplayName || p.PlatformName }}</span>
                <span class="platform-dim-badge">{{ p.Dimension }}</span>
                <span class="platform-count">{{ p.ItemCount }} 部</span>
              </div>
              <n-button text size="tiny" :loading="coverGenerating[p.Id]" @click="handleGenCover(p.Id)" title="生成封面">封面</n-button>
              <n-button text size="tiny" :disabled="idx === 0" @click="movePlatform(idx, -1)" title="上移" style="margin-left: 4px">↑</n-button>
              <n-button text size="tiny" :disabled="idx === platformsData.Platforms.length - 1" @click="movePlatform(idx, 1)" title="下移" style="margin-left: 2px">↓</n-button>
              <n-switch :value="p.Enabled" @update:value="(v: boolean) => togglePlatform(p.Id, v)" size="small" style="margin-left: 8px" />
              <n-button text type="error" size="tiny" @click="handleDeletePlatform(p.Id)" style="margin-left: 8px">&times;</n-button>
            </div>
          </div>

          <div style="margin-top: 16px; display: flex; gap: 8px">
            <n-input v-model:value="newPlatformName" placeholder="自定义片商名称(studio 维度)" size="small" style="flex: 1" @keydown.enter.prevent="handleAddPlatform" />
            <n-button secondary size="small" @click="handleAddPlatform">添加</n-button>
          </div>
        </div>
      </n-tab-pane>

      <n-tab-pane name="scan" tab="高级设置">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">高级设置</h3>
              <div class="settings-card-desc">扫描参数与媒体库显示偏好。</div>
            </div>
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">并发扫描线程数</div>
              <div class="setting-desc">同时处理的媒体文件数量，值越大扫描越快但占用资源越多</div>
            </div>
            <n-select v-model:value="scanThreads" :options="scanThreadsOptions" style="width: 100px" />
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">文件监听</div>
              <div class="setting-desc">实时监听媒体库目录变动，文件新增/删除后自动入库</div>
            </div>
            <n-switch v-model:value="fileWatcherEnabled" />
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">平台库排列位置</div>
              <div class="setting-desc">控制平台虚拟媒体库在播放器中的排列位置，位于普通媒体库前面或后面</div>
            </div>
            <n-select
              v-model:value="platformPosition"
              :options="[{ label: '媒体库前面', value: 'before' }, { label: '媒体库后面', value: 'after' }]"
              style="width: 140px"
            />
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">显示媒体总数</div>
              <div class="setting-desc">在媒体库和平台库卡片右上角显示媒体总数角标</div>
            </div>
            <n-switch v-model:value="showLibraryItemCount" />
          </div>
          <div class="settings-actions">
            <n-button type="primary" @click="saveLibrarySettings" :loading="savingConfig">保存设置</n-button>
          </div>
        </div>
      </n-tab-pane>
    </n-tabs>

    <n-modal v-model:show="showGenerateAllCovers" preset="card" title="生成所有媒体库封面" :style="[forceSolidModalStyle, { width: '520px', maxWidth: '92vw' }]" class="solid-modal-card force-solid-modal">
      <div class="form-group">
        <label class="form-label">封面风格</label>
        <n-select
          v-model:value="batchCoverStyle"
          :options="coverStyleOptions"
          :loading="!coverStylesLoaded"
          :menu-props="solidModalMenuProps"
          placeholder="选择风格"
        />
      </div>
      <div v-if="batchIsShowcase" class="batch-cover-options">
        <div class="form-group">
          <label class="form-label">预制图标</label>
          <n-select
            v-model:value="batchShowcaseIcon"
            :options="showcaseIconOptions"
            :menu-props="solidModalMenuProps"
          />
        </div>
        <div class="batch-cover-checks">
          <n-checkbox v-model:checked="batchShowcaseShowPosterTitles">显示海报标题</n-checkbox>
          <n-checkbox v-model:checked="batchShowcaseShowCount">显示媒体数量</n-checkbox>
        </div>
      </div>
      <div v-if="batchCoverResult" class="batch-cover-result">
        <div>共 {{ batchCoverResult.Total }} 个媒体库，成功 {{ batchCoverResult.Success }}，跳过 {{ batchCoverResult.Skipped }}，失败 {{ batchCoverResult.Failed }}</div>
        <div v-if="batchCoverIssues.length > 0" class="batch-cover-result-list">
          <div v-for="item in batchCoverIssues" :key="item.Id" class="batch-cover-result-item">
            {{ item.Name }}：{{ item.Message || item.Status }}
          </div>
        </div>
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showGenerateAllCovers = false">关闭</n-button>
          <n-button type="primary" :loading="generatingAllCovers" :disabled="!canGenerateAllCovers" @click="handleGenerateAllCovers">
            生成全部
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Add Library Modal -->
    <n-modal v-model:show="showAddLib" preset="card" title="添加媒体库" :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]" class="solid-modal-card force-solid-modal">
      <form @submit.prevent="handleAddLibrary">
        <div class="form-group">
          <label class="form-label">名称</label>
          <n-input v-model:value="newLibName" placeholder="例如：电影" />
        </div>
        <div class="form-group">
          <label class="form-label">类型</label>
          <n-select v-model:value="newLibType" :options="libTypeOptions" :menu-props="solidModalMenuProps" />
        </div>
        <div>
          <label class="form-label">路径</label>
          <div v-if="newLibPaths.length > 0" style="margin-bottom: 8px">
            <div v-for="(p, i) in newLibPaths" :key="i" class="path-chip">
              <span style="flex: 1; word-break: break-all">{{ p }}</span>
              <n-button text type="error" size="tiny" @click="removePathFromList(i)">&times;</n-button>
            </div>
          </div>
          <div style="display: flex; gap: 6px; align-items: stretch">
            <n-input v-model:value="newLibPathInput" placeholder="输入路径，如 /mnt/media/movies" style="flex: 1" @keydown.enter.prevent="handleAddPathManual" />
            <n-button secondary @click="handleAddPathManual">添加</n-button>
            <n-button secondary @click="openDirBrowser">
              <template #icon><n-icon><FolderOutline /></n-icon></template>
              浏览
            </n-button>
          </div>
        </div>
      </form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddLib = false">取消</n-button>
          <n-button type="primary" @click="handleAddLibrary">添加</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Dir Browser Modal -->
    <n-modal v-model:show="showDirBrowser" preset="card" title="选择文件夹" :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]" class="dir-browser-modal solid-modal-card force-solid-modal">
      <div class="dir-current">{{ dirBrowserPath }}</div>
      <n-scrollbar style="max-height: min(400px, 50vh)">
        <div v-if="dirBrowserPath !== '/'" @click="dirParentPath" class="dir-row" style="color: var(--app-text-muted)">&#8592; 上一级</div>
        <div v-if="dirBrowserLoading" style="padding: 20px; text-align: center; color: var(--app-text-muted)"><n-spin size="small" /> 加载中...</div>
        <div v-else-if="dirBrowserDirs.length === 0" style="padding: 20px; text-align: center; color: var(--app-text-muted)">没有子目录</div>
        <div v-else v-for="d in dirBrowserDirs" :key="d.Path" @click="loadDirBrowser(d.Path)" class="dir-row">
          <n-icon size="16"><FolderOutline /></n-icon>
          {{ d.Name }}
        </div>
      </n-scrollbar>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDirBrowser = false">取消</n-button>
          <n-button type="primary" @click="addPathToList(dirBrowserPath); showDirBrowser = false">选择当前目录</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Library Edit Modal -->
    <LibraryEditModal
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

.libraries-actions {
  margin-bottom: 18px;
}

.lib-grid {
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(2, 1fr);
}
@media (min-width: 600px)  { .lib-grid { grid-template-columns: repeat(3, 1fr); } }
@media (min-width: 960px)  { .lib-grid { grid-template-columns: repeat(4, 1fr); } }
@media (min-width: 1400px) { .lib-grid { grid-template-columns: repeat(5, 1fr); } }

.lib-card-wrapper { position: relative; }
.lib-sort-btns {
  position: absolute;
  top: 4px;
  left: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  z-index: 5;
  opacity: 0;
  transition: opacity 0.2s;
}
.lib-card-wrapper:hover .lib-sort-btns { opacity: 1; }
.lib-sort-btns .n-button {
  background: rgba(0,0,0,0.6);
  border-radius: 4px;
  padding: 2px 6px;
  color: #fff !important;
  font-size: 10px;
}

.lib-empty-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 8px 0;
}
.lib-empty { padding: 20px 24px; color: var(--app-text-muted); font-size: 14px; }

.settings-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 20px 24px;
}
.settings-card-header { margin-bottom: 10px; }
.settings-card-title { font-size: 15px; font-weight: 600; color: var(--app-text); margin-bottom: 6px; }
.settings-card-desc { font-size: 13px; color: var(--app-text-muted); }
.setting-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 0; }
.setting-row + .setting-row { border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.setting-label { font-size: 14px; color: var(--app-text); }
.setting-desc { font-size: 12px; color: var(--app-text-muted); margin-top: 2px; }
.settings-actions { margin-top: 16px; }

.form-group { margin-bottom: 20px; }
.form-label { display: block; font-size: 12px; color: var(--app-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500; }
.path-chip { display: flex; align-items: center; gap: 8px; padding: 6px 10px; background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(255,255,255,0.04))); border-radius: 4px; margin-bottom: 4px; font-size: 13px; color: var(--app-text); }

.batch-cover-options {
  padding-top: 4px;
}
.batch-cover-checks {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.batch-cover-result {
  margin-top: 14px;
  padding: 10px 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: 6px;
  background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04));
  color: var(--app-text);
  font-size: 13px;
}
.batch-cover-result-list {
  margin-top: 8px;
  max-height: 160px;
  overflow: auto;
  color: var(--app-text-muted);
}
.batch-cover-result-item + .batch-cover-result-item {
  margin-top: 4px;
}

.dir-current { background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(0,0,0,0.3))); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; color: var(--app-primary); word-break: break-all; font-family: monospace; }
.dir-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 14px; color: var(--app-text); }
.dir-row:hover { background: var(--app-modal-hover-bg, var(--app-surface-2, #2a2a2a)); }

.rescrape-progress { margin-top: 12px; padding: 12px 0; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.rescrape-stats { font-size: 13px; color: var(--app-text-muted); margin-top: 6px; }

.platform-logo-icon { width: 28px; height: 28px; border-radius: 6px; object-fit: cover; margin-right: 10px; flex-shrink: 0; }
.platform-cover-thumb { width: 48px; height: 27px; border-radius: 4px; object-fit: cover; margin-right: 10px; flex-shrink: 0; }
.platform-dim-badge { font-size: 10px; color: var(--app-text-muted); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); border-radius: 4px; padding: 0 5px; margin-left: 8px; vertical-align: middle; }
.discover-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 6px 12px; max-height: 320px; overflow-y: auto; padding: 4px 2px; }
.platform-row { display: flex; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.platform-row:last-child { border-bottom: none; }
.platform-name { font-size: 14px; color: var(--app-text); font-weight: 500; }
.platform-count { font-size: 12px; color: var(--app-text-muted); margin-left: 8px; }

@media (max-width: 640px) {
  .libraries-actions {
    justify-content: stretch !important;
  }

  .setting-row {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
}
</style>
