<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  NButton, NCheckbox, NInput, NInputNumber, NSelect, NModal, NSpace, NIcon, NSpin, NTag, NScrollbar,
} from 'naive-ui'
import { FolderOutline, CloudUploadOutline, TrashOutline, RefreshOutline, LinkOutline, LayersOutline, ArrowUpOutline, ArrowDownOutline, CloseOutline, SparklesOutline, ReorderFourOutline } from '@vicons/ionicons5'
import {
  getLibraryDetail, updateLibraryInfo, deleteLibraryById,
  addLibraryPath, removeLibraryPath, refreshSingleLibrary,
  uploadLibraryImage, setLibraryImageUrl, deleteLibraryImage, browseDirectories,
  getScrapeDefaults, getLibraryScrapeConfig, updateLibraryScrapeConfig,
  listCoverStyles, generateLibraryCover,
  type FieldPriorityMap, type ScrapeConfigOverride, type CoverStyle,
} from '../api/client'
import { useToast } from '../composables/useToast'
import {
  buildOrderedProviders,
  buildProviderPriorityMap,
  defaultScrapeProviders,
  getScrapeFieldLabel,
  getScrapeProviderLabel,
  normalizeProviderList,
  scrapeProviderMeta,
} from '../utils/scrapeConfigUi'

const props = defineProps<{
  libraryId: string | null
}>()

const emit = defineEmits<{
  close: []
  deleted: []
  updated: []
}>()

const { showToast } = useToast()

const library = ref<any>(null)
const loading = ref(true)
const name = ref('')
const collectionType = ref('movies')
const saving = ref(false)
const scanning = ref(false)

const newPath = ref('')
const addingPath = ref(false)
const showBrowser = ref(false)
const browserPath = ref('/mnt')
const browserDirs = ref<{ Name: string; Path: string }[]>([])
const browserLoading = ref(false)
const uploadingImage = ref(false)
const imageTag = ref<string | null>(null)
const showDeleteConfirm = ref(false)
const deleting = ref(false)
const coverKey = ref(0)
const coverUrlInput = ref('')
const settingUrlImage = ref(false)
const showUrlInput = ref(false)
const coverStyles = ref<CoverStyle[]>([])
const coverStylesLoaded = ref(false)
const generatingCover = ref(false)
const showStylePicker = ref(false)
const showcaseIcon = ref('auto')
const showcaseShowPosterTitles = ref(true)
const showcaseShowCount = ref(true)

const typeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
  { label: '混合', value: 'mixed' },
]
const typeLabels: Record<string, string> = { movies: '电影', tvshows: '电视剧', mixed: '混合' }
const typeEmoji: Record<string, string> = { movies: '🎬', tvshows: '📺', mixed: '🗂️' }
const typeTagTheme: Record<string, 'default' | 'error' | 'primary' | 'info' | 'success' | 'warning'> = {
  movies: 'info',
  tvshows: 'success',
  mixed: 'warning',
}
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
const hasShowcaseStyle = computed(() => coverStyles.value.some(s => s.name === 'showcase'))
const solidModalMenuProps = { class: 'solid-modal-menu' }
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

const visible = ref(false)

watch(() => props.libraryId, (id) => {
  if (id) {
    visible.value = true
    loadLibrary(id)
  } else {
    visible.value = false
  }
}, { immediate: true })

function handleClose() {
  visible.value = false
  emit('close')
}

function coverUrl() {
  const tag = imageTag.value || library.value?.ImageTag
  if (!tag || !props.libraryId) return ''
  return `/Items/${props.libraryId}/Images/Primary?tag=${tag}&v=${coverKey.value}`
}

