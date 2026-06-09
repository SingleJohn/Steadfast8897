<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton, NCheckbox, NCheckboxGroup, NInput, NInputNumber, NSelect, NSwitch, NModal, NSpace, NIcon, NSpin, NScrollbar, NTabs, NTabPane, NProgress, NPopconfirm,
} from 'naive-ui'
import { FolderOutline, MoveOutline } from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import LibraryCard from '@/components/LibraryCard.vue'
import LibraryEditModal from '@/components/LibraryEditModal.vue'
import { AppIcons } from '@/icons/appIcons'
import { getPlatformIcon } from '@/icons/PlatformIcons'
import {
  getLibraries, addLibrary, refreshLibrary, forceLibraryRescanOptions,
  getSystemConfig, updateSystemConfig, browseDirectories,
  getPlatforms, addPlatformLibrary, setPlatformEnable, deletePlatformLibrary, updatePlatformSortOrder, scanPlatformStudios, scanPlatformByFilename, rescrapeMissingStudio, getTaskSummary, updateLibrarySortOrder,
  discoverPlatformDimension, addPlatformsBatch, generatePlatformCover, generateAllPlatformCovers, renamePlatform,
  listCoverStyles, generateAllLibraryCovers, type CoverStyle,
  getViews, setLibraryDisplayOrder, addPlatformValues, removePlatformValue, deletePlatformCover,
} from '@/api/client'
import { useTaskStream } from '@/composables/useTaskStream'

const { showToast } = useToast()

const scanThreadsOptions = [1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20].map((n) => ({ label: String(n), value: String(n) }))
const libTypeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
  { label: '混合', value: 'mixed' },
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
const activeView = ref<'libraries' | 'scan' | 'platforms' | 'order'>('libraries')
const draggingLibraryId = ref<string | null>(null)
const dragOverLibraryId = ref<string | null>(null)
const dragStartLibraries = ref<any[]>([])
const libraryDragChanged = ref(false)
const libraryDragCommitted = ref(false)
const savingLibraryOrder = ref(false)

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

// ===== 平台库封面生成(可选风格) =====
const showPlatformCover = ref(false)
const platformCoverTargetId = ref<string | null>(null) // null = 批量生成全部
const platformCoverStyle = ref('')
const platformShowcaseIcon = ref('auto')
const platformShowcaseShowPosterTitles = ref(true)
const platformShowcaseShowCount = ref(true)
const generatingPlatformCover = ref(false)
const platformCoverIsShowcase = computed(() => platformCoverStyle.value === 'showcase')

async function openPlatformCover(id: string | null) {
  platformCoverTargetId.value = id
  showPlatformCover.value = true
  await ensureCoverStylesLoaded()
  if (!platformCoverStyle.value && coverStyles.value.length > 0) {
    platformCoverStyle.value = coverStyles.value.find((s) => s.name === 'showcase')?.name || coverStyles.value[0].name
  }
}

function platformCoverOptions() {
  if (platformCoverStyle.value !== 'showcase') return undefined
  const o: Record<string, any> = {
    ShowPosterTitles: platformShowcaseShowPosterTitles.value,
    ShowCount: platformShowcaseShowCount.value,
  }
  if (platformShowcaseIcon.value !== 'auto') o.Icon = platformShowcaseIcon.value
  return o
}

async function confirmPlatformCover() {
  if (!platformCoverStyle.value) return
  generatingPlatformCover.value = true
  try {
    const opts = platformCoverOptions()
    if (platformCoverTargetId.value) {
      await generatePlatformCover(platformCoverTargetId.value, platformCoverStyle.value, opts)
      showToast('封面已生成', 'success')
    } else {
      const res = await generateAllPlatformCovers(platformCoverStyle.value, opts)
      showToast(`封面生成完成：成功 ${res.generated}，跳过 ${res.skipped}`, 'success')
    }
    showPlatformCover.value = false
    await loadPlatforms()
  } catch (e: any) {
    showToast(e?.message || '生成失败(可能没有海报素材)', 'error')
  } finally {
    generatingPlatformCover.value = false
  }
}

