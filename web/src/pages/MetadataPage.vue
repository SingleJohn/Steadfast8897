<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import { NButton, NCard, NEmpty, NForm, NFormItem, NGrid, NGridItem, NIcon, NInput, NInputNumber, NProgress, NSelect, NSlider, NSwitch } from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { SearchOutline, VideocamOutline, AddOutline, CloseOutline, ArrowForwardOutline, LayersOutline, ArrowUpOutline, ArrowDownOutline, ReorderFourOutline, CheckmarkCircle, EllipseOutline } from '@vicons/ionicons5'
import {
  getSystemConfig, updateSystemConfig,
  scrapeAllMetadata, getScrapeProgress,
  startProbe, stopProbe,
  startBackfill, stopBackfill,
  getBackfillConfig, updateBackfillConfig,
  resetBackfillQuality, resetBackfillEpisodeImage,
  getScrapeDefaults,
  type BackfillStage, type FieldPriorityMap,
} from '@/api/client'
import { useTaskStream } from '@/composables/useTaskStream'
import {
  buildOrderedProviders,
  buildProviderPriorityMap,
  defaultScrapeProviders,
  getScrapeFieldLabel,
  getScrapeProviderLabel,
  scrapeFieldLabels,
  scrapeProviderMeta,
} from '@/utils/scrapeConfigUi'

const { showToast } = useToast()
const router = useRouter()

const tmdbLanguageOptions = [
  { label: 'zh-CN (简体中文)', value: 'zh-CN' },
  { label: 'en-US (English)', value: 'en-US' },
  { label: 'ja-JP (日本语)', value: 'ja-JP' },
]
const scrapeSaveModeOptions = [
  { label: '数据库（图片存服务器 data/）', value: 'database' },
  { label: '媒体目录（NFO+图片写到文件夹）', value: 'media_dir' },
  { label: '两者都写', value: 'both' },
]
const probeThreadsOptions = [1, 2, 3, 5, 8, 10, 15, 20].map((n) => ({ label: String(n), value: String(n) }))

// ===== 基础设置(全局) =====
const tmdbLanguage = ref('zh-CN')
const tmdbProxy = ref('')
const scrapeSaveMode = ref('database')
const autoScrape = ref(false)
const confidenceThreshold = ref<number>(0.72)
const autoApplyEnabled = ref(true)
const adultContentFilterEnabled = ref(true)
const savingConfig = ref(false)
const scraping = ref(false)

// 任务进度改由 SSE 流驱动
const { snapshots } = useTaskStream()

// "待刮削 N / 总 M" 提示,点入队按钮后手动刷新
const scrapeSummary = ref<{ missing_count: number; items_total: number } | null>(null)

async function refreshScrapeSummary() {
  try {
    const p: any = await getScrapeProgress()
    scrapeSummary.value = {
      missing_count: Number(p?.missing_count ?? 0),
      items_total: Number(p?.items_total ?? 0),
    }
  } catch {
    scrapeSummary.value = null
  }
}

// ===== 刮削源 =====
// providerMeta: sidebar 每行展示 + detail 区域表单分支所需的元信息
const providerMeta = scrapeProviderMeta
const defaultProviders = defaultScrapeProviders
const providerLabel = getScrapeProviderLabel

// providerOrder = 显示+优先级顺序; providersEnabled = 勾选启用集合
// 二者合并呈现为一个可拖拽 + checkbox 的 sidebar 列表。
const providerOrder = ref<string[]>([...defaultProviders])
const providersEnabled = ref<string[]>([...defaultProviders])
const selectedProvider = ref<string>('tmdb')

// 凭据(全局)
const tmdbApiKeys = ref<string[]>([''])
const showApiKey = ref(false)
const tvdbApiKey = ref('')
const tvdbPin = ref('')
const bangumiUA = ref('')
const doubanCookie = ref('')
const fanartApiKey = ref('')
const savingScrapeSources = ref(false)

function isProviderEnabled(name: string) {
  return providersEnabled.value.includes(name)
}
function toggleProvider(name: string) {
  if (providersEnabled.value.includes(name)) {
    providersEnabled.value = providersEnabled.value.filter((n) => n !== name)
  } else {
    providersEnabled.value = [...providersEnabled.value, name]
  }
}

// ===== 拖拽排序 =====
const draggingIndex = ref<number | null>(null)
const dragOverIndex = ref<number | null>(null)