async function loadLibrary(id?: string) {
  const libId = id || props.libraryId
  if (!libId) return
  loading.value = true
  try {
    const lib = await getLibraryDetail(libId)
    library.value = lib
    name.value = lib.Name
    collectionType.value = lib.CollectionType
    imageTag.value = lib.ImageTag || null
    // 并行加载刮削配置(不阻塞主 UI,失败静默)
    void loadScrapeConfig(libId)
  } catch {
    showToast('加载媒体库信息失败', 'error')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  if (!props.libraryId || !name.value.trim()) return
  saving.value = true
  try {
    await updateLibraryInfo(props.libraryId, { Name: name.value.trim(), CollectionType: collectionType.value })
    showToast('媒体库设置已保存', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('保存失败', 'error')
  } finally {
    saving.value = false
  }
}

async function handleAddPath() {
  if (!props.libraryId || !newPath.value.trim()) return
  addingPath.value = true
  try {
    await addLibraryPath(props.libraryId, newPath.value.trim())
    newPath.value = ''
    showToast('文件夹已添加', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('添加文件夹失败', 'error')
  } finally {
    addingPath.value = false
  }
}

async function handleRemovePath(pathToRemove: string) {
  if (!props.libraryId) return
  if (library.value?.Locations?.length <= 1) {
    showToast('至少需要保留一个文件夹', 'error')
    return
  }
  try {
    await removeLibraryPath(props.libraryId, pathToRemove)
    showToast('文件夹已移除', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('移除文件夹失败', 'error')
  }
}

async function handleScan() {
  if (!props.libraryId) return
  scanning.value = true
  try {
    await refreshSingleLibrary(props.libraryId)
    showToast('媒体库扫描已开始', 'success')
  } catch {
    showToast('启动扫描失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function handleDelete() {
  if (!props.libraryId || deleting.value) return
  deleting.value = true
  try {
    await deleteLibraryById(props.libraryId)
    // 后端只做了 soft delete,items 物理删除在后台跑;完成后由
    // cleanup task 的 SSE snapshot 触发另一条 toast(见 LibrariesPage)。
    showToast('媒体库正在后台清理,完成后会通知', 'info')
    showDeleteConfirm.value = false
    emit('deleted')
  } catch {
    showToast('删除媒体库失败', 'error')
  } finally {
    deleting.value = false
  }
}

async function loadBrowserDir(path: string) {
  browserLoading.value = true
  try {
    const res = await browseDirectories(path)
    browserPath.value = res.Path
    browserDirs.value = res.Directories || []
  } catch {
    showToast('无法读取目录', 'error')
  } finally {
    browserLoading.value = false
  }
}

function openBrowser() {
  showBrowser.value = true
  loadBrowserDir('/mnt')
}
function selectBrowserPath() {
  newPath.value = browserPath.value
  showBrowser.value = false
}

function parentDir(): string {
  const p = browserPath.value
  if (p === '/') return '/'
  const idx = p.lastIndexOf('/')
  return idx <= 0 ? '/' : p.substring(0, idx) || '/'
}

async function onCoverChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !props.libraryId) return
  uploadingImage.value = true
  try {
    const res = await uploadLibraryImage(props.libraryId, file)
    imageTag.value = res.ImageTag
    coverKey.value++
    showToast('封面已上传', 'success')
    emit('updated')
  } catch {
    showToast('上传失败', 'error')
  } finally {
    uploadingImage.value = false
    input.value = ''
  }
}

async function onDeleteCover() {
  if (!props.libraryId) return
  try {
    await deleteLibraryImage(props.libraryId)
    imageTag.value = null
    if (library.value) library.value.ImageTag = null
    showToast('封面已删除', 'success')
    emit('updated')
  } catch {
    showToast('删除封面失败', 'error')
  }
}

async function onSetCoverUrl() {
  const url = coverUrlInput.value.trim()
  if (!url || !props.libraryId) return
  settingUrlImage.value = true
  try {
    const res = await setLibraryImageUrl(props.libraryId, url) as { ImageTag: string }
    imageTag.value = res.ImageTag
    coverKey.value++
    coverUrlInput.value = ''
    showUrlInput.value = false
    showToast('封面已设置', 'success')
    emit('updated')
  } catch {
    showToast('从 URL 获取封面失败', 'error')
  } finally {
    settingUrlImage.value = false
  }
}

async function ensureCoverStylesLoaded() {
  if (coverStylesLoaded.value) return
  try {
    coverStyles.value = await listCoverStyles()
  } catch {
    // 静默,后续点击生成时再提示
  } finally {
    coverStylesLoaded.value = true
  }
}

async function onClickGenerate() {
  await ensureCoverStylesLoaded()
  if (coverStyles.value.length === 0) {
    showToast('暂无可用的封面风格', 'error')
    return
  }
  if (coverStyles.value.length === 1) {
    await onGenerateCover(coverStyles.value[0].name)
    return
  }
  showStylePicker.value = !showStylePicker.value
}

async function onGenerateCover(style: string) {
  if (!props.libraryId || generatingCover.value) return
  generatingCover.value = true
  showStylePicker.value = false
  try {
    const res = await generateLibraryCover(props.libraryId, style, coverOptionsForStyle(style))
    imageTag.value = res.ImageTag
    coverKey.value++
    showToast('封面已生成', 'success')
    emit('updated')
  } catch (e: any) {
    const msg = String(e?.message || '')
    if (msg.includes('422')) showToast('媒体库暂无可用海报素材,请先扫描入库', 'error')
    else if (msg.includes('409')) showToast('已有生成任务进行中,请稍候', 'info')
    else if (msg.includes('424')) showToast('字体资源缺失,请参见 internal/services/coverart/assets/fonts/ 下的 README', 'error')
    else showToast('封面生成失败', 'error')
  } finally {
    generatingCover.value = false
  }
}

// ===== 元数据源(Phase 6) =====

const defaultProviders = defaultScrapeProviders
const providerLabel = getScrapeProviderLabel
const fieldLabel = getScrapeFieldLabel

const scrapeMode = ref<'inherit' | 'custom'>('inherit')
const savingScrapeCfg = ref(false)
const scrapeDefaults = ref<{ providers: string[]; field_names: string[]; default_policy: FieldPriorityMap } | null>(null)
const scrapeEffective = ref<Record<string, any>>({})

const enableProvidersOn = ref(false)
const enablePriorityOn = ref(false)
const enableThresholdOn = ref(false)
const enableAutoApplyOn = ref(false)
const enableAdultFilterOn = ref(false)

const override = reactive<{
  providersEnabled: string[]
  confidenceThreshold: number
  autoApply: boolean
  adultContentFilterEnabled: boolean
  fieldPriority: FieldPriorityMap
}>({
  providersEnabled: [],
  confidenceThreshold: 0.72,
  autoApply: true,
  adultContentFilterEnabled: true,
  fieldPriority: {},
})
const providerOrder = ref<string[]>([...defaultProviders])
const draggingIndex = ref<number | null>(null)
const dragOverIndex = ref<number | null>(null)

const fieldDragging = ref<{ field: string; index: number } | null>(null)
const fieldDragOver = ref<{ field: string; index: number } | null>(null)
const fieldToAdd = ref<string | null>(null)

const effectiveProvidersEnabled = computed(() => {
  const fallback = normalizeProviderList(scrapeEffective.value.ProvidersEnabled, scrapeDefaults.value?.providers || defaultProviders)
  return enableProvidersOn.value
    ? normalizeProviderList(override.providersEnabled, [])
    : fallback
})

const activeProviderOrder = computed(() => {
  const basis = providerOrder.value.length > 0 ? providerOrder.value : (scrapeDefaults.value?.providers || defaultProviders)
  return basis.filter((name) => effectiveProvidersEnabled.value.includes(name))
})

const effectivePreviewSummary = computed(() => {
  const enabled = activeProviderOrder.value.map(providerLabel).join(' / ') || '(无)'
  const threshold = scrapeEffective.value.ConfidenceThreshold ?? '-'
  const autoApply = scrapeEffective.value.AutoApply === false ? '关闭' : '开启'
  const adultFilter = scrapeEffective.value.AdultContentFilterEnabled === false ? '关闭' : '开启'
  return `当前生效: 启用源 ${enabled}; 阈值 ${threshold}; 自动采纳 ${autoApply}; 成人内容过滤 ${adultFilter}`
})

const availableFieldsToAdd = computed(() => {
  if (!scrapeDefaults.value) return []
  const used = new Set(Object.keys(override.fieldPriority))
  return scrapeDefaults.value.field_names
    .filter((f) => !used.has(f))
    .map((f) => ({ label: fieldLabel(f), value: f }))
})

function isProviderEnabled(name: string) {
  return effectiveProvidersEnabled.value.includes(name)
}

function toggleProvider(name: string) {
  if (!enableProvidersOn.value) return
  if (override.providersEnabled.includes(name)) {
    override.providersEnabled = override.providersEnabled.filter((n) => n !== name)
  } else {
    override.providersEnabled = [...override.providersEnabled, name]
  }
}

function coverOptionsForStyle(style: string) {
  if (style !== 'showcase') return undefined
  const options: Record<string, any> = {
    ShowPosterTitles: showcaseShowPosterTitles.value,
    ShowCount: showcaseShowCount.value,
  }
  if (showcaseIcon.value !== 'auto') {
    options.Icon = showcaseIcon.value
  }
  return options
}

function onDragStart(index: number, e: DragEvent) {
  if (!enablePriorityOn.value) return
  draggingIndex.value = index
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(index))
  }
}

function onDragOver(index: number, e: DragEvent) {
  if (!enablePriorityOn.value || draggingIndex.value === null) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (dragOverIndex.value !== index) dragOverIndex.value = index
}

function onDrop(index: number, e: DragEvent) {
  if (!enablePriorityOn.value) return
  e.preventDefault()
  const from = draggingIndex.value
  draggingIndex.value = null
  dragOverIndex.value = null
  if (from === null || from === index) return
  const next = [...providerOrder.value]
  const [moved] = next.splice(from, 1)
  next.splice(index, 0, moved)
  providerOrder.value = next
}

function onDragEnd() {
  draggingIndex.value = null
  dragOverIndex.value = null
}

function moveProvider(index: number, delta: number) {
  if (!enablePriorityOn.value) return
  const target = index + delta
  if (target < 0 || target >= providerOrder.value.length) return
  const next = [...providerOrder.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  providerOrder.value = next
}

function syncOverrideFieldPriority() {
  if (Object.keys(override.fieldPriority).length === 0) return
  const enabled = new Set(effectiveProvidersEnabled.value)
  const basis = activeProviderOrder.value
  const next: FieldPriorityMap = {}
  for (const [field, list] of Object.entries(override.fieldPriority)) {
    const kept = list.filter((p) => enabled.has(p))
    for (const p of basis) {
      if (!kept.includes(p)) kept.push(p)
    }
    if (kept.length > 0) next[field] = kept
  }
  override.fieldPriority = next
}

watch(
  () => [effectiveProvidersEnabled.value.join(','), activeProviderOrder.value.join(',')],
  () => { syncOverrideFieldPriority() },
)

function addFieldOverride() {
  const f = fieldToAdd.value
  if (!f || !scrapeDefaults.value) return
  const defaults = scrapeDefaults.value.default_policy[f] ?? []
  const basis = activeProviderOrder.value.length > 0 ? activeProviderOrder.value : defaults
  override.fieldPriority[f] = defaults.filter((p) => basis.includes(p))
  for (const p of basis) {
    if (!override.fieldPriority[f].includes(p)) override.fieldPriority[f].push(p)
  }
  fieldToAdd.value = null
}

function removeFieldOverride(f: string) {
  delete override.fieldPriority[f]
}

function moveOverrideField(f: string, idx: number, delta: number) {
  const cur = override.fieldPriority[f]
  if (!cur) return
  const t = idx + delta
  if (t < 0 || t >= cur.length) return
  ;[cur[idx], cur[t]] = [cur[t], cur[idx]]
}

function onFieldPillDragStart(field: string, index: number, e: DragEvent) {
  fieldDragging.value = { field, index }
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(index))
  }
}

function onFieldPillDragOver(field: string, index: number, e: DragEvent) {
  const d = fieldDragging.value
  if (!d || d.field !== field) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  const cur = fieldDragOver.value
  if (!cur || cur.field !== field || cur.index !== index) {
    fieldDragOver.value = { field, index }
  }
}

function onFieldPillDrop(field: string, index: number, e: DragEvent) {
  e.preventDefault()
  const from = fieldDragging.value
  fieldDragging.value = null
  fieldDragOver.value = null
  if (!from || from.field !== field || from.index === index) return
  const cur = [...(override.fieldPriority[field] ?? [])]
  const [moved] = cur.splice(from.index, 1)
  cur.splice(index, 0, moved)
  override.fieldPriority[field] = cur
}

function onFieldPillDragEnd() {
  fieldDragging.value = null
  fieldDragOver.value = null
}

function onFieldPillDragLeave(field: string, index: number) {
  const cur = fieldDragOver.value
  if (cur && cur.field === field && cur.index === index) {
    fieldDragOver.value = null
  }
}

async function loadScrapeConfig(id?: string) {
  const libId = id || props.libraryId
  if (!libId) return
  try {
    if (!scrapeDefaults.value) {
      scrapeDefaults.value = await getScrapeDefaults()
    }
    const resp = await getLibraryScrapeConfig(libId)
    scrapeEffective.value = resp.effective || {}

    const defaultOrder = scrapeDefaults.value?.providers || defaultProviders
    const effectiveEnabled = normalizeProviderList(resp.effective?.ProvidersEnabled, defaultOrder)
    const effectiveOrder = buildOrderedProviders(resp.effective?.ProviderPriority, defaultOrder, effectiveEnabled)
    providerOrder.value = [...effectiveOrder]

    if (resp.inherit || !resp.override) {
      scrapeMode.value = 'inherit'
      enableProvidersOn.value = false
      enablePriorityOn.value = false
      enableThresholdOn.value = false
      enableAutoApplyOn.value = false
      enableAdultFilterOn.value = false
      override.providersEnabled = [...effectiveEnabled]
      override.confidenceThreshold = resp.effective?.ConfidenceThreshold ?? 0.72
      override.autoApply = resp.effective?.AutoApply ?? true
      override.adultContentFilterEnabled = resp.effective?.AdultContentFilterEnabled ?? true
      override.fieldPriority = {}
      return
    }

    scrapeMode.value = 'custom'
    const ov = resp.override
    enableProvidersOn.value = Array.isArray(ov.providers_enabled)
    enablePriorityOn.value = !!(ov.provider_priority && Object.keys(ov.provider_priority).length > 0)
    enableThresholdOn.value = typeof ov.confidence_threshold === 'number'
    enableAutoApplyOn.value = typeof ov.auto_apply === 'boolean'
    enableAdultFilterOn.value = typeof ov.adult_content_filter_enabled === 'boolean'
    override.providersEnabled = Array.isArray(ov.providers_enabled)
      ? [...ov.providers_enabled]
      : [...effectiveEnabled]
    if (enablePriorityOn.value) {
      const overrideOrder = buildOrderedProviders(ov.provider_priority, defaultOrder, override.providersEnabled)
      providerOrder.value = [...overrideOrder]
    }
    override.confidenceThreshold = ov.confidence_threshold ?? resp.effective?.ConfidenceThreshold ?? 0.72
    override.autoApply = ov.auto_apply ?? resp.effective?.AutoApply ?? true
    override.adultContentFilterEnabled = ov.adult_content_filter_enabled ?? resp.effective?.AdultContentFilterEnabled ?? true
    override.fieldPriority = ov.field_priority ? { ...ov.field_priority } : {}
  } catch {
    // 读失败静默,保留上次值
  }
}

async function handleSaveScrapeCfg() {
  if (!props.libraryId) return
  savingScrapeCfg.value = true
  try {
    if (scrapeMode.value === 'inherit') {
      await updateLibraryScrapeConfig(props.libraryId, { inherit: true, override: null })
      showToast('已改为继承全局', 'success')
      await loadScrapeConfig()
      return
    }
    const ov: ScrapeConfigOverride = {}
    if (enableProvidersOn.value) ov.providers_enabled = [...override.providersEnabled]
    if (enablePriorityOn.value) ov.provider_priority = buildProviderPriorityMap(providerOrder.value)
    if (enableThresholdOn.value) ov.confidence_threshold = override.confidenceThreshold
    if (enableAutoApplyOn.value) ov.auto_apply = override.autoApply
    if (enableAdultFilterOn.value) ov.adult_content_filter_enabled = override.adultContentFilterEnabled
    const fieldPriorityEntries = Object.entries(override.fieldPriority)
      .filter(([, providers]) => Array.isArray(providers) && providers.length > 0)
    if (fieldPriorityEntries.length > 0) {
      ov.field_priority = Object.fromEntries(fieldPriorityEntries)
    }
    const isEmpty = !ov.providers_enabled &&
      !ov.provider_priority &&
      ov.confidence_threshold === undefined && ov.auto_apply === undefined &&
      ov.adult_content_filter_enabled === undefined &&
      !ov.field_priority
    if (isEmpty) {
      await updateLibraryScrapeConfig(props.libraryId, { inherit: true, override: null })
      showToast('无任何覆盖项,已改为继承全局', 'info')
    } else {
      await updateLibraryScrapeConfig(props.libraryId, { inherit: false, override: ov })
      showToast('元数据源配置已保存', 'success')
    }
    await loadScrapeConfig()
  } catch {
    showToast('保存失败', 'error')
  } finally {
    savingScrapeCfg.value = false
  }
}
</script>

<template>
  <n-modal
    :show="visible"
    preset="card"
    :title="library?.Name || '编辑媒体库'"
    :style="[forceSolidModalStyle, { width: '620px', maxWidth: '92vw' }]"
    class="solid-modal-card force-solid-modal"
    :mask-closable="true"
    @update:show="(v: boolean) => { if (!v) handleClose() }"
  >
    <!-- Loading -->
    <div v-if="loading || !library" style="padding: 40px; text-align: center">
      <n-spin size="medium" />
    </div>

    <template v-else>
      <!-- Banner: cover + info side by side -->
      <div class="em-banner">
        <div class="em-cover-wrap">
          <div class="em-cover-ratio">
            <img v-if="coverUrl()" :src="coverUrl()" alt="cover" class="em-cover-img" />
            <div v-else class="em-cover-placeholder">
              <span class="em-cover-emoji">{{ typeEmoji[library.CollectionType] || '🗂️' }}</span>
            </div>
          </div>
          <div class="em-cover-actions">
            <label class="em-cover-btn em-cover-btn-upload">
              <n-icon :size="12"><CloudUploadOutline /></n-icon>
              {{ uploadingImage ? '...' : '上传' }}
              <input type="file" accept="image/*" style="display: none" :disabled="uploadingImage" @change="onCoverChange" />
            </label>
            <button class="em-cover-btn em-cover-btn-link" @click="showUrlInput = !showUrlInput">
              <n-icon :size="12"><LinkOutline /></n-icon>
              链接
            </button>
            <button class="em-cover-btn em-cover-btn-gen" :disabled="generatingCover" @click="onClickGenerate">
              <n-icon :size="12"><SparklesOutline /></n-icon>
              {{ generatingCover ? '···' : '生成' }}
            </button>
            <button v-if="coverUrl()" class="em-cover-btn em-cover-btn-del" @click="onDeleteCover">
              <n-icon :size="12"><TrashOutline /></n-icon>
              删除
            </button>
          </div>
          <div v-if="showUrlInput" class="em-url-input">
            <n-input v-model:value="coverUrlInput" size="tiny" placeholder="输入图片 URL" :disabled="settingUrlImage" @keydown.enter.prevent="onSetCoverUrl" />
            <n-button size="tiny" type="primary" :loading="settingUrlImage" :disabled="!coverUrlInput.trim()" @click="onSetCoverUrl">确定</n-button>
          </div>
          <div v-if="showStylePicker && coverStyles.length > 1" class="em-style-picker">
            <div class="em-style-picker-title">选择风格</div>
            <button
              v-for="s in coverStyles"
              :key="s.name"
              class="em-style-opt"
              :disabled="generatingCover"
              @click="onGenerateCover(s.name)"
            >
              {{ s.label }}
            </button>
            <div v-if="hasShowcaseStyle" class="em-showcase-options">
              <div class="em-style-picker-title">横幅陈列选项</div>
              <n-select
                v-model:value="showcaseIcon"
                size="tiny"
                :options="showcaseIconOptions"
                :menu-props="solidModalMenuProps"
              />
              <div class="em-showcase-checks">
                <n-checkbox v-model:checked="showcaseShowPosterTitles" size="small">海报标题</n-checkbox>
                <n-checkbox v-model:checked="showcaseShowCount" size="small">数量</n-checkbox>
              </div>
            </div>
          </div>
        </div>

        <div class="em-banner-info">
          <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 10px">
            <n-tag :type="typeTagTheme[library.CollectionType] || 'default'" size="small" round :bordered="false">
              {{ typeLabels[library.CollectionType] || library.CollectionType }}
            </n-tag>
            <span class="em-item-count">{{ library.ItemCount || 0 }} 个项目</span>
          </div>
          <div class="em-fields">
            <div>
              <label class="em-label">媒体库名称</label>
              <n-input v-model:value="name" size="small" />
            </div>
            <div>
              <label class="em-label">内容类型</label>
              <n-select v-model:value="collectionType" :options="typeOptions" size="small" :menu-props="solidModalMenuProps" />
            </div>
          </div>
          <div style="margin-top: 12px">
            <n-button type="primary" size="small" :loading="saving" @click="handleSave">保存修改</n-button>
          </div>
        </div>
      </div>

      <!-- Folders -->
      <div class="em-section">
        <h4 class="em-section-title">
          <n-icon :size="15"><FolderOutline /></n-icon>
          媒体文件夹
        </h4>
        <div class="em-folder-list">
          <div v-for="(p, i) in library.Locations || []" :key="i" class="em-folder-item">
            <n-icon :size="15" style="color: var(--app-text-muted); flex-shrink: 0"><FolderOutline /></n-icon>
            <span class="em-folder-path">{{ p }}</span>
            <n-button text type="error" size="tiny" @click="handleRemovePath(p)" title="移除">×</n-button>
          </div>
        </div>
        <div class="em-add-path">
          <n-input v-model:value="newPath" placeholder="/mnt/media/movies" size="small" @keydown.enter.prevent="handleAddPath" />
          <n-button secondary size="small" :disabled="addingPath || !newPath.trim()" :loading="addingPath" @click="handleAddPath">添加</n-button>
          <n-button secondary size="small" @click="openBrowser">浏览</n-button>
        </div>
      </div>

      <!-- Scan -->
      <div class="em-section">
        <h4 class="em-section-title">
          <n-icon :size="15"><RefreshOutline /></n-icon>
          扫描
        </h4>
        <p class="em-section-desc">扫描此媒体库中所有文件夹的媒体文件。</p>
        <n-button type="primary" size="small" :loading="scanning" @click="handleScan">立即扫描</n-button>
      </div>

      <!-- Scrape config -->
      <div class="em-section">
        <h4 class="em-section-title">
          <n-icon :size="15"><LayersOutline /></n-icon>
          元数据源
        </h4>
        <p class="em-section-desc">本媒体库的刮削源 / 阈值 / 字段优先级。未覆盖的项继承全局。</p>

        <div class="em-scrape-mode">
          <label class="em-mode-opt" :class="{ active: scrapeMode === 'inherit' }">
            <input type="radio" v-model="scrapeMode" value="inherit" />
            <span>继承全局</span>
          </label>
          <label class="em-mode-opt" :class="{ active: scrapeMode === 'custom' }">
            <input type="radio" v-model="scrapeMode" value="custom" />
            <span>自定义</span>
          </label>
        </div>

        <div v-if="scrapeMode === 'inherit'" class="em-inherit-preview">
          <div class="hint-text">{{ effectivePreviewSummary }}</div>
        </div>

        <div v-else class="em-scrape-overrides">
          <div class="em-override-block">
            <div class="em-override-head em-override-head-split">
              <label class="em-toggle-opt">
                <input type="checkbox" v-model="enableProvidersOn" />
                <span>覆盖启用的源</span>
              </label>
              <label class="em-toggle-opt">
                <input type="checkbox" v-model="enablePriorityOn" />
                <span>覆盖源顺序</span>
              </label>
            </div>
            <div class="em-override-body em-override-body-column">
              <div class="em-provider-list">
                <div
                  v-for="(name, idx) in providerOrder"
                  :key="name"
                  class="em-provider-row"
                  :class="{
                    dragging: draggingIndex === idx,
                    'drag-over': dragOverIndex === idx && draggingIndex !== idx,
                    disabled: !isProviderEnabled(name),
                    'order-locked': !enablePriorityOn,
                  }"
                  :style="{ '--accent': scrapeProviderMeta[name]?.accent }"
                  :draggable="enablePriorityOn"
                  @dragstart="onDragStart(idx, $event)"
                  @dragover="onDragOver(idx, $event)"
                  @drop="onDrop(idx, $event)"
                  @dragend="onDragEnd"
                  @dragleave="dragOverIndex === idx ? (dragOverIndex = null) : null"
                >
                  <div class="em-provider-handle" :title="enablePriorityOn ? '拖拽调序' : '继承全局顺序'">
                    <n-icon><ReorderFourOutline /></n-icon>
                  </div>
                  <label class="em-provider-check" @click.stop>
                    <input type="checkbox" :checked="isProviderEnabled(name)" :disabled="!enableProvidersOn" @change="toggleProvider(name)" />
                  </label>
                  <div class="em-provider-info">
                    <div class="em-provider-name">
                      {{ scrapeProviderMeta[name]?.label || name }}
                      <span v-if="scrapeProviderMeta[name]?.badge" class="em-provider-badge">{{ scrapeProviderMeta[name]?.badge }}</span>
                    </div>
                    <div class="em-provider-desc">{{ scrapeProviderMeta[name]?.desc }}</div>
                  </div>
                  <span class="em-provider-index">{{ idx + 1 }}</span>
                  <div class="em-provider-move">
                    <n-button quaternary circle size="tiny" :disabled="!enablePriorityOn || idx === 0" @click.stop="moveProvider(idx, -1)">
                      <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
                    </n-button>
                    <n-button quaternary circle size="tiny" :disabled="!enablePriorityOn || idx === providerOrder.length - 1" @click.stop="moveProvider(idx, 1)">
                      <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
                    </n-button>
                  </div>
                </div>
              </div>
              <div class="hint-text">TVDB / Fanart / 豆瓣 Cookie 等凭据始终读全局，这里只覆盖启用状态与顺序。</div>
            </div>
          </div>

          <div class="em-override-block">
            <label class="em-override-head">
              <input type="checkbox" v-model="enableThresholdOn" />
              <span>覆盖识别阈值</span>
            </label>
            <div v-if="enableThresholdOn" class="em-override-body">
              <n-input-number v-model:value="override.confidenceThreshold" :min="0.5" :max="1" :step="0.01" size="small" />
            </div>
          </div>

          <div class="em-override-block">
            <label class="em-override-head">
              <input type="checkbox" v-model="enableAutoApplyOn" />
              <span>覆盖"自动采纳"</span>
            </label>
            <div v-if="enableAutoApplyOn" class="em-override-body">
              <n-checkbox v-model:checked="override.autoApply">低于阈值时自动采纳(否则进人工确认队列)</n-checkbox>
            </div>
          </div>

          <div class="em-override-block">
            <label class="em-override-head">
              <input type="checkbox" v-model="enableAdultFilterOn" />
              <span>覆盖"成人内容过滤"</span>
            </label>
            <div v-if="enableAdultFilterOn" class="em-override-body">
              <n-checkbox v-model:checked="override.adultContentFilterEnabled">拦截成人影视内容候选，识别失败时保持刮削前原样</n-checkbox>
            </div>
          </div>

          <div class="em-override-block">
            <div class="em-field-head">
              <span>字段来源(仅覆盖列出的字段,其余继承)</span>
              <n-select
                v-model:value="fieldToAdd"
                :options="availableFieldsToAdd"
                placeholder="+ 添加字段"
                size="tiny"
                :menu-props="solidModalMenuProps"
                style="width: 140px"
                @update:value="addFieldOverride"
              />
            </div>
            <div v-if="Object.keys(override.fieldPriority).length === 0" class="hint-text">暂未覆盖任何字段</div>
            <div v-for="f in Object.keys(override.fieldPriority)" :key="f" class="em-field-row">
              <div class="em-field-name">{{ fieldLabel(f) }}</div>
              <div class="em-field-pills">
                <div
                  v-for="(pname, idx) in override.fieldPriority[f]"
                  :key="pname"
                  class="em-field-pill"
                  :class="{
                    dragging: fieldDragging?.field === f && fieldDragging?.index === idx,
                    'drag-over': fieldDragOver?.field === f && fieldDragOver?.index === idx,
                  }"
                  :style="{ '--accent': scrapeProviderMeta[pname]?.accent }"
                  draggable="true"
                  @dragstart="onFieldPillDragStart(f, idx, $event)"
                  @dragover="onFieldPillDragOver(f, idx, $event)"
                  @drop="onFieldPillDrop(f, idx, $event)"
                  @dragend="onFieldPillDragEnd"
                  @dragleave="onFieldPillDragLeave(f, idx)"
                >
                  <span class="em-pill-handle">⋮⋮</span>
                  <span class="em-pill-idx">{{ idx + 1 }}</span>
                  <span>{{ providerLabel(pname) }}</span>
                  <n-button quaternary circle size="tiny" :disabled="idx === 0" @click="moveOverrideField(f, idx, -1)">
                    <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
                  </n-button>
                  <n-button quaternary circle size="tiny" :disabled="idx === override.fieldPriority[f].length - 1" @click="moveOverrideField(f, idx, 1)">
                    <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
                  </n-button>
                </div>
              </div>
              <n-button quaternary circle size="tiny" type="error" @click="removeFieldOverride(f)">
                <template #icon><n-icon><CloseOutline /></n-icon></template>
              </n-button>
            </div>
          </div>
        </div>

        <div style="margin-top: 12px">
          <n-button type="primary" size="small" :loading="savingScrapeCfg" @click="handleSaveScrapeCfg">保存元数据源配置</n-button>
        </div>
      </div>

      <!-- Danger -->
      <div class="em-section em-danger">
        <h4 class="em-section-title" style="color: var(--app-error)">
          <n-icon :size="15"><TrashOutline /></n-icon>
          危险操作
        </h4>
        <p class="em-section-desc">删除媒体库将移除所有关联的媒体信息（不会删除实际文件）。</p>
        <n-button type="error" ghost size="small" @click="showDeleteConfirm = true">删除此媒体库</n-button>
      </div>
    </template>

    <!-- Dir Browser Sub-Modal -->
    <n-modal v-model:show="showBrowser" preset="card" title="选择文件夹" :style="[forceSolidModalStyle, { maxWidth: '480px', maxHeight: '70vh' }]" class="solid-modal-card force-solid-modal">
      <div class="em-dir-current">{{ browserPath }}</div>
      <n-scrollbar style="max-height: min(350px, 45vh)">
        <div v-if="browserPath !== '/'" class="em-dir-row" @click="loadBrowserDir(parentDir())">← 上一级</div>
        <div v-if="browserLoading" style="padding: 20px; text-align: center; color: var(--app-text-muted)"><n-spin size="small" /></div>
        <div v-else-if="browserDirs.length === 0" style="padding: 20px; text-align: center; color: var(--app-text-muted)">没有子目录</div>
        <div v-else v-for="d in browserDirs" :key="d.Path" class="em-dir-row" @click="loadBrowserDir(d.Path)">
          <n-icon :size="16"><FolderOutline /></n-icon> {{ d.Name }}
        </div>
      </n-scrollbar>
      <template #action>
        <n-space justify="end">
          <n-button @click="showBrowser = false">取消</n-button>
          <n-button type="primary" @click="selectBrowserPath">选择当前目录</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Delete Confirm Sub-Modal -->
    <n-modal
      v-model:show="showDeleteConfirm"
      preset="dialog"
      type="error"
      title="删除媒体库"
      :positive-text="deleting ? '删除中…' : '删除'"
      negative-text="取消"
      :loading="deleting"
      :positive-button-props="{ disabled: deleting, loading: deleting }"
      :mask-closable="!deleting"
      :close-on-esc="!deleting"
      @positive-click="handleDelete"
    >
      <p style="color: var(--app-text-muted); font-size: 14px">
        确定要删除媒体库「<strong style="color: var(--app-text)">{{ library?.Name }}</strong>」吗？此操作不可撤销。
      </p>
      <p style="color: var(--app-text-muted); font-size: 12px; margin-top: 8px">
        删除后会立即从列表消失,关联媒体项由后台分批清理,完成时会在右上角通知。
      </p>
    </n-modal>
  </n-modal>