// 恢复默认封面:清除生成的封面,内置平台(如 Netflix)回退到默认 logo。
async function handleRestoreCover(id: string) {
  try {
    await deletePlatformCover(id)
    await loadPlatforms()
    showToast('已恢复默认封面', 'success')
  } catch {
    showToast('恢复失败', 'error')
  }
}

// ===== 整体排序(实际库 + 虚拟库统一) =====
const orderList = ref<{ kind: 'library' | 'platform'; id: string; name: string; type: string }[]>([])
const savingOrder = ref(false)

// 直接用 getViews 的结果作为来源:它已按统一展示顺序返回(后端按 library_display_order 排序),
// 只含会出现在播放器里的条目(全部实际库 + 已启用虚拟库)。
async function loadOrderList() {
  try {
    const res = await getViews()
    orderList.value = (res.Items || []).map((it: any) => ({
      kind: it.PlatformLibrary ? 'platform' : 'library',
      id: it.Id,
      name: it.Name,
      type: it.PlatformLibrary ? '虚拟库' : '媒体库',
    }))
  } catch {
    showToast('加载顺序失败', 'error')
  }
}

function orderKey(e: { kind: string; id: string }) {
  return e.kind + ':' + e.id
}

// 拖动排序(与媒体库一致):dragover 实时换位,drop 落地自动保存。
const draggingOrderKey = ref<string | null>(null)
const dragOverOrderKey = ref<string | null>(null)
const orderDragStart = ref<typeof orderList.value>([])
const orderDragChanged = ref(false)
const orderDragCommitted = ref(false)

function reorderOrderList(fromIndex: number, toIndex: number) {
  if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= orderList.value.length || toIndex >= orderList.value.length) {
    return false
  }
  const arr = [...orderList.value]
  const [moved] = arr.splice(fromIndex, 1)
  arr.splice(toIndex, 0, moved)
  orderList.value = arr
  return true
}

async function persistOrder() {
  savingOrder.value = true
  try {
    await setLibraryDisplayOrder(orderList.value.map((e) => ({ Kind: e.kind, Id: e.id })))
  } catch {
    showToast('排序保存失败，已恢复服务器顺序', 'error')
    await loadOrderList()
  } finally {
    savingOrder.value = false
  }
}

function handleOrderDragStart(index: number, e: DragEvent) {
  const item = orderList.value[index]
  if (!item || orderList.value.length <= 1 || savingOrder.value) return
  draggingOrderKey.value = orderKey(item)
  dragOverOrderKey.value = orderKey(item)
  orderDragStart.value = [...orderList.value]
  orderDragChanged.value = false
  orderDragCommitted.value = false
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', orderKey(item))
  }
}

function handleOrderDragOver(index: number, e: DragEvent) {
  if (!draggingOrderKey.value) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  const target = orderList.value[index]
  if (!target) return
  dragOverOrderKey.value = orderKey(target)
  const fromIndex = orderList.value.findIndex((x) => orderKey(x) === draggingOrderKey.value)
  if (reorderOrderList(fromIndex, index)) orderDragChanged.value = true
}

async function finishOrderDrag(commit: boolean) {
  if (!draggingOrderKey.value) return
  if (!commit) {
    if (orderDragChanged.value && orderDragStart.value.length > 0) orderList.value = orderDragStart.value
    resetOrderDrag()
    return
  }
  if (orderDragChanged.value) await persistOrder()
  resetOrderDrag()
}

function handleOrderDrop(e: DragEvent) {
  if (!draggingOrderKey.value) return
  e.preventDefault()
  orderDragCommitted.value = true
  void finishOrderDrag(true)
}

function handleOrderDragEnd() {
  if (orderDragCommitted.value) return
  void finishOrderDrag(false)
}

function resetOrderDrag() {
  draggingOrderKey.value = null
  dragOverOrderKey.value = null
  orderDragStart.value = []
  orderDragChanged.value = false
  orderDragCommitted.value = false
}

async function moveOrderKeyboard(index: number, dir: number) {
  if (savingOrder.value) return
  if (reorderOrderList(index, index + dir)) await persistOrder()
}

function onOrderDragHandleKeydown(index: number, e: KeyboardEvent) {
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    void moveOrderKeyboard(index, -1)
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    void moveOrderKeyboard(index, 1)
  }
}