function onDragStart(index: number, e: DragEvent) {
  draggingIndex.value = index
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(index))
  }
}
function onDragOver(index: number, e: DragEvent) {
  if (draggingIndex.value === null) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (dragOverIndex.value !== index) dragOverIndex.value = index
}
function onDrop(index: number, e: DragEvent) {
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
  const target = index + delta
  if (target < 0 || target >= providerOrder.value.length) return
  const next = [...providerOrder.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  providerOrder.value = next
}

// ===== 字段来源顺序 =====
const fieldNames = ref<string[]>([])
const defaultPolicy = ref<FieldPriorityMap>({})
const fieldPriority = ref<FieldPriorityMap>({})

const fieldLabel = (n: string) => {
  const base = getScrapeFieldLabel(n)
  const extra = scrapeFieldLabels[n] ? ` ${n.replace(/_/g, ' ').replace(/\b\w/g, (s) => s.toUpperCase())}` : ''
  return `${base}${extra}`
}

// 字段 pill 拖拽排序状态(按字段独立)
const fieldDragging = ref<{ field: string; index: number } | null>(null)
const fieldDragOver = ref<{ field: string; index: number } | null>(null)

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
  const cur = [...(fieldPriority.value[field] ?? [])]
  const [moved] = cur.splice(from.index, 1)
  cur.splice(index, 0, moved)
  fieldPriority.value = { ...fieldPriority.value, [field]: cur }
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

function resetFieldPriority() {
  const next: FieldPriorityMap = {}
  const enabled = new Set(providersEnabled.value)
  for (const f of fieldNames.value) {
    next[f] = (defaultPolicy.value[f] ?? []).filter((p) => enabled.has(p))
    for (const p of providersEnabled.value) {
      if (!next[f].includes(p)) next[f].push(p)
    }
  }
  fieldPriority.value = next
}

// 启用列表变化时同步每个字段:剔除已禁用,追加新启用
watch(providersEnabled, () => {
  if (fieldNames.value.length === 0) return
  const enabled = new Set(providersEnabled.value)
  const next: FieldPriorityMap = {}
  for (const f of fieldNames.value) {
    const cur = fieldPriority.value[f] ?? defaultPolicy.value[f] ?? []
    const kept = cur.filter((p) => enabled.has(p))
    for (const p of providersEnabled.value) {
      if (!kept.includes(p)) kept.push(p)
    }
    next[f] = kept
  }
  fieldPriority.value = next
}, { deep: true })

// ===== 媒体信息探测(FFprobe) =====
const probeProgress = computed(() => {
  const s = snapshots.probe
  if (!s) return null
  return {
    status: s.status === 'succeeded' ? 'completed' : s.status,
    totalItems: s.total,
    processedItems: s.processed,
    successItems: s.success ?? 0,
    failedItems: s.failed ?? 0,
    currentItem: s.current ?? '',
    percentage: s.percent,
    missingCount: s.counters?.missing ?? 0,
    versionsTotal: s.counters?.versionsTotal ?? 0,
  }
})
const probeThreads = ref('5')
const probePathMappings = ref<{ from: string; to: string }[]>([])
const probeOnIngest = ref(false)
const savingProbe = ref(false)

// ===== Backfill 存量回填 =====
const backfillProgress = computed(() => {
  const s = snapshots.backfill
  if (!s) return null
  const status =
    s.status === 'succeeded' ? 'completed' :
    s.status === 'cancelled' ? 'stopped' :
    s.status === 'failed'    ? 'error'   : s.status
  return {
    status,
    stage: s.stage ?? '',
    processed: s.processed,
    total: s.total,
    last_error: s.error ?? '',
    counters: s.counters ?? {},
    started_at: s.startedAt ?? 0,
    completed_at: s.completedAt ?? 0,
    last_run_at: s.completedAt ?? 0,
  }
})
const backfillConfig = ref<{ enabled_on_startup: boolean; episode_still_fetch: boolean }>({ enabled_on_startup: false, episode_still_fetch: true })
const backfillBusy = ref(false)
const stageLabel = (s: string) => ({ quality: '画质标签', name: 'Episode 标题', image: '分集缩略图' } as Record<string, string>)[s] || s
const backfillRunning = () => ['running', 'stopping'].includes(backfillProgress.value?.status ?? '')

async function refreshBackfill() {}
async function refreshBackfillConfig() {
  try {
    backfillConfig.value = await getBackfillConfig()
  } catch {}
}
async function handleStartBackfill(stages?: BackfillStage[]) {
  backfillBusy.value = true
  try {
    await startBackfill(stages)
    await refreshBackfill()
    showToast('回填任务已启动', 'success')
  } catch (err: any) {
    showToast(err?.message || '启动回填失败', 'error')
  } finally {
    setTimeout(() => { backfillBusy.value = false }, 1500)
  }
}
async function handleStopBackfill() {
  await stopBackfill()
  await refreshBackfill()
  showToast('正在停止回填...', 'success')
}
async function handleToggleBackfillStartup(v: boolean) {
  backfillConfig.value.enabled_on_startup = v
  try {
    await updateBackfillConfig({ enabled_on_startup: v })
  } catch {
    backfillConfig.value.enabled_on_startup = !v
    showToast('保存失败', 'error')
  }
}
async function handleToggleEpisodeStill(v: boolean) {
  backfillConfig.value.episode_still_fetch = v
  try {
    await updateBackfillConfig({ episode_still_fetch: v })
  } catch {
    backfillConfig.value.episode_still_fetch = !v
    showToast('保存失败', 'error')
  }
}
async function handleResetQuality() {
  if (!confirm('将清空全部 media_versions 的画质字段,再跑回填时会重新计算。确定?')) return
  try {
    await resetBackfillQuality()
    showToast('已重置画质标签字段', 'success')
    await refreshBackfill()
  } catch (err: any) {
    showToast(err?.message || '重置失败', 'error')
  }
}
async function handleResetEpisodeImage() {
  if (!confirm('将清空由 TMDB 下载的 Episode still 封面(本地兜底命中的不受影响)。确定?')) return
  try {
    await resetBackfillEpisodeImage()
    showToast('已重置 Episode 封面', 'success')
    await refreshBackfill()
  } catch (err: any) {
    showToast(err?.message || '重置失败', 'error')
  }
}

async function refreshTaskSummary() {
  await refreshScrapeSummary()
}

// ===== 保存 =====
async function handleSaveConfig() {
  savingConfig.value = true
  try {
    await updateSystemConfig({
      tmdb_language: tmdbLanguage.value,
      auto_scrape_enabled: String(autoScrape.value),
      tmdb_proxy: tmdbProxy.value,
      scrape_save_mode: scrapeSaveMode.value,
      scrape_confidence_threshold: String(confidenceThreshold.value),
      scrape_auto_apply: String(autoApplyEnabled.value),
      scrape_adult_content_filter_enabled: String(adultContentFilterEnabled.value),
    })
    showToast('基础设置已保存', 'success')
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    savingConfig.value = false
  }
}

async function handleSaveScrapeSources() {
  savingScrapeSources.value = true
  try {
    const priorityObj = buildProviderPriorityMap(providerOrder.value)
    await updateSystemConfig({
      tmdb_api_key: tmdbApiKeys.value.filter((k) => k.trim()).join(','),
      scrape_providers_enabled: JSON.stringify(providersEnabled.value),
      scrape_provider_priority: JSON.stringify(priorityObj),
      bangumi_ua: bangumiUA.value,
      douban_cookie: doubanCookie.value,
      tvdb_api_key: tvdbApiKey.value,
      tvdb_pin: tvdbPin.value,
      fanart_api_key: fanartApiKey.value,
    })
    showToast('刮削源已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  } finally {
    savingScrapeSources.value = false
  }
}

async function handleSaveFieldPriority() {
  try {
    await updateSystemConfig({
      scrape_field_priority: JSON.stringify(fieldPriority.value),
    })
    showToast('字段填充优先级已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  }
}

async function handleScrapeAll() {
  scraping.value = true
  try {
    const r: any = await scrapeAllMetadata()
    const n = Number(r?.enqueued ?? 0)
    if (n === 0) {
      showToast('没有需要入队的 item(都已有元数据或已入队)', 'info')
    } else {
      showToast(`已入队 ${n} 条,请到"观测中心 > 队列管道"查看进度`, 'success')
    }
    await refreshScrapeSummary()
  } catch {
    showToast('入队失败', 'error')
  } finally {
    scraping.value = false
  }
}

async function saveProbeSettingsOnly() {
  try {
    await updateSystemConfig({
      probe_threads: probeThreads.value,
      probe_path_mappings: JSON.stringify(probePathMappings.value.filter((m) => m.from && m.to)),
      probe_on_ingest: probeOnIngest.value ? 'true' : 'false',
    })
    showToast('探测设置已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  }
}

async function startProbeJob() {
  savingProbe.value = true
  try {
    await updateSystemConfig({
      probe_threads: probeThreads.value,
      probe_path_mappings: JSON.stringify(probePathMappings.value.filter((m) => m.from && m.to)),
      probe_on_ingest: probeOnIngest.value ? 'true' : 'false',
    })
    await startProbe(parseInt(probeThreads.value, 10))
    await refreshTaskSummary()
    showToast('媒体信息探测已启动', 'success')
  } catch (err: any) {
    showToast(err.message || '启动探测失败', 'error')
  } finally {
    savingProbe.value = false
  }
}

async function stopProbeJob() {
  await stopProbe()
  await refreshTaskSummary()
  showToast('正在停止探测...', 'success')
}

onMounted(() => {
  refreshScrapeSummary()
  getScrapeDefaults().then((defs) => {
    fieldNames.value = defs.field_names
    defaultPolicy.value = { ...defs.default_policy }
  }).catch(() => {})
  getSystemConfig().then((cfg: any) => {
    const keys = (cfg.tmdb_api_key || '').split(',').map((k: string) => k.trim()).filter((k: string) => k)
    tmdbApiKeys.value = keys.length > 0 ? keys : ['']
    tmdbLanguage.value = cfg.tmdb_language || 'zh-CN'
    autoScrape.value = cfg.auto_scrape_enabled === true || cfg.auto_scrape_enabled === 'true'
    tmdbProxy.value = cfg.tmdb_proxy || ''
    scrapeSaveMode.value = cfg.scrape_save_mode || 'database'

    try {
      const names = cfg.scrape_providers_enabled ? JSON.parse(cfg.scrape_providers_enabled) : null
      providersEnabled.value = Array.isArray(names) && names.length > 0
        ? names.filter((n: string) => defaultProviders.includes(n))
        : [...defaultProviders]
    } catch {
      providersEnabled.value = [...defaultProviders]
    }
    try {
      const savedPriority = cfg.scrape_provider_priority ? JSON.parse(cfg.scrape_provider_priority) : null
      providerOrder.value = buildOrderedProviders(
        savedPriority && typeof savedPriority === 'object' ? savedPriority : null,
        defaultProviders,
        providersEnabled.value,
      )
    } catch {
      providerOrder.value = [...defaultProviders]
    }

    const threshold = parseFloat(cfg.scrape_confidence_threshold)
    confidenceThreshold.value = Number.isFinite(threshold) && threshold > 0 && threshold <= 1 ? threshold : 0.72
    autoApplyEnabled.value = cfg.scrape_auto_apply !== 'false'
    adultContentFilterEnabled.value = cfg.scrape_adult_content_filter_enabled !== 'false'
    bangumiUA.value = cfg.bangumi_ua || ''
    doubanCookie.value = cfg.douban_cookie || ''
    tvdbApiKey.value = cfg.tvdb_api_key || ''
    tvdbPin.value = cfg.tvdb_pin || ''
    fanartApiKey.value = cfg.fanart_api_key || ''

    // 字段级优先级:saved 优先,缺字段用默认;过滤出当前启用的 provider
    let savedFP: FieldPriorityMap = {}
    try {
      savedFP = cfg.scrape_field_priority ? JSON.parse(cfg.scrape_field_priority) : {}
    } catch { savedFP = {} }
    const enabled = new Set(providersEnabled.value)
    const applyEnabledFilter = (arr: string[]) => {
      const kept = arr.filter((p) => enabled.has(p))
      for (const p of providersEnabled.value) {
        if (!kept.includes(p)) kept.push(p)
      }
      return kept
    }
    const waitForFieldNames = () => {
      if (fieldNames.value.length === 0) {
        setTimeout(waitForFieldNames, 50)
        return
      }
      const merged: FieldPriorityMap = {}
      for (const f of fieldNames.value) {
        const base = (Array.isArray(savedFP[f]) && savedFP[f].length > 0)
          ? savedFP[f]
          : (defaultPolicy.value[f] ?? [])
        merged[f] = applyEnabledFilter([...base])
      }
      fieldPriority.value = merged
    }
    waitForFieldNames()

    try { probePathMappings.value = cfg.probe_path_mappings ? JSON.parse(cfg.probe_path_mappings) : [] } catch { probePathMappings.value = [] }
    probeThreads.value = cfg.probe_threads || '5'
    probeOnIngest.value = cfg.probe_on_ingest === 'true'
  }).catch(() => {})
  void refreshBackfillConfig()
})
</script>

<template>
  <page-shell title="元数据" :icon="AppIcons.metadata" description="刮削源配置 · FFprobe 探测 · 历史回填">
    <div class="config-content">

    <!-- 卡 1: 基础设置 -->
    <n-card :bordered="false" class="glass-card section-card metadata-card">
      <template #header>
        <div class="card-header-wrap">
          <div class="icon-box tmdb">
            <n-icon :size="18"><SearchOutline /></n-icon>
          </div>
          <div class="header-copy">
            <div class="header-title">基础设置</div>
            <div class="header-desc">语言、保存位置、代理、自动化行为。</div>
          </div>
        </div>
      </template>
      <template #header-extra>
        <div v-if="scrapeSummary" class="inline-stat">
          <span class="inline-stat-value">{{ scrapeSummary.missing_count }}</span>
          <span class="inline-stat-name">待刮削<template v-if="scrapeSummary.items_total"> / {{ scrapeSummary.items_total }}</template></span>
        </div>
      </template>

      <n-form label-placement="top" size="small" class="config-form">
        <n-grid cols="1 m:3" x-gap="12" responsive="screen">
          <n-grid-item>
            <n-form-item label="元数据语言">
              <n-select v-model:value="tmdbLanguage" :options="tmdbLanguageOptions" size="small" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="保存位置">
              <n-select v-model:value="scrapeSaveMode" :options="scrapeSaveModeOptions" size="small" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="代理">
              <n-input v-model:value="tmdbProxy" placeholder="http:// 或 socks5://" size="small" />
            </n-form-item>
          </n-grid-item>
        </n-grid>

        <n-form-item label="识别阈值">
          <div class="threshold-row">
            <n-slider v-model:value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" :tooltip="true" style="flex: 1" />
            <n-input-number v-model:value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" size="small" style="width: 110px" />
          </div>
          <div class="hint-text">候选 ≥ 阈值直接采纳。推荐 0.72。</div>
        </n-form-item>

        <div class="switch-row-grid">
          <div class="switch-section-compact">
            <div class="switch-copy">
              <div class="switch-title">自动刮削</div>
              <div class="hint-text">新媒体入库时自动抓取元数据。</div>
            </div>
            <n-switch v-model:value="autoScrape" :round="false" />
          </div>
          <div class="switch-section-compact">
            <div class="switch-copy">
              <div class="switch-title">自动采纳</div>
              <div class="hint-text">低于阈值的候选自动采纳,否则进入人工确认队列。</div>
            </div>
            <n-switch v-model:value="autoApplyEnabled" :round="false" />
          </div>
          <div class="switch-section-compact">
            <div class="switch-copy">
              <div class="switch-title">成人内容过滤</div>
              <div class="hint-text">拦截成人影视内容候选；命中时按识别失败处理，不覆盖现有元数据。</div>
            </div>
            <n-switch v-model:value="adultContentFilterEnabled" :round="false" />
          </div>
        </div>
      </n-form>

      <div class="card-actions">
        <n-button type="primary" size="small" :loading="savingConfig" @click="handleSaveConfig">保存基础设置</n-button>
        <n-button secondary size="small" :loading="scraping" :disabled="scraping || scrapeSummary?.missing_count === 0" @click="handleScrapeAll">刮削缺失元数据</n-button>
      </div>
    </n-card>

    <!-- 卡 2: 刮削源 (sidebar + detail) -->
    <n-card :bordered="false" class="glass-card section-card metadata-card">
      <template #header>
        <div class="card-header-wrap">
          <div class="icon-box tmdb">
            <n-icon :size="18"><LayersOutline /></n-icon>
          </div>
          <div class="header-copy">
            <div class="header-title">刮削源</div>
            <div class="header-desc">识别按列表顺序逐源尝试,首个命中即停。拖拽调整顺序、勾选启用/停用。</div>
          </div>
        </div>
      </template>
      <template #header-extra>
        <n-button quaternary size="small" @click="router.push({ name: 'media_unmatched' })">
          <template #icon><n-icon><ArrowForwardOutline /></n-icon></template>
          未匹配面板
        </n-button>
      </template>

      <div class="provider-split">
        <!-- sidebar -->
        <div class="provider-sidebar">
          <div
            v-for="(name, idx) in providerOrder"
            :key="name"
            class="provider-row"
            :class="{
              dragging: draggingIndex === idx,
              'drag-over': dragOverIndex === idx && draggingIndex !== idx,
              selected: selectedProvider === name,
              disabled: !isProviderEnabled(name),
            }"
            :style="{ '--accent': providerMeta[name]?.accent }"
            draggable="true"
            @click="selectedProvider = name"
            @dragstart="onDragStart(idx, $event)"
            @dragover="onDragOver(idx, $event)"
            @drop="onDrop(idx, $event)"
            @dragend="onDragEnd"
            @dragleave="dragOverIndex === idx ? (dragOverIndex = null) : null"
          >
            <div class="provider-handle" title="拖拽调序">
              <n-icon><ReorderFourOutline /></n-icon>
            </div>

            <label class="provider-check" @click.stop>
              <input type="checkbox" :checked="isProviderEnabled(name)" @change="toggleProvider(name)" />
            </label>

            <div class="provider-info">
              <div class="provider-name">
                {{ providerMeta[name]?.label }}
                <span v-if="providerMeta[name]?.badge" class="provider-badge">{{ providerMeta[name]?.badge }}</span>
              </div>
              <div class="provider-desc">{{ providerMeta[name]?.desc }}</div>
            </div>

            <span class="provider-index">{{ idx + 1 }}</span>

            <div class="provider-move">
              <n-button quaternary circle size="tiny" :disabled="idx === 0" @click.stop="moveProvider(idx, -1)">
                <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
              </n-button>
              <n-button quaternary circle size="tiny" :disabled="idx === providerOrder.length - 1" @click.stop="moveProvider(idx, 1)">
                <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
              </n-button>
            </div>
          </div>
        </div>

        <!-- detail -->
        <div class="provider-detail">
          <div class="detail-header">
            <n-icon :size="16" :color="providerMeta[selectedProvider]?.accent">
              <component :is="isProviderEnabled(selectedProvider) ? CheckmarkCircle : EllipseOutline" />
            </n-icon>
            <div class="detail-title">{{ providerMeta[selectedProvider]?.label }}</div>
            <div class="detail-badge" v-if="providerMeta[selectedProvider]?.badge">{{ providerMeta[selectedProvider]?.badge }}</div>
          </div>

          <!-- TMDB -->
          <div v-if="selectedProvider === 'tmdb'" class="detail-body">
            <div class="subsection-title-inline">API Key</div>
            <div v-for="(key, idx) in tmdbApiKeys" :key="idx" class="api-key-row">
              <span class="row-index">{{ idx + 1 }}</span>
              <n-input :value="tmdbApiKeys[idx]" @update:value="(v: string) => tmdbApiKeys[idx] = v" :type="showApiKey ? 'text' : 'password'" :placeholder="`TMDB API Key ${idx + 1}`" size="small" />
              <n-button v-if="tmdbApiKeys.length > 1" quaternary circle type="error" size="small" @click="tmdbApiKeys = tmdbApiKeys.filter((_, i) => i !== idx)">
                <template #icon><n-icon><CloseOutline /></n-icon></template>
              </n-button>
            </div>
            <div class="inline-actions">
              <n-button secondary size="tiny" @click="tmdbApiKeys = [...tmdbApiKeys, '']">
                <template #icon><n-icon><AddOutline /></n-icon></template>
                添加 Key
              </n-button>
              <n-button quaternary size="tiny" @click="showApiKey = !showApiKey">{{ showApiKey ? '隐藏' : '显示' }}</n-button>
            </div>
            <div class="hint-text">支持多个 Key 轮询,避免单 Key 风控。未配置时 TMDB 源自动跳过。</div>
          </div>

          <!-- TVDB -->
          <div v-else-if="selectedProvider === 'tvdb'" class="detail-body">
            <n-form-item label="API Key">
              <n-input v-model:value="tvdbApiKey" type="password" placeholder="订阅 TVDB 后填入,留空则禁用" size="small" show-password-on="click" />
            </n-form-item>
            <n-form-item label="Pin (可选)">
              <n-input v-model:value="tvdbPin" placeholder="TVDB 用户 Pin" size="small" />
            </n-form-item>
            <div class="hint-text">未配置 API Key 时 TVDB 源自动跳过。</div>
          </div>

          <!-- Bangumi -->
          <div v-else-if="selectedProvider === 'bangumi'" class="detail-body">
            <n-form-item label="User-Agent">
              <n-input v-model:value="bangumiUA" placeholder="留空使用默认 fyms/1.0" size="small" />
            </n-form-item>
            <div class="hint-text">Bangumi 要求请求带 UA 注明来源;填 GitHub/邮箱标识更友好。</div>
          </div>

          <!-- Douban -->
          <div v-else-if="selectedProvider === 'douban'" class="detail-body">
            <n-form-item label="Cookie (可选)">
              <n-input v-model:value="doubanCookie" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" placeholder="粘贴已登录账号的 Cookie,提高搜索配额" size="small" />
            </n-form-item>
            <div class="hint-text">非官方 API,仅作中文补全。触发风控会自动熔断 10 分钟。停用豆瓣请在左侧取消勾选。</div>
          </div>

          <!-- Fanart.tv -->
          <div v-else-if="selectedProvider === 'fanart'" class="detail-body">
            <n-form-item label="API Key">
              <n-input v-model:value="fanartApiKey" type="password" placeholder="留空则禁用图片补充" size="small" show-password-on="click" />
            </n-form-item>
            <div class="hint-text">只参与图片补充(poster / backdrop / seasonposter),不参与识别。</div>
          </div>
        </div>
      </div>

      <div class="card-actions">
        <n-button type="primary" size="small" :loading="savingScrapeSources" @click="handleSaveScrapeSources">保存刮削源</n-button>
      </div>
    </n-card>

    <!-- 卡 3: 字段填充优先级 -->
    <n-card :bordered="false" class="glass-card section-card metadata-card">
      <template #header>
        <div class="card-header-wrap">
          <div class="icon-box field">
            <n-icon :size="18"><LayersOutline /></n-icon>
          </div>
          <div class="header-copy">
            <div class="header-title">字段填充优先级</div>
            <div class="header-desc">多源合并时,每个字段按此顺序取首个非空值。</div>
          </div>
        </div>
      </template>
      <template #header-extra>
        <n-button quaternary size="small" @click="resetFieldPriority">重置为默认</n-button>
      </template>

      <div class="field-priority-list">
        <div v-for="f in fieldNames" :key="f" class="field-priority-row">
          <div class="field-priority-label">{{ fieldLabel(f) }}</div>
          <div class="field-priority-pills">
            <div
              v-for="(pname, pidx) in (fieldPriority[f] || [])"
              :key="pname"
              class="field-priority-pill"
              :class="{
                dragging: fieldDragging && fieldDragging.field === f && fieldDragging.index === pidx,
                'drag-over': fieldDragOver && fieldDragOver.field === f && fieldDragOver.index === pidx && !(fieldDragging && fieldDragging.field === f && fieldDragging.index === pidx),
              }"
              :style="{ '--accent': providerMeta[pname]?.accent }"
              draggable="true"
              @dragstart="onFieldPillDragStart(f, pidx, $event)"
              @dragover="onFieldPillDragOver(f, pidx, $event)"
              @drop="onFieldPillDrop(f, pidx, $event)"
              @dragend="onFieldPillDragEnd"
              @dragleave="onFieldPillDragLeave(f, pidx)"
              title="拖拽调整顺序"
            >
              <n-icon class="pill-handle"><ReorderFourOutline /></n-icon>
              <span class="pill-order">{{ pidx + 1 }}</span>
              <span class="pill-name">{{ providerLabel(pname) }}</span>
            </div>
            <div v-if="!(fieldPriority[f] || []).length" class="hint-text">无启用源</div>
          </div>
        </div>
      </div>

      <div class="card-actions">
        <n-button type="primary" size="small" @click="handleSaveFieldPriority">保存字段顺序</n-button>
      </div>
    </n-card>

    <!-- 卡 4+5: 探测 + 回填 (两栏) -->
    <div class="two-col">
      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box probe">
              <n-icon :size="18"><VideocamOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">媒体信息探测</div>
              <div class="header-desc">对缺少媒体信息的 `strm` 执行 ffprobe 探测,补充视频/音频流信息。</div>
            </div>
          </div>
        </template>
        <template #header-extra>
          <span class="header-badge">FFprobe</span>
        </template>

        <div v-if="probeProgress" class="stats-grid">
          <div class="stat-box">
            <div class="stat-value">{{ probeProgress.status === 'idle' ? probeProgress.missingCount : probeProgress.totalItems - probeProgress.processedItems }}</div>
            <div class="stat-name">待探测<template v-if="probeProgress.versionsTotal"> / {{ probeProgress.versionsTotal }} 总版本</template></div>
          </div>
          <div class="stat-box">
            <div class="stat-value ok">{{ probeProgress.successItems || 0 }}</div>
            <div class="stat-name">成功</div>
          </div>
          <div class="stat-box">
            <div class="stat-value err">{{ probeProgress.failedItems || 0 }}</div>
            <div class="stat-name">失败</div>
          </div>
        </div>

        <div v-if="probeProgress && (probeProgress.status === 'running' || probeProgress.status === 'stopping')" class="progress-panel">
          <div class="progress-row">
            <n-progress type="line" :percentage="probeProgress.percentage" :show-indicator="false" :color="probeProgress.status === 'stopping' ? '#ff9800' : undefined" style="flex: 1" />
            <span class="pct">{{ probeProgress.percentage }}%</span>
          </div>
          <div class="panel-meta">
            {{ probeProgress.processedItems }}/{{ probeProgress.totalItems }}
            <span v-if="probeProgress.currentItem">当前: {{ probeProgress.currentItem }}</span>
            <span v-if="probeProgress.status === 'stopping'">正在停止...</span>
          </div>
        </div>
        <div v-else-if="probeProgress?.status === 'completed'" class="success-note">
          探测完成: {{ probeProgress.successItems }} 成功, {{ probeProgress.failedItems }} 失败
        </div>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">执行设置</div>
            <n-grid cols="1 m:2" x-gap="12" responsive="screen">
              <n-grid-item span="1">
                <n-form-item label="并发线程">
                  <n-select v-model:value="probeThreads" :options="probeThreadsOptions" :disabled="probeProgress?.status === 'running'" size="small" />
                </n-form-item>
              </n-grid-item>
              <n-grid-item span="1">
                <n-form-item label="新入库自动探测">
                  <n-switch v-model:value="probeOnIngest" :disabled="probeProgress?.status === 'running'" />
                  <span class="hint-text" style="margin-left: 10px">扫库结束后,若有未探测的 media_version 则自动跑一次 ffprobe</span>
                </n-form-item>
              </n-grid-item>
            </n-grid>
          </div>

          <div class="subsection">
            <div class="subsection-title">路径映射</div>
            <div class="hint-text mapping-hint">将 `strm` 中的路径映射到当前机器可访问的挂载路径。</div>
            <div class="mappings-box">
              <div class="mappings-head">
                <div>源路径</div>
                <div class="arrow-slot"></div>
                <div>目标路径</div>
                <div class="action-slot"></div>
              </div>

              <div v-if="probePathMappings.length === 0" class="mappings-empty">
                <n-empty size="small" description="暂无路径映射" />
              </div>

              <div v-else class="mappings-list">
                <div v-for="(m, i) in probePathMappings" :key="i" class="mapping-row">
                  <n-input v-model:value="m.from" placeholder="/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" />
                  <div class="arrow-slot">
                    <n-icon depth="3"><ArrowForwardOutline /></n-icon>
                  </div>
                  <n-input v-model:value="m.to" placeholder="/mnt/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" />
                  <div class="action-slot">
                    <n-button quaternary circle type="error" size="small" :disabled="probeProgress?.status === 'running'" @click="probePathMappings = probePathMappings.filter((_, idx) => idx !== i)">
                      <template #icon><n-icon><CloseOutline /></n-icon></template>
                    </n-button>
                  </div>
                </div>
              </div>

              <div class="mappings-footer">
                <n-button dashed size="small" block :disabled="probeProgress?.status === 'running'" @click="probePathMappings = [...probePathMappings, { from: '', to: '' }]">
                  <template #icon><n-icon><AddOutline /></n-icon></template>
                  添加映射
                </n-button>
              </div>
            </div>
          </div>
        </n-form>

        <div class="card-actions">
          <n-button v-if="probeProgress?.status !== 'running' && probeProgress?.status !== 'stopping'" type="primary" size="small" :loading="savingProbe" :disabled="savingProbe || (probeProgress?.missingCount === 0 && probeProgress?.status === 'idle')" @click="startProbeJob">开始探测</n-button>
          <n-button v-else type="warning" size="small" :disabled="probeProgress?.status === 'stopping'" @click="stopProbeJob">{{ probeProgress?.status === 'stopping' ? '停止中...' : '停止探测' }}</n-button>
          <n-button secondary size="small" :disabled="probeProgress?.status === 'running'" @click="saveProbeSettingsOnly">保存设置</n-button>
        </div>
      </n-card>

      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box backfill">
              <n-icon :size="18"><LayersOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">历史数据回填</div>
              <div class="header-desc">补齐存量库的画质标签、Episode 标题与分集缩略图。按 画质 → 标题 → 封面 顺序执行。</div>
            </div>
          </div>
        </template>

        <div v-if="backfillProgress && backfillProgress.total > 0" class="progress-panel">
          <div class="progress-row">
            <n-progress type="line" :percentage="backfillProgress.total > 0 ? Math.floor(backfillProgress.processed * 100 / backfillProgress.total) : 0" :show-indicator="false" :color="backfillProgress.status === 'stopping' ? '#ff9800' : undefined" style="flex: 1" />
            <span class="pct">{{ backfillProgress.total > 0 ? Math.floor(backfillProgress.processed * 100 / backfillProgress.total) : 0 }}%</span>
          </div>
          <div class="panel-meta">
            <span>阶段:{{ stageLabel(backfillProgress.stage) || '—' }}</span>
            <span>{{ backfillProgress.processed }}/{{ backfillProgress.total }}</span>
            <span v-if="backfillProgress.counters?.quality_updated">画质 {{ backfillProgress.counters.quality_updated }}</span>
            <span v-if="backfillProgress.counters?.name_cleaned">标题 {{ backfillProgress.counters.name_cleaned }}</span>
            <span v-if="backfillProgress.counters?.image_local_hit">本地封面 {{ backfillProgress.counters.image_local_hit }}</span>
            <span v-if="backfillProgress.counters?.image_api_hit">TMDB 封面 {{ backfillProgress.counters.image_api_hit }}</span>
          </div>
          <div v-if="backfillProgress.last_error" class="panel-error">{{ backfillProgress.last_error }}</div>
        </div>
        <div v-else-if="backfillProgress?.status === 'completed'" class="success-note">
          上次回填完成<span v-if="backfillProgress.last_run_at">于 {{ new Date(backfillProgress.last_run_at).toLocaleString() }}</span>
        </div>
        <div v-else-if="backfillProgress?.status === 'stopped'" class="success-note">
          上次任务被停止
        </div>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">总开关</div>
            <div class="switch-row">
              <n-switch :value="backfillConfig.enabled_on_startup" @update:value="handleToggleBackfillStartup" size="small" />
              <div class="switch-copy">
                <div class="switch-title">启动时自动回填</div>
                <div class="switch-desc">服务启动时按 画质 → 标题 → 封面 顺序跑一次。24h 内不重复触发。</div>
              </div>
            </div>
            <div class="switch-row">
              <n-switch :value="backfillConfig.episode_still_fetch" @update:value="handleToggleEpisodeStill" size="small" />
              <div class="switch-copy">
                <div class="switch-title">拉取 TMDB 分集封面</div>
                <div class="switch-desc">关闭后,分集封面只读本地 thumb,不再打 TMDB。</div>
              </div>
            </div>
          </div>
        </n-form>

        <div class="card-actions">
          <n-button v-if="!backfillRunning()" type="primary" size="small" :loading="backfillBusy" @click="handleStartBackfill()">全部执行</n-button>
          <n-button v-if="!backfillRunning()" secondary size="small" :disabled="backfillBusy" @click="handleStartBackfill(['quality'])">仅画质(快)</n-button>
          <n-button v-if="!backfillRunning()" secondary size="small" :disabled="backfillBusy" @click="handleStartBackfill(['name'])">仅 Episode 标题</n-button>
          <n-button v-if="!backfillRunning()" secondary size="small" :disabled="backfillBusy" @click="handleStartBackfill(['image'])">仅分集封面(慢)</n-button>
          <n-button v-if="backfillRunning()" type="warning" size="small" :disabled="backfillProgress?.status === 'stopping'" @click="handleStopBackfill">{{ backfillProgress?.status === 'stopping' ? '停止中...' : '停止回填' }}</n-button>
        </div>

        <div class="card-actions" style="margin-top: 8px">
          <n-button quaternary size="small" :disabled="backfillRunning()" @click="handleResetQuality">重置画质字段</n-button>
          <n-button quaternary size="small" :disabled="backfillRunning()" @click="handleResetEpisodeImage">重置 Episode 封面</n-button>
        </div>
      </n-card>
    </div>
    </div>
  </page-shell>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  align-items: stretch;
}
@media (max-width: 900px) {
  .two-col {
    grid-template-columns: 1fr;
  }
}

.metadata-card {
  display: flex;
  flex-direction: column;
}

.card-header-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
}

.icon-box {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: var(--c-slate-100);
  color: var(--app-primary);
}
.app-dark .icon-box {
  background: var(--c-slate-800);
}
.icon-box.tmdb {
  color: #0ea5e9;
  background: rgba(14, 165, 233, 0.12);
}
.icon-box.probe {
  color: #10b981;
  background: rgba(16, 185, 129, 0.12);
}
.icon-box.field {
  color: #a855f7;
  background: rgba(168, 85, 247, 0.12);
}
.icon-box.backfill {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.12);
}

.header-copy {
  min-width: 0;
}
.header-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
  line-height: 1.3;
}
.header-desc {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.header-badge {
  font-size: 10px;
  font-weight: 600;
  color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.1);
  padding: 2px 7px;
  border-radius: 4px;
  letter-spacing: 0.3px;
}