</template>

<style scoped>
.em-banner {
  display: flex;
  gap: 18px;
  margin-bottom: 16px;
}

.em-cover-wrap {
  flex-shrink: 0;
  width: 200px;
}

.em-cover-ratio {
  position: relative;
  width: 100%;
  padding-bottom: 56.25%; /* 16:9 */
  border-radius: 8px;
  overflow: hidden;
  background: linear-gradient(135deg, #1a1a2e 0%, #1e293b 40%, #334155 100%);
}

.em-cover-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.em-cover-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.em-cover-emoji {
  font-size: 28px;
  opacity: 0.35;
}

.em-cover-actions {
  display: flex;
  flex-wrap: nowrap;
  gap: 4px;
  margin-top: 8px;
}
.em-cover-btn {
  flex: 1 1 0;
  min-width: 0;
  justify-content: center;
  display: inline-flex; align-items: center; gap: 3px;
  padding: 3px 4px;
  font-size: 11px; font-weight: 500;
  border-radius: 5px;
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.08));
  border: 1px solid var(--app-border);
  color: var(--app-text-muted);
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease, transform 0.18s ease, box-shadow 0.18s ease;
}
.em-cover-btn:hover:not(:disabled) { transform: translateY(-1px); }
.em-cover-btn:disabled { opacity: 0.55; cursor: not-allowed; transform: none; box-shadow: none; }