watch(activeView, (v) => {
  if (v === 'order') void loadOrderList()
})

// ===== 虚拟库重命名 =====
const showRename = ref(false)
const renameTargetId = ref<string | null>(null)
const renameValue = ref('')

function openRename(p: any) {
  renameTargetId.value = p.Id
  renameValue.value = p.CustomName || ''
  showRename.value = true
}

async function confirmRename() {
  if (!renameTargetId.value) return
  try {
    await renamePlatform(renameTargetId.value, renameValue.value.trim())
    showRename.value = false
    await loadPlatforms()
    showToast('名称已更新', 'success')
  } catch {
    showToast('重命名失败', 'error')
  }
}

// ===== 多值聚合(把簡繁/译名等多个值合并到一个虚拟库) =====
const showAlias = ref(false)
const aliasTarget = ref<any>(null)
const aliasValues = ref<string[]>([])
const aliasSearch = ref('')
const aliasResults = ref<{ Value: string; Count: number; AlreadyAdded: boolean }[]>([])
const aliasSelected = ref<string[]>([])
const aliasLoading = ref(false)

function syncAliasTarget() {
  const updated = (platformsData.value?.Platforms || []).find((x: any) => x.Id === aliasTarget.value?.Id)
  if (updated) {
    aliasTarget.value = updated
    aliasValues.value = [...(updated.MatchValues || [updated.MatchValue])]
  }
}

function openAlias(p: any) {
  aliasTarget.value = p
  aliasValues.value = [...(p.MatchValues || [p.MatchValue])]
  aliasSearch.value = ''
  aliasResults.value = []
  aliasSelected.value = []
  showAlias.value = true
}

async function runAliasDiscover() {
  if (!aliasTarget.value) return
  aliasLoading.value = true
  aliasSelected.value = []
  try {
    const res = await discoverPlatformDimension(aliasTarget.value.Dimension, aliasSearch.value.trim(), 1)
    aliasResults.value = (res.values || []).filter((v) => !aliasValues.value.includes(v.Value))
  } catch {
    showToast('扫描失败', 'error')
  } finally {
    aliasLoading.value = false
  }
}

async function addAliasSelected() {
  if (!aliasTarget.value || aliasSelected.value.length === 0) return
  try {
    await addPlatformValues(aliasTarget.value.Id, aliasSelected.value)
    showToast(`已合并 ${aliasSelected.value.length} 个值`, 'success')
    await loadPlatforms()
    syncAliasTarget()
    aliasSelected.value = []
    await runAliasDiscover()
  } catch {
    showToast('合并失败', 'error')
  }
}

async function removeAlias(value: string) {
  if (!aliasTarget.value) return
  if (value === aliasTarget.value.MatchValue) {
    showToast('主匹配值不可移除', 'info')
    return
  }
  try {
    await removePlatformValue(aliasTarget.value.Id, value)
    await loadPlatforms()
    syncAliasTarget()
  } catch {
    showToast('移除失败', 'error')
  }
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

function reorderLibrary(fromIndex: number, toIndex: number) {
  if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= libraries.value.length || toIndex >= libraries.value.length) {
    return false
  }
  const arr = [...libraries.value]
  const [moved] = arr.splice(fromIndex, 1)
  arr.splice(toIndex, 0, moved)
  libraries.value = arr
  return true
}

async function persistLibraryOrder() {
  const arr = [...libraries.value]
  const orders = arr.map((lib: any, i: number) => ({ Id: lib.ItemId, SortOrder: i }))
  savingLibraryOrder.value = true
  try {
    await updateLibrarySortOrder(orders)
  } catch {
    showToast('排序保存失败，已恢复服务器顺序', 'error')
    try {
      libraries.value = await getLibraries()
    } catch {
      if (dragStartLibraries.value.length > 0) libraries.value = dragStartLibraries.value
    }
  } finally {
    savingLibraryOrder.value = false
  }
}

async function moveLibrary(index: number, direction: 'up' | 'down') {
  const targetIndex = direction === 'up' ? index - 1 : index + 1
  if (!reorderLibrary(index, targetIndex)) return
  await persistLibraryOrder()
}