.inline-stat {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-size: 11px;
  color: var(--app-text-muted);
}
.inline-stat-value {
  font-size: 18px;
  font-weight: 700;
  color: var(--app-text);
}

.config-form {
  margin-top: 14px;
}

.subsection {
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 12px;
  padding: 14px;
  background: rgba(255,255,255,0.02);
  margin-bottom: 14px;
}
.subsection-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
  margin-bottom: 10px;
}
.subsection-title-inline {
  font-size: 11px;
  font-weight: 600;
  color: var(--app-text-muted);
  margin-bottom: 6px;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.hint-text {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.5;
  margin-top: 6px;
  opacity: 0.85;
}

.threshold-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.switch-row-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 8px;
}
@media (max-width: 700px) {
  .switch-row-grid {
    grid-template-columns: 1fr;
  }
}
.switch-section-compact {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 10px;
  background: rgba(255,255,255,0.02);
}

.api-key-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 6px;
}
.row-index {
  font-size: 11px;
  color: var(--app-text-muted);
  width: 18px;
  text-align: right;
  flex-shrink: 0;
  opacity: 0.6;
}
.inline-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
  flex-wrap: wrap;
}

/* ===== 刮削源 sidebar + detail ===== */
.provider-split {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 12px;
  margin-top: 14px;
  align-items: stretch;
  min-height: 240px;
}
@media (max-width: 700px) {
  .provider-split {
    grid-template-columns: 1fr;
  }
}