/* 上传 — 蓝色 */
.em-cover-btn-upload {
  border-color: rgba(59, 130, 246, 0.35);
  color: #60a5fa;
}
.em-cover-btn-upload:hover {
  background: rgba(59, 130, 246, 0.18);
  border-color: rgba(59, 130, 246, 0.65);
  color: #93c5fd;
}

/* 链接 — 青色 */
.em-cover-btn-link {
  border-color: rgba(20, 184, 166, 0.35);
  color: #2dd4bf;
}
.em-cover-btn-link:hover {
  background: rgba(20, 184, 166, 0.18);
  border-color: rgba(20, 184, 166, 0.65);
  color: #5eead4;
}

/* 自动生成 — 紫色,作为新功能默认稍亮一点 */
.em-cover-btn-gen {
  background: rgba(139, 92, 246, 0.14);
  border-color: rgba(139, 92, 246, 0.5);
  color: #c4b5fd;
}
.em-cover-btn-gen:hover:not(:disabled) {
  background: rgba(139, 92, 246, 0.26);
  border-color: rgba(139, 92, 246, 0.85);
  color: #ede9fe;
  box-shadow: 0 2px 10px -3px rgba(139, 92, 246, 0.5);
}

/* 删除 — 默认克制灰,hover 才变红 */
.em-cover-btn-del {
  border-color: rgba(239, 68, 68, 0.28);
  color: rgba(239, 68, 68, 0.85);
}
.em-cover-btn-del:hover {
  background: rgba(239, 68, 68, 0.16);
  border-color: rgba(239, 68, 68, 0.6);
  color: #f87171;
}