function handleLibraryDragStart(index: number, e: DragEvent) {
  const lib = libraries.value[index]
  if (!lib || libraries.value.length <= 1 || savingLibraryOrder.value) return
  draggingLibraryId.value = lib.ItemId
  dragOverLibraryId.value = lib.ItemId
  dragStartLibraries.value = [...libraries.value]
  libraryDragChanged.value = false
  libraryDragCommitted.value = false
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', lib.ItemId)
  }
}

function handleLibraryDragOver(index: number, e: DragEvent) {
  if (!draggingLibraryId.value) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  const target = libraries.value[index]
  if (!target) return
  dragOverLibraryId.value = target.ItemId
  const fromIndex = libraries.value.findIndex((lib: any) => lib.ItemId === draggingLibraryId.value)
  if (reorderLibrary(fromIndex, index)) {
    libraryDragChanged.value = true
  }
}

async function finishLibraryDrag(commit: boolean) {
  if (!draggingLibraryId.value) return
  if (!commit) {
    if (libraryDragChanged.value && dragStartLibraries.value.length > 0) {
      libraries.value = dragStartLibraries.value
    }
    resetLibraryDrag()
    return
  }
  if (libraryDragChanged.value) {
    await persistLibraryOrder()
  }
  resetLibraryDrag()
}

function handleLibraryDrop(e: DragEvent) {
  if (!draggingLibraryId.value) return
  e.preventDefault()
  libraryDragCommitted.value = true
  void finishLibraryDrag(true)
}

function handleLibraryDragEnd() {
  if (libraryDragCommitted.value) return
  void finishLibraryDrag(false)
}

function resetLibraryDrag() {
  draggingLibraryId.value = null
  dragOverLibraryId.value = null
  dragStartLibraries.value = []
  libraryDragChanged.value = false
  libraryDragCommitted.value = false
}