.provider-sidebar {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 10px;
  background: rgba(255,255,255,0.01);
}

.provider-row {
  display: grid;
  grid-template-columns: 22px 22px 1fr auto 40px;
  gap: 8px;
  align-items: center;
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid transparent;
  background: transparent;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s, border-color 0.15s, opacity 0.15s;
}
.provider-row:hover {
  background: rgba(255, 255, 255, 0.04);
}
.provider-row.dragging {
  opacity: 0.4;
}
.provider-row.drag-over {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.08);
}
.provider-row.selected {
  background: color-mix(in srgb, var(--accent, var(--app-primary)) 12%, transparent);
  border-color: color-mix(in srgb, var(--accent, var(--app-primary)) 40%, transparent);
}
.provider-row.disabled .provider-info {
  opacity: 0.45;
}

.provider-handle {
  color: var(--app-text-muted);
  opacity: 0.5;
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}
.provider-row:hover .provider-handle {
  opacity: 0.9;
}
.provider-handle:active {
  cursor: grabbing;
}

.provider-check {
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.provider-check input {
  accent-color: var(--accent, var(--app-primary));
  cursor: pointer;
}

.provider-info {
  min-width: 0;
}
.provider-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
  display: flex;
  align-items: center;
  gap: 6px;
  line-height: 1.3;
}
.provider-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--accent, var(--app-primary)) 18%, transparent);
  color: var(--accent, var(--app-primary));
  letter-spacing: 0.3px;
}
.provider-desc {
  font-size: 11px;
  color: var(--app-text-muted);
  margin-top: 2px;
}