.em-style-picker {
  margin-top: 6px;
  padding: 8px 10px;
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.06));
  border: 1px solid var(--app-border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.em-style-picker-title {
  font-size: 11px;
  color: var(--app-text-muted);
  margin-bottom: 2px;
}
.em-style-opt {
  display: block;
  text-align: left;
  padding: 4px 8px;
  font-size: 12px;
  border-radius: 4px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--app-text);
  cursor: pointer;
  transition: all 0.15s;
}
.em-style-opt:hover { border-color: var(--app-primary); color: var(--app-primary); }
.em-style-opt:disabled { opacity: 0.55; cursor: not-allowed; }

.em-showcase-options {
  margin-top: 4px;
  padding-top: 8px;
  border-top: 1px solid var(--app-border);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.em-showcase-checks {
  display: flex;
  gap: 12px;
  align-items: center;
}

.em-url-input {
  display: flex;
  gap: 4px;
  margin-top: 6px;
}

.em-banner-info {
  flex: 1;
  min-width: 0;
}
.em-item-count { font-size: 13px; color: var(--app-text-muted); }

.em-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.em-label {
  display: block; font-size: 11px; color: var(--app-text-muted);
  margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500;
}

.em-section {
  background: var(--app-modal-panel-bg, rgba(128,128,128,0.04));
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 16px 18px;
  margin-bottom: 12px;
}
.em-danger { border-color: rgba(239,68,68,0.2); }

.em-section-title {
  font-size: 13px; font-weight: 600; color: var(--app-text);
  margin: 0 0 10px; padding-bottom: 8px;
  border-bottom: 1px solid var(--app-border);
  display: flex; align-items: center; gap: 6px;
}

.em-section-desc {
  font-size: 12px; color: var(--app-text-muted); margin: 0 0 10px;
}

.em-folder-list { margin-bottom: 10px; }
.em-folder-item {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.04));
  border-radius: 6px;
  margin-bottom: 4px;
}
.em-folder-path {
  flex: 1; font-size: 12px; color: var(--app-text);
  word-break: break-all; font-family: 'SF Mono', 'Fira Code', monospace;
}