function onLibraryDragHandleKeydown(index: number, e: KeyboardEvent) {
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    void moveLibrary(index, 'up')
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    void moveLibrary(index, 'down')
  }
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
      @update:value="(value) => activeView = value as 'libraries' | 'scan' | 'platforms' | 'order'"
    >
      <n-tab-pane name="libraries" tab="媒体库">
        <n-space justify="center" class="libraries-actions">
          <n-button secondary @click="showAddLib = true">+ 添加媒体库</n-button>
          <n-button secondary :disabled="libraries.length === 0" @click="openGenerateAllCovers">生成所有封面</n-button>
          <n-button type="primary" @click="handleScan" :disabled="scanning || scanProgress.some((s: any) => s.Status === 'scanning')" :loading="scanning">
            {{ scanProgress.some((s: any) => s.Status === 'scanning') ? '扫描中...' : '扫描所有媒体库' }}
          </n-button>
          <n-popconfirm
            positive-text="强制重扫"
            negative-text="取消"
            @positive-click="handleForceScan"
          >
            <template #trigger>
              <n-button
                secondary
                type="warning"
                :disabled="libraries.length === 0 || scanning || scanProgress.some((s: any) => s.Status === 'scanning')"
              >
                强制重扫
              </n-button>
            </template>
            扫描完成后会重新读取本地 NFO 和图片，所有媒体库都会入刷新队列。
          </n-popconfirm>
        </n-space>

        <div v-if="libraries.length === 0" class="lib-empty-card">
          <div class="lib-empty">尚未配置媒体库。点击"添加媒体库"开始使用。</div>
        </div>
        <div v-else class="lib-grid">
          <div
            v-for="(lib, idx) in libraries"
            :key="lib.ItemId"
            class="lib-card-wrapper"
            :class="{
              'lib-card-wrapper-dragging': draggingLibraryId === lib.ItemId,
              'lib-card-wrapper-over': dragOverLibraryId === lib.ItemId && draggingLibraryId !== lib.ItemId,
            }"
            @dragover="handleLibraryDragOver(idx, $event)"
            @drop="handleLibraryDrop"
          >
            <LibraryCard
              :lib="lib"
              :scan-prog="scanProgForLib(lib.ItemId)"
              :show-item-count="showLibraryItemCount"
              @click="openEditModal"
            />
            <button
              type="button"
              class="lib-drag-handle"
              :draggable="libraries.length > 1 && !savingLibraryOrder"
              :disabled="libraries.length <= 1 || savingLibraryOrder"
              :aria-label="`拖动排序：${lib.Name}`"
              title="拖动排序"
              @click.stop
              @keydown.stop="onLibraryDragHandleKeydown(idx, $event)"
              @dragstart.stop="handleLibraryDragStart(idx, $event)"
              @dragend.stop="handleLibraryDragEnd"
            >
              <n-icon size="17" aria-hidden="true">
                <MoveOutline />
              </n-icon>
            </button>
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
              <n-button text size="tiny" @click="openPlatformCover(null)">一键生成封面</n-button>
            </div>
            <div v-for="(p, idx) in platformsData.Platforms" :key="p.Id" class="platform-row">
              <img v-if="p.CoverUrl" :src="p.CoverUrl" class="platform-cover-thumb" />
              <img v-else-if="p.LogoUrl" :src="p.LogoUrl" class="platform-logo-icon" />
              <n-icon v-else size="28" style="margin-right: 10px; flex-shrink: 0"><component :is="getPlatformIcon(p.PlatformName)" /></n-icon>
              <div style="flex: 1; min-width: 0">
                <span class="platform-name">{{ p.DisplayName || p.PlatformName }}</span>
                <span class="platform-dim-badge">{{ p.Dimension }}</span>
                <span v-if="(p.MatchValues?.length || 1) > 1" class="platform-dim-badge" title="已聚合的匹配值数量">聚合 {{ p.MatchValues.length }}</span>
                <span class="platform-count">{{ p.ItemCount }} 部</span>
              </div>
              <n-button text size="tiny" @click="openAlias(p)" title="聚合多个匹配值" style="margin-left: 4px">聚合</n-button>
              <n-button text size="tiny" @click="openRename(p)" title="重命名" style="margin-left: 4px">重命名</n-button>
              <n-button text size="tiny" @click="openPlatformCover(p.Id)" title="生成封面" style="margin-left: 4px">封面</n-button>
              <n-button v-if="p.HasCover" text size="tiny" @click="handleRestoreCover(p.Id)" title="恢复默认封面" style="margin-left: 4px">恢复默认</n-button>
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

      <n-tab-pane name="order" tab="整体排序">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">整体排序</h3>
              <div class="settings-card-desc">调整实际媒体库与虚拟库在播放器中的统一显示顺序。保存后立即生效；未参与排序的新库会自动排在末尾。</div>
            </div>
          </div>
          <div v-if="orderList.length === 0" class="setting-desc" style="padding: 12px 0">暂无可排序的媒体库</div>
          <div v-else>
            <div
              v-for="(e, idx) in orderList"
              :key="e.kind + ':' + e.id"
              class="order-row"
              :class="{
                'order-row-dragging': draggingOrderKey === (e.kind + ':' + e.id),
                'order-row-over': dragOverOrderKey === (e.kind + ':' + e.id) && draggingOrderKey !== (e.kind + ':' + e.id),
              }"
              @dragover="handleOrderDragOver(idx, $event)"
              @drop="handleOrderDrop"
            >
              <button
                type="button"
                class="order-drag-handle"
                :draggable="orderList.length > 1 && !savingOrder"
                :disabled="orderList.length <= 1 || savingOrder"
                :aria-label="`拖动排序：${e.name}`"
                title="拖动排序"
                @click.stop
                @keydown.stop="onOrderDragHandleKeydown(idx, $event)"
                @dragstart.stop="handleOrderDragStart(idx, $event)"
                @dragend.stop="handleOrderDragEnd"
              >
                <n-icon size="16" aria-hidden="true"><MoveOutline /></n-icon>
              </button>
              <span class="order-kind-badge" :class="e.kind === 'platform' ? 'is-platform' : 'is-library'">{{ e.type }}</span>
              <span class="order-name">{{ e.name }}</span>
            </div>
            <div class="setting-desc" style="margin-top: 10px">拖动左侧手柄即可调整顺序，松手后自动保存。</div>
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

    <!-- Platform Cover Modal -->
    <n-modal v-model:show="showPlatformCover" preset="card" :title="platformCoverTargetId ? '生成虚拟库封面' : '一键生成虚拟库封面'" :style="[forceSolidModalStyle, { width: '480px', maxWidth: '92vw' }]" class="solid-modal-card force-solid-modal">
      <div class="form-group">
        <label class="form-label">封面风格</label>
        <n-select
          v-model:value="platformCoverStyle"
          :options="coverStyleOptions"
          :loading="!coverStylesLoaded"
          :menu-props="solidModalMenuProps"
          placeholder="选择风格"
        />
      </div>
      <div v-if="platformCoverIsShowcase" class="batch-cover-options">
        <div class="form-group">
          <label class="form-label">预制图标</label>
          <n-select
            v-model:value="platformShowcaseIcon"
            :options="showcaseIconOptions"
            :menu-props="solidModalMenuProps"
          />
        </div>
        <div class="batch-cover-checks">
          <n-checkbox v-model:checked="platformShowcaseShowPosterTitles">显示海报标题</n-checkbox>
          <n-checkbox v-model:checked="platformShowcaseShowCount">显示媒体数量</n-checkbox>
        </div>
      </div>
      <div v-if="!platformCoverTargetId" class="setting-desc">将为所有已启用的虚拟库生成封面，无海报素材的会自动跳过。</div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showPlatformCover = false">取消</n-button>
          <n-button type="primary" :loading="generatingPlatformCover" :disabled="!platformCoverStyle || generatingPlatformCover" @click="confirmPlatformCover">
            {{ platformCoverTargetId ? '生成' : '生成全部' }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Platform Rename Modal -->
    <n-modal v-model:show="showRename" preset="card" title="自定义虚拟库名称" :style="[forceSolidModalStyle, { width: '440px', maxWidth: '92vw' }]" class="solid-modal-card force-solid-modal">
      <div class="form-group">
        <label class="form-label">显示名称</label>
        <n-input v-model:value="renameValue" placeholder="留空则恢复默认名称" @keydown.enter.prevent="confirmRename" />
        <div class="setting-desc" style="margin-top: 6px">仅改变在播放器中显示的名称，不影响分组匹配与图标。</div>
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRename = false">取消</n-button>
          <n-button type="primary" @click="confirmRename">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Platform Alias / Aggregation Modal -->
    <n-modal v-model:show="showAlias" preset="card" :title="`聚合匹配值 · ${aliasTarget?.DisplayName || aliasTarget?.PlatformName || ''}`" :style="[forceSolidModalStyle, { width: '560px', maxWidth: '92vw' }]" class="solid-modal-card force-solid-modal">
      <div class="form-group">
        <label class="form-label">已绑定的值（{{ aliasTarget?.Dimension }} 维度）</label>
        <div class="alias-chips">
          <span v-for="v in aliasValues" :key="v" class="alias-chip" :class="{ 'is-primary': v === aliasTarget?.MatchValue }">
            {{ v }}
            <span v-if="v === aliasTarget?.MatchValue" class="alias-primary-tag">主</span>
            <button v-else class="alias-chip-remove" title="移除" @click="removeAlias(v)">&times;</button>
          </span>
        </div>
        <div class="setting-desc" style="margin-top: 6px">将簡繁/译名等同一实体的不同写法合并到此库；主值不可移除。</div>
      </div>
      <div class="form-group">
        <label class="form-label">查找并合并更多值</label>
        <div style="display: flex; gap: 8px; align-items: center">
          <n-input v-model:value="aliasSearch" placeholder="搜索同维度的值（可选）" size="small" style="flex: 1" @keydown.enter.prevent="runAliasDiscover" />
          <n-button secondary size="small" :loading="aliasLoading" @click="runAliasDiscover">扫描</n-button>
        </div>
        <div v-if="aliasResults.length > 0" style="margin-top: 10px">
          <n-checkbox-group v-model:value="aliasSelected">
            <div class="discover-grid">
              <n-checkbox v-for="d in aliasResults" :key="d.Value" :value="d.Value">
                {{ d.Value }} <span class="platform-count">{{ d.Count }}</span>
                <span v-if="d.AlreadyAdded" style="color: var(--n-text-color-disabled); font-size: 11px">(其他库已用)</span>
              </n-checkbox>
            </div>
          </n-checkbox-group>
          <div style="margin-top: 8px">
            <n-button type="primary" size="small" :disabled="aliasSelected.length === 0" @click="addAliasSelected">
              合并所选 ({{ aliasSelected.length }})
            </n-button>
          </div>
        </div>
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAlias = false">关闭</n-button>
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

.lib-card-wrapper {
  position: relative;
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.lib-card-wrapper-dragging {
  opacity: 0.58;
  transform: scale(0.98);
}
.lib-card-wrapper-over::after {
  content: "";
  position: absolute;
  inset: -6px;
  z-index: 40;
  pointer-events: none;
  border: 1px solid var(--app-primary, #10b981);
  border-radius: 12px;
  background: color-mix(in srgb, var(--app-primary, #10b981) 12%, transparent);
}
.lib-drag-handle {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 45;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border: 1px solid rgba(255,255,255,0.16);
  border-radius: 4px;
  background: rgba(0,0,0,0.58);
  color: rgba(255,255,255,0.84);
  line-height: 1;
  cursor: grab;
  opacity: 0.72;
  box-shadow: 0 6px 16px rgba(0,0,0,0.28);
  transition: opacity 0.16s ease, background-color 0.16s ease, border-color 0.16s ease, transform 0.16s ease;
}
.lib-drag-handle:hover,
.lib-drag-handle:focus-visible {
  opacity: 1;
  background: rgba(0,0,0,0.74);
  border-color: rgba(255,255,255,0.3);
}
.lib-drag-handle:focus-visible {
  outline: 2px solid var(--app-primary, #10b981);
  outline-offset: 2px;
}
.lib-drag-handle:active {
  cursor: grabbing;
  transform: scale(0.96);
}
.lib-drag-handle:disabled {
  cursor: default;
  opacity: 0.36;
}

@media (prefers-reduced-motion: reduce) {
  .lib-card-wrapper,
  .lib-drag-handle {
    transition: none;
  }
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
.order-row { display: flex; align-items: center; gap: 8px; padding: 10px 8px; border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.04)); border-radius: 8px; transition: background-color 0.16s ease, opacity 0.16s ease; }
.order-row:last-child { border-bottom: none; }
.order-row-dragging { opacity: 0.55; }
.order-row-over { background: color-mix(in srgb, var(--app-primary, #10b981) 12%, transparent); box-shadow: inset 0 0 0 1px var(--app-primary, #10b981); }
.order-drag-handle {
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; flex-shrink: 0;
  border: 1px solid var(--app-border, rgba(255,255,255,0.16));
  border-radius: 6px; background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04));
  color: var(--app-text-muted); cursor: grab; line-height: 1;
  transition: opacity 0.16s ease, border-color 0.16s ease, background-color 0.16s ease;
}
.order-drag-handle:hover, .order-drag-handle:focus-visible { color: var(--app-text); border-color: rgba(var(--app-primary-rgb), 0.4); }
.order-drag-handle:focus-visible { outline: 2px solid var(--app-primary, #10b981); outline-offset: 2px; }
.order-drag-handle:active { cursor: grabbing; }
.order-drag-handle:disabled { cursor: default; opacity: 0.4; }
@media (prefers-reduced-motion: reduce) { .order-row, .order-drag-handle { transition: none; } }
.order-name { flex: 1; min-width: 0; font-size: 14px; color: var(--app-text); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.order-kind-badge { font-size: 10px; border-radius: 4px; padding: 1px 6px; flex-shrink: 0; }
.order-kind-badge.is-library { color: var(--app-primary); border: 1px solid rgba(var(--app-primary-rgb), 0.4); }
.order-kind-badge.is-platform { color: var(--app-text-muted); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); }

.alias-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.alias-chip { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 6px; font-size: 13px; color: var(--app-text); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04)); }
.alias-chip.is-primary { border-color: rgba(var(--app-primary-rgb), 0.5); }
.alias-primary-tag { font-size: 10px; color: var(--app-primary); }
.alias-chip-remove { border: 0; background: transparent; color: var(--app-text-muted); cursor: pointer; font-size: 15px; line-height: 1; padding: 0; }
.alias-chip-remove:hover { color: #d03050; }

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