.provider-index {
  font-variant-numeric: tabular-nums;
  font-size: 11px;
  font-weight: 600;
  color: var(--app-text-muted);
  opacity: 0.6;
  min-width: 18px;
  text-align: right;
}

.provider-move {
  display: flex;
  gap: 0;
}
.provider-move :deep(.n-button) {
  width: 18px;
  height: 18px;
  min-width: 18px;
}

.provider-detail {
  padding: 16px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 10px;
  background: rgba(255,255,255,0.02);
}
.detail-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 12px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.05));
}
.detail-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}
.detail-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 10px;
  background: rgba(var(--app-primary-rgb), 0.1);
  color: var(--app-primary);
  letter-spacing: 0.3px;
}
.detail-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* ===== 字段填充优先级 ===== */
.field-priority-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}
.field-priority-row {
  display: grid;
  grid-template-columns: 180px 1fr;
  gap: 10px;
  align-items: center;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--app-border, rgba(255,255,255,0.04));
}
@media (max-width: 600px) {
  .field-priority-row {
    grid-template-columns: 1fr;
  }
}
.field-priority-label {
  font-size: 12px;
  color: var(--app-text);
  font-weight: 500;
}
.field-priority-pills {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.field-priority-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px 3px 7px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--accent, #888) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent, #888) 30%, transparent);
  font-size: 11px;
  color: var(--app-text);
  cursor: grab;
  user-select: none;
  transition: background 0.15s, border-color 0.15s, opacity 0.15s, transform 0.15s, box-shadow 0.15s;
}
.field-priority-pill:hover {
  background: color-mix(in srgb, var(--accent, #888) 22%, transparent);
  border-color: color-mix(in srgb, var(--accent, #888) 50%, transparent);
}
.field-priority-pill:active {
  cursor: grabbing;
}
.field-priority-pill.dragging {
  opacity: 0.4;
}
.field-priority-pill.drag-over {
  transform: translateX(3px);
  border-color: color-mix(in srgb, var(--accent, var(--app-primary)) 75%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent, var(--app-primary)) 40%, transparent);
}
.field-priority-pill .pill-handle {
  font-size: 11px;
  color: var(--app-text-muted);
  opacity: 0.55;
  display: inline-flex;
}
.field-priority-pill:hover .pill-handle {
  opacity: 0.85;
}
.field-priority-pill .pill-order {
  font-variant-numeric: tabular-nums;
  color: var(--app-text-muted);
  font-weight: 600;
  opacity: 0.7;
}
.field-priority-pill .pill-name {
  padding: 0 2px;
}

/* ===== 通用 switch / card-actions 保持和老样式兼容 ===== */
.switch-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.switch-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
}
.switch-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}
.switch-desc {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.4;
}

.card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 14px;
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
  margin-top: auto;
}

