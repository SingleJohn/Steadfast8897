<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import {
  getSystemConfig, updateSystemConfig,
  scrapeAllMetadata, getScrapeProgress,
  startProbe, stopProbe,
  startBackfill, stopBackfill,
  getBackfillConfig, updateBackfillConfig,
  resetBackfillQuality, resetBackfillEpisodeImage,
  getScrapeDefaults,
  getActorImageSummary, backfillAllActorImages,
  type BackfillStage, type FieldPriorityMap, type ActorImageSummary,
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
import ActorImageCard from './metadata/components/ActorImageCard.vue'
import BackfillCard from './metadata/components/BackfillCard.vue'
import BasicSettingsCard from './metadata/components/BasicSettingsCard.vue'
import FieldPriorityCard from './metadata/components/FieldPriorityCard.vue'
import ProbeCard from './metadata/components/ProbeCard.vue'
import ProviderSourcesCard from './metadata/components/ProviderSourcesCard.vue'

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
// 本地/挂载原图直读(不复制到 data/cache/sources)。对应 system_config.image_cache_copy_local 取反。
const imageDirectRead = ref(true)
const autoScrape = ref(false)
const confidenceThreshold = ref<number>(0.72)
const autoApplyEnabled = ref(true)
const adultContentFilterEnabled = ref(true)
const savingConfig = ref(false)
const scraping = ref(false)

// ===== 演员头像 =====
const actorNfoThumb = ref(true)
const actorLocalActors = ref(true)
const actorLocalLib = ref(false)
const actorLocalLibPath = ref('')
const actorExtSource = ref(false)
const actorExtUrl = ref('')
const actorImageSummary = ref<ActorImageSummary | null>(null)
const savingActorConfig = ref(false)
const backfillingActor = ref(false)

async function refreshActorImageSummary() {
  try {
    actorImageSummary.value = await getActorImageSummary()
  } catch {
    actorImageSummary.value = null
  }
}

async function handleSaveActorImageConfig() {
  savingActorConfig.value = true
  try {
    await updateSystemConfig({
      actor_img_nfo_thumb: String(actorNfoThumb.value),
      actor_img_local_actors: String(actorLocalActors.value),
      actor_img_local_lib: String(actorLocalLib.value),
      actor_img_local_lib_path: actorLocalLibPath.value,
      actor_img_ext_source: String(actorExtSource.value),
      actor_img_ext_url: actorExtUrl.value,
    })
    showToast('演员头像设置已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  } finally {
    savingActorConfig.value = false
  }
}

async function handleBackfillActorImages() {
  backfillingActor.value = true
  try {
    const res = await backfillAllActorImages()
    const parts: string[] = []
    if (res.name_source_on) parts.push(`按名补 ${res.persons_filled}/${res.persons_scanned}`)
    if (res.tmdb_items_queued > 0) parts.push(`TMDB 入队 ${res.tmdb_items_queued} 项`)
    showToast(parts.length ? `已补演员头像:${parts.join(',')}` : '没有可补的演员头像', 'success')
    await refreshActorImageSummary()
  } catch (err: any) {
    showToast(err?.message || '批量补头像失败', 'error')
  } finally {
    backfillingActor.value = false
  }
}

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
      image_cache_copy_local: String(!imageDirectRead.value),
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
  refreshActorImageSummary()
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
    imageDirectRead.value = cfg.image_cache_copy_local !== 'true'

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

    // 演员头像开关(默认:NFO/.actors 开,头像库/外部源关)
    actorNfoThumb.value = cfg.actor_img_nfo_thumb !== 'false'
    actorLocalActors.value = cfg.actor_img_local_actors !== 'false'
    actorLocalLib.value = cfg.actor_img_local_lib === 'true'
    actorLocalLibPath.value = cfg.actor_img_local_lib_path || ''
    actorExtSource.value = cfg.actor_img_ext_source === 'true'
    actorExtUrl.value = cfg.actor_img_ext_url || ''
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
    <div class="metadata-page">
      <div class="config-content">
        <BasicSettingsCard
          v-model:tmdb-language="tmdbLanguage"
          v-model:scrape-save-mode="scrapeSaveMode"
          v-model:tmdb-proxy="tmdbProxy"
          v-model:confidence-threshold="confidenceThreshold"
          v-model:auto-scrape="autoScrape"
          v-model:auto-apply-enabled="autoApplyEnabled"
          v-model:adult-content-filter-enabled="adultContentFilterEnabled"
          v-model:image-direct-read="imageDirectRead"
          :tmdb-language-options="tmdbLanguageOptions"
          :scrape-save-mode-options="scrapeSaveModeOptions"
          :scrape-summary="scrapeSummary"
          :saving-config="savingConfig"
          :scraping="scraping"
          @save="handleSaveConfig"
          @scrape="handleScrapeAll"
        />

        <ActorImageCard
          v-model:nfo-thumb="actorNfoThumb"
          v-model:local-actors="actorLocalActors"
          v-model:local-lib="actorLocalLib"
          v-model:local-lib-path="actorLocalLibPath"
          v-model:ext-source="actorExtSource"
          v-model:ext-url="actorExtUrl"
          :summary="actorImageSummary"
          :saving-config="savingActorConfig"
          :backfilling="backfillingActor"
          @save="handleSaveActorImageConfig"
          @backfill="handleBackfillActorImages"
        />

        <ProviderSourcesCard
          v-model:selected-provider="selectedProvider"
          v-model:drag-over-index="dragOverIndex"
          v-model:tmdb-api-keys="tmdbApiKeys"
          v-model:show-api-key="showApiKey"
          v-model:tvdb-api-key="tvdbApiKey"
          v-model:tvdb-pin="tvdbPin"
          v-model:bangumi-ua="bangumiUA"
          v-model:douban-cookie="doubanCookie"
          v-model:fanart-api-key="fanartApiKey"
          :provider-order="providerOrder"
          :providers-enabled="providersEnabled"
          :provider-meta="providerMeta"
          :dragging-index="draggingIndex"
          :saving-scrape-sources="savingScrapeSources"
          @toggle-provider="toggleProvider"
          @drag-start="onDragStart"
          @drag-over="onDragOver"
          @drop="onDrop"
          @drag-end="onDragEnd"
          @move-provider="moveProvider"
          @unmatched="router.push({ name: 'media_unmatched' })"
          @save="handleSaveScrapeSources"
        />

        <FieldPriorityCard
          :field-names="fieldNames"
          :field-priority="fieldPriority"
          :provider-meta="providerMeta"
          :field-dragging="fieldDragging"
          :field-drag-over="fieldDragOver"
          :field-label="fieldLabel"
          :provider-label="providerLabel"
          @reset="resetFieldPriority"
          @save="handleSaveFieldPriority"
          @drag-start="onFieldPillDragStart"
          @drag-over="onFieldPillDragOver"
          @drop="onFieldPillDrop"
          @drag-end="onFieldPillDragEnd"
          @drag-leave="onFieldPillDragLeave"
        />

        <div class="two-col">
          <ProbeCard
            v-model:probe-threads="probeThreads"
            v-model:probe-path-mappings="probePathMappings"
            v-model:probe-on-ingest="probeOnIngest"
            :probe-progress="probeProgress"
            :probe-threads-options="probeThreadsOptions"
            :saving-probe="savingProbe"
            @start="startProbeJob"
            @stop="stopProbeJob"
            @save="saveProbeSettingsOnly"
          />

          <BackfillCard
            :backfill-progress="backfillProgress"
            :backfill-config="backfillConfig"
            :backfill-busy="backfillBusy"
            :is-running="backfillRunning()"
            :stage-label="stageLabel"
            @toggle-startup="handleToggleBackfillStartup"
            @toggle-episode-still="handleToggleEpisodeStill"
            @start="handleStartBackfill"
            @stop="handleStopBackfill"
            @reset-quality="handleResetQuality"
            @reset-episode-image="handleResetEpisodeImage"
          />
        </div>
      </div>
    </div>
  </page-shell>
</template>


<style src="./metadata/styles.css"></style>