.em-add-path { display: flex; gap: 6px; align-items: stretch; }

.em-dir-current {
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.06)); padding: 8px 12px;
  border-radius: 6px; margin-bottom: 10px;
  font-size: 12px; color: var(--app-primary);
  word-break: break-all; font-family: monospace;
}
.em-dir-row {
  display: flex; align-items: center; gap: 8px; padding: 6px 10px;
  cursor: pointer; border-radius: 4px; font-size: 13px; color: var(--app-text);
  transition: background 0.15s;
}
.em-dir-row:hover { background: var(--app-modal-hover-bg, rgba(128,128,128,0.08)); }

/* ===== 元数据源 section ===== */
.em-scrape-mode {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}
.em-mode-opt {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 6px;
  border: 1px solid var(--app-border);
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.04));
  font-size: 12px;
  color: var(--app-text);
  cursor: pointer;
}
.em-mode-opt.active {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb, 59,130,246), 0.1);
  color: var(--app-primary);
}
.em-mode-opt input {
  accent-color: var(--app-primary);
}

.em-inherit-preview {
  padding: 8px 10px;
  background: rgba(128,128,128,0.03);
  border-radius: 4px;
}

.em-scrape-overrides {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.em-override-block {
  padding: 8px 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
}
.em-override-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-text);
  font-weight: 500;
  cursor: pointer;
}
.em-override-head input {
  accent-color: var(--app-primary);
}
.em-override-head-split {
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.em-toggle-opt {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.em-override-body {
  padding-top: 8px;
  padding-left: 20px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.em-override-body-column {
  flex-direction: column;
  align-items: stretch;
  padding-left: 0;
}
.em-provider-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}
.em-provider-row {
  display: grid;
  grid-template-columns: 22px 22px 1fr auto 40px;
  gap: 8px;
  align-items: center;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid transparent;
  background: rgba(255,255,255,0.02);
  transition: background 0.15s, border-color 0.15s, opacity 0.15s;
}
.em-provider-row.dragging {
  opacity: 0.4;
}
.em-provider-row.drag-over {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb, 59,130,246), 0.08);
}
.em-provider-row.disabled .em-provider-info {
  opacity: 0.45;
}
.em-provider-row.order-locked .em-provider-handle {
  cursor: default;
  opacity: 0.28;
}
.em-provider-handle {
  color: var(--app-text-muted);
  opacity: 0.55;
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}
.em-provider-row:hover .em-provider-handle {
  opacity: 0.9;
}
.em-provider-check {
  display: flex;
  align-items: center;
  justify-content: center;
}
.em-provider-check input {
  accent-color: var(--accent, var(--app-primary));
  cursor: pointer;
}
.em-provider-info {
  min-width: 0;
}
.em-provider-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
}
.em-provider-desc {
  font-size: 11px;
  color: var(--app-text-muted);
  margin-top: 2px;
}
.em-provider-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent, var(--app-primary)) 18%, transparent);
  color: var(--accent, var(--app-primary));
}
.em-provider-index {
  font-variant-numeric: tabular-nums;
  font-size: 11px;
  font-weight: 600;
  color: var(--app-text-muted);
  opacity: 0.65;
  min-width: 18px;
  text-align: right;
}
.em-provider-move {
  display: flex;
}
.em-field-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: var(--app-text);
  font-weight: 500;
  padding-bottom: 8px;
}
.em-field-row {
  display: grid;
  grid-template-columns: 80px 1fr auto;
  gap: 8px;
  align-items: center;
  padding: 6px 0;
  border-top: 1px dashed var(--app-border);
}
.em-field-row:first-of-type { border-top: none; }
.em-field-name {
  font-size: 12px;
  color: var(--app-text-muted);
}
.em-field-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.em-field-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 4px 2px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent, #888) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent, #888) 30%, transparent);
  font-size: 11px;
  color: var(--app-text);
  cursor: grab;
  transition: background 0.15s, border-color 0.15s, opacity 0.15s, transform 0.15s, box-shadow 0.15s;
}
.em-field-pill:hover {
  background: color-mix(in srgb, var(--accent, #888) 22%, transparent);
  border-color: color-mix(in srgb, var(--accent, #888) 50%, transparent);
}
.em-field-pill.dragging {
  opacity: 0.4;
}
.em-field-pill.drag-over {
  transform: translateX(3px);
  border-color: color-mix(in srgb, var(--accent, var(--app-primary)) 75%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent, var(--app-primary)) 40%, transparent);
}
.em-pill-handle {
  color: var(--app-text-muted);
  opacity: 0.55;
  font-size: 11px;
}
.em-pill-idx {
  font-variant-numeric: tabular-nums;
  color: var(--app-text-muted);
  font-weight: 600;
  opacity: 0.7;
}
.hint-text {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.5;
  margin-top: 4px;
  opacity: 0.8;
}

@media (max-width: 500px) {
  .em-banner { flex-direction: column; }
  .em-cover-wrap { width: 100%; }
  .em-fields { grid-template-columns: 1fr !important; }
  .em-field-row { grid-template-columns: 1fr; }
  .em-provider-row { grid-template-columns: 22px 22px 1fr auto; }
  .em-provider-index { display: none; }
}
</style>