/* ===== 探测/回填 panel 复用旧样式 ===== */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}
.stat-box {
  padding: 12px;
  border-radius: 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  background: rgba(255,255,255,0.02);
}
.stat-value {
  font-size: 24px;
  font-weight: 700;
  line-height: 1.2;
  color: var(--app-text);
}
.stat-value.ok { color: #22c55e; }
.stat-value.err { color: var(--app-danger, #e53935); }
.stat-name {
  margin-top: 4px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.progress-panel {
  margin-top: 16px;
  padding: 12px;
  background: rgba(128,128,128,0.04);
  border-radius: 10px;
}
.progress-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.pct {
  font-size: 11px;
  color: var(--app-primary);
  font-weight: 600;
  min-width: 32px;
  text-align: right;
}
.panel-meta {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 8px;
  font-size: 11px;
  color: var(--app-text-muted);
}
.panel-error {
  margin-top: 6px;
  font-size: 11px;
  color: var(--app-danger, #e53935);
}
.success-note {
  margin-top: 16px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(34, 197, 94, 0.08);
  color: #16a34a;
  font-size: 12px;
}

.mapping-hint {
  margin-bottom: 10px;
  margin-top: -2px;
}
.mappings-box {
  background: var(--app-bg);
  border-radius: 10px;
  padding: 12px;
  border: 1px solid var(--app-border);
}
.mappings-head,
.mapping-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 24px minmax(0, 1fr) 32px;
  gap: 8px;
  align-items: center;
}
.mappings-head {
  padding: 0 4px 8px;
  margin-bottom: 8px;
  border-bottom: 1px dashed var(--app-border);
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text-muted);
}
.mappings-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mappings-empty {
  padding: 12px 0;
}
.arrow-slot,
.action-slot {
  display: flex;
  align-items: center;
  justify-content: center;
}
.mappings-footer {
  margin-top: 12px;
}

@media (max-width: 500px) {
  .stats-grid {
    grid-template-columns: 1fr;
  }
  .mappings-head,
  .mapping-row {
    grid-template-columns: 1fr;
  }
  .arrow-slot,
  .action-slot {
    justify-content: flex-start;
  }
}
</style>
