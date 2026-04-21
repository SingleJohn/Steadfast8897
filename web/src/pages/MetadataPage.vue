<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import { NButton, NCard, NCheckbox, NCheckboxGroup, NEmpty, NForm, NFormItem, NGrid, NGridItem, NIcon, NInput, NInputNumber, NProgress, NSelect, NSlider, NSwitch } from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { SearchOutline, VideocamOutline, AddOutline, CloseOutline, ArrowForwardOutline, LayersOutline, ArrowUpOutline, ArrowDownOutline, ReorderFourOutline } from '@vicons/ionicons5'
import {
  getSystemConfig, updateSystemConfig,
  scrapeAllMetadata, getScrapeProgress,
  startProbe, stopProbe,
  startBackfill, stopBackfill,
  getBackfillConfig, updateBackfillConfig,
  resetBackfillQuality, resetBackfillEpisodeImage,
  getScrapeDefaults,
  type BackfillStage, type ScrapeStrategy, type FieldPriorityMap,
} from '@/api/client'
import { useTaskStream } from '@/composables/useTaskStream'

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

const tmdbApiKeys = ref<string[]>([''])
const tmdbLanguage = ref('zh-CN')
const tmdbProxy = ref('')
const scrapeSaveMode = ref('database')
const autoScrape = ref(false)
const showApiKey = ref(false)
const savingConfig = ref(false)
const scraping = ref(false)

// 任务进度改由 SSE 流驱动（useTaskStream 单例），替代以前的 setInterval 轮询。
// 下面的 computed 把统一的 TaskSnapshot 反向映射到模板沿用的旧字段名，
// 避免大面积改模板；等后续页面重构再收敛字段命名。
const { snapshots } = useTaskStream()

// 刮削已由 scrape_queue + ScrapeWorker 持续驱动,本页面不再展示进度。
// 这里只保留一个轻量 ref 用于显示"待刮削 N / 总 M"提示,点入队按钮后手动刷新。
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

const providerOptions = [
  { label: 'TMDB (基准源)', value: 'tmdb' },
  { label: 'TVDB (剧集/季海报)', value: 'tvdb' },
  { label: 'Bangumi (动画)', value: 'bangumi' },
  { label: '豆瓣 (中文补全)', value: 'douban' },
  { label: 'Fanart.tv (图片)', value: 'fanart' },
]
const defaultProviders = providerOptions.map((o) => o.value)
const providerLabel = (name: string) => providerOptions.find((o) => o.value === name)?.label || name
const providersEnabled = ref<string[]>([...defaultProviders])
const providerOrder = ref<string[]>([...defaultProviders])

function moveProvider(index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= providerOrder.value.length) return
  const next = [...providerOrder.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  providerOrder.value = next
}

const draggingIndex = ref<number | null>(null)
const dragOverIndex = ref<number | null>(null)

function onDragStart(index: number, e: DragEvent) {
  draggingIndex.value = index
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    // Firefox 需要 setData 才会触发 drag
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
const confidenceThreshold = ref<number>(0.72)
const autoApplyEnabled = ref(true)
const doubanEnabled = ref(true)
const bangumiUA = ref('')
const tvdbApiKey = ref('')
const tvdbPin = ref('')
const fanartApiKey = ref('')
const savingScrapeSources = ref(false)

// ===== 识别策略 + 字段级来源顺序(Phase 6) =====
const strategy = ref<ScrapeStrategy>('aggregated')
const strategyOptions: { label: string; value: ScrapeStrategy; desc: string }[] = [
  { label: '多源投票', value: 'aggregated', desc: '并发请求所有启用源,多源互投,准确度优先' },
  { label: '按顺序尝试', value: 'sequential', desc: '按优先级逐个尝试,首个过阈值即停,请求更少' },
]
const fieldNames = ref<string[]>([])
const defaultPolicy = ref<FieldPriorityMap>({})
const fieldPriority = ref<FieldPriorityMap>({})

const fieldLabels: Record<string, string> = {
  overview: '简介 Overview',
  title: '标题 Title',
  original_title: '原始标题 Original Title',
  tagline: '标语 Tagline',
  premiered: '首映日期 Premiered',
  year: '年份 Year',
  rating: '评分 Rating',
  actors: '演员 Actors',
  poster: '海报 Poster',
  backdrop: '背景图 Backdrop',
  season_poster: '季海报 Season Poster',
}
const fieldLabel = (n: string) => fieldLabels[n] || n

function moveField(field: string, idx: number, delta: number) {
  const cur = [...(fieldPriority.value[field] ?? [])]
  const target = idx + delta
  if (target < 0 || target >= cur.length) return
  ;[cur[idx], cur[target]] = [cur[target], cur[idx]]
  fieldPriority.value = { ...fieldPriority.value, [field]: cur }
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
const savingProbe = ref(false)

// ===== M7 Backfill 存量回填 =====
const backfillProgress = computed(() => {
  const s = snapshots.backfill
  if (!s) return null
  // 旧模板里的 "completed" / "stopped" 由 TaskStatus 反向映射。
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
    // 适配旧模板：completed/stopped 状态下 last_run_at = completedAt。
    last_run_at: s.completedAt ?? 0,
  }
})
const backfillConfig = ref<{ enabled_on_startup: boolean; episode_still_fetch: boolean }>({ enabled_on_startup: false, episode_still_fetch: true })
const backfillBusy = ref(false)
const stageLabel = (s: string) => ({ quality: '画质标签', name: 'Episode 标题', image: '分集缩略图' } as Record<string, string>)[s] || s
const backfillRunning = () => ['running', 'stopping'].includes(backfillProgress.value?.status ?? '')

// 保留签名以减少调用点改动：进度由 SSE 自动更新，这里是 no-op。
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

// 进度由 SSE 自动更新,这里只负责刮削计数的手动刷新(轻量调用)。
async function refreshTaskSummary() {
  await refreshScrapeSummary()
}

async function handleSaveConfig() {
  savingConfig.value = true
  try {
    await updateSystemConfig({
      tmdb_api_key: tmdbApiKeys.value.filter((k) => k.trim()).join(','),
      tmdb_language: tmdbLanguage.value,
      auto_scrape_enabled: String(autoScrape.value),
      tmdb_proxy: tmdbProxy.value,
      scrape_save_mode: scrapeSaveMode.value,
    })
    showToast('元数据设置已保存', 'success')
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    savingConfig.value = false
  }
}

async function handleSaveScrapeSources() {
  savingScrapeSources.value = true
  try {
    const priorityObj: Record<string, number> = {}
    providerOrder.value.forEach((name, i) => { priorityObj[name] = i + 1 })
    await updateSystemConfig({
      scrape_providers_enabled: JSON.stringify(providersEnabled.value),
      scrape_provider_priority: JSON.stringify(priorityObj),
      scrape_field_priority: JSON.stringify(fieldPriority.value),
      scrape_strategy: strategy.value,
      scrape_confidence_threshold: String(confidenceThreshold.value),
      scrape_auto_apply: String(autoApplyEnabled.value),
      douban_enabled: String(doubanEnabled.value),
      bangumi_ua: bangumiUA.value,
      tvdb_api_key: tvdbApiKey.value,
      tvdb_pin: tvdbPin.value,
      fanart_api_key: fanartApiKey.value,
    })
    showToast('刮削源设置已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  } finally {
    savingScrapeSources.value = false
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
        ? names
        : [...defaultProviders]
    } catch {
      providersEnabled.value = [...defaultProviders]
    }
    try {
      const savedPriority = cfg.scrape_provider_priority ? JSON.parse(cfg.scrape_provider_priority) : null
      if (savedPriority && typeof savedPriority === 'object') {
        const sorted = Object.keys(savedPriority)
          .filter((k) => typeof savedPriority[k] === 'number')
          .sort((a, b) => savedPriority[a] - savedPriority[b])
        const merged = [...sorted]
        for (const name of defaultProviders) {
          if (!merged.includes(name)) merged.push(name)
        }
        providerOrder.value = merged
      } else {
        providerOrder.value = [...defaultProviders]
      }
    } catch {
      providerOrder.value = [...defaultProviders]
    }
    const threshold = parseFloat(cfg.scrape_confidence_threshold)
    confidenceThreshold.value = Number.isFinite(threshold) && threshold > 0 && threshold <= 1 ? threshold : 0.72
    autoApplyEnabled.value = cfg.scrape_auto_apply !== 'false'
    doubanEnabled.value = cfg.douban_enabled !== 'false'
    bangumiUA.value = cfg.bangumi_ua || ''
    tvdbApiKey.value = cfg.tvdb_api_key || ''
    tvdbPin.value = cfg.tvdb_pin || ''
    fanartApiKey.value = cfg.fanart_api_key || ''
    // 策略
    const s = String(cfg.scrape_strategy || '').toLowerCase()
    strategy.value = s === 'sequential' ? 'sequential' : 'aggregated'
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
  }).catch(() => {})
  void refreshBackfillConfig()
})
</script>

<template>
  <page-shell title="元数据" :icon="AppIcons.metadata" description="TMDB 刮削与 FFprobe 探测配置" body-class="config-content">
    <div class="two-col">
      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box tmdb">
              <n-icon :size="18"><SearchOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">TMDB 刮削</div>
              <div class="header-desc">配置 API Key、语言与保存方式，用于自动补全媒体元数据。</div>
            </div>
          </div>
        </template>

        <div v-if="scrapeSummary" class="stats-grid">
          <div class="stat-box">
            <div class="stat-value">{{ scrapeSummary.missing_count }}</div>
            <div class="stat-name">待刮削<template v-if="scrapeSummary.items_total"> / {{ scrapeSummary.items_total }} 总项</template></div>
          </div>
        </div>
        <div class="hint-text" style="margin-bottom: 12px">
          点击下方按钮将缺失元数据的 Movie/Series 入队,实时进度请到 "观测中心 &gt; 队列管道" 查看。
        </div>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">访问密钥</div>
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
              <n-button quaternary size="tiny" @click="showApiKey = !showApiKey">{{ showApiKey ? '隐藏 Key' : '显示 Key' }}</n-button>
            </div>
            <div class="hint-text">支持多个 Key 轮询使用，避免单个 Key 触发风控。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">基础设置</div>
            <n-grid cols="1 m:2" x-gap="12" responsive="screen">
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
            </n-grid>
            <n-form-item label="代理">
              <n-input v-model:value="tmdbProxy" placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080" size="small" />
            </n-form-item>
            <div class="hint-text">支持 HTTP / HTTPS / SOCKS5，留空则直接访问 TMDB。</div>
          </div>

          <div class="subsection switch-section">
            <div class="switch-copy">
              <div class="switch-title">自动刮削</div>
              <div class="hint-text">新媒体入库时自动抓取元数据。</div>
            </div>
            <n-switch v-model:value="autoScrape" :round="false" />
          </div>
        </n-form>

        <div class="card-actions">
          <n-button type="primary" size="small" :loading="savingConfig" @click="handleSaveConfig">保存设置</n-button>
          <n-button secondary size="small" :loading="scraping" :disabled="scraping || scrapeSummary?.missing_count === 0" @click="handleScrapeAll">刮削缺失元数据</n-button>
        </div>
      </n-card>

      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box tmdb">
              <n-icon :size="18"><LayersOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">刮削源</div>
              <div class="header-desc">多源聚合识别与字段填充。启用的源并发搜索并投票,命中阈值后按 Priority 合并字段。</div>
            </div>
          </div>
        </template>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">启用的源</div>
            <n-checkbox-group v-model:value="providersEnabled">
              <div class="provider-grid">
                <n-checkbox v-for="opt in providerOptions" :key="opt.value" :value="opt.value" :label="opt.label" />
              </div>
            </n-checkbox-group>
            <div class="hint-text">未配置 API Key 的源会自动跳过。至少保留 TMDB,否则无法写入 items.tmdb_id。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">优先级排序</div>
            <div class="priority-list">
              <div
                v-for="(name, idx) in providerOrder"
                :key="name"
                class="priority-row"
                :class="{
                  dragging: draggingIndex === idx,
                  'drag-over': dragOverIndex === idx && draggingIndex !== idx,
                }"
                draggable="true"
                @dragstart="onDragStart(idx, $event)"
                @dragover="onDragOver(idx, $event)"
                @drop="onDrop(idx, $event)"
                @dragend="onDragEnd"
                @dragleave="dragOverIndex === idx ? (dragOverIndex = null) : null"
              >
                <n-icon class="priority-handle"><ReorderFourOutline /></n-icon>
                <span class="priority-index">{{ idx + 1 }}</span>
                <span class="priority-label">{{ providerLabel(name) }}</span>
                <div class="priority-actions">
                  <n-button quaternary circle size="tiny" :disabled="idx === 0" @click="moveProvider(idx, -1)">
                    <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
                  </n-button>
                  <n-button quaternary circle size="tiny" :disabled="idx === providerOrder.length - 1" @click="moveProvider(idx, 1)">
                    <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
                  </n-button>
                </div>
              </div>
            </div>
            <div class="hint-text">拖拽或使用箭头调整顺序。数字越小越优先,决定识别互投的主源与字段合并的回落顺序。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">识别阈值</div>
            <n-grid cols="1 m:3" x-gap="12" responsive="screen">
              <n-grid-item :span="2">
                <n-slider v-model:value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" :tooltip="true" />
              </n-grid-item>
              <n-grid-item>
                <n-input-number v-model:value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" size="small" />
              </n-grid-item>
            </n-grid>
            <div class="hint-text">单源候选 ≥ 阈值直接采纳;多源互投(≥2)可低于阈值。推荐 0.72。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">识别策略</div>
            <div class="strategy-options">
              <label
                v-for="opt in strategyOptions"
                :key="opt.value"
                class="strategy-row"
                :class="{ active: strategy === opt.value }"
              >
                <input type="radio" :value="opt.value" v-model="strategy" />
                <div class="strategy-copy">
                  <div class="strategy-name">{{ opt.label }}</div>
                  <div class="strategy-desc">{{ opt.desc }}</div>
                </div>
              </label>
            </div>
          </div>

          <div class="subsection">
            <div class="subsection-title">
              字段来源顺序
              <n-button size="tiny" quaternary @click="resetFieldPriority">重置为默认</n-button>
            </div>
            <div class="field-priority-list">
              <div v-for="f in fieldNames" :key="f" class="field-priority-row">
                <div class="field-priority-label">{{ fieldLabel(f) }}</div>
                <div class="field-priority-pills">
                  <div
                    v-for="(pname, pidx) in (fieldPriority[f] || [])"
                    :key="pname"
                    class="field-priority-pill"
                  >
                    <span class="pill-order">{{ pidx + 1 }}</span>
                    <span class="pill-name">{{ providerLabel(pname) }}</span>
                    <n-button
                      quaternary
                      circle
                      size="tiny"
                      :disabled="pidx === 0"
                      @click="moveField(f, pidx, -1)"
                    >
                      <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
                    </n-button>
                    <n-button
                      quaternary
                      circle
                      size="tiny"
                      :disabled="pidx === (fieldPriority[f] || []).length - 1"
                      @click="moveField(f, pidx, 1)"
                    >
                      <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
                    </n-button>
                  </div>
                  <div v-if="!(fieldPriority[f] || []).length" class="hint-text">无启用源</div>
                </div>
              </div>
            </div>
            <div class="hint-text">每个字段按左起第一个启用源的优先,不可用再落下一个。评分推荐"豆瓣优先",图片推荐"TVDB 优先"。</div>
          </div>

          <div class="subsection switch-section">
            <div class="switch-copy">
              <div class="switch-title">自动采纳</div>
              <div class="hint-text">关闭后低于阈值的候选进入人工确认队列,不写 items。</div>
            </div>
            <n-switch v-model:value="autoApplyEnabled" :round="false" />
          </div>

          <div class="subsection switch-section">
            <div class="switch-copy">
              <div class="switch-title">豆瓣补全</div>
              <div class="hint-text">非官方 API,仅作中文补全。触发风控会自动熔断 10 分钟。</div>
            </div>
            <n-switch v-model:value="doubanEnabled" :round="false" />
          </div>

          <div class="subsection">
            <div class="subsection-title">TVDB 凭证</div>
            <n-grid cols="1 m:2" x-gap="12" responsive="screen">
              <n-grid-item>
                <n-form-item label="API Key">
                  <n-input v-model:value="tvdbApiKey" type="password" placeholder="订阅 TVDB 后填入,留空则禁用" size="small" show-password-on="click" />
                </n-form-item>
              </n-grid-item>
              <n-grid-item>
                <n-form-item label="Pin (可选)">
                  <n-input v-model:value="tvdbPin" placeholder="TVDB 用户 Pin" size="small" />
                </n-form-item>
              </n-grid-item>
            </n-grid>
          </div>

          <div class="subsection">
            <div class="subsection-title">Fanart.tv API Key</div>
            <n-input v-model:value="fanartApiKey" type="password" placeholder="留空则禁用图片补充" size="small" show-password-on="click" />
            <div class="hint-text">只参与图片补充(poster / backdrop / seasonposter),不参与识别。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">Bangumi UA</div>
            <n-input v-model:value="bangumiUA" placeholder="留空使用默认 fyms/1.0" size="small" />
            <div class="hint-text">Bangumi 要求请求带 UA 注明来源。</div>
          </div>
        </n-form>

        <div class="card-actions">
          <n-button type="primary" size="small" :loading="savingScrapeSources" @click="handleSaveScrapeSources">保存刮削源设置</n-button>
          <n-button secondary size="small" @click="router.push({ name: 'media_unmatched' })">
            <template #icon><n-icon><ArrowForwardOutline /></n-icon></template>
            未匹配面板
          </n-button>
        </div>
      </n-card>

      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box probe">
              <n-icon :size="18"><VideocamOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">媒体信息探测</div>
              <div class="header-desc">对缺少媒体信息的 `strm` 执行 ffprobe 探测，补充视频与音频流信息。</div>
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
            <div class="icon-box tmdb">
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
    </div><!-- /two-col -->
  </page-shell>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
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
  min-height: 100%;
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

.config-form {
  margin-top: 16px;
  flex: 1;
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

.hint-text {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.5;
  margin-top: 6px;
  opacity: 0.8;
}

.api-key-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
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
  margin-top: 6px;
  flex-wrap: wrap;
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px 16px;
  margin-top: 4px;
}

.priority-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 4px;
}
.priority-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid transparent;
  cursor: grab;
  transition: background 0.15s, border-color 0.15s, opacity 0.15s;
  user-select: none;
}
.priority-row:hover {
  background: rgba(255, 255, 255, 0.05);
}
.priority-row:active {
  cursor: grabbing;
}
.priority-row.dragging {
  opacity: 0.4;
}
.priority-row.drag-over {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.08);
}
.priority-handle {
  color: var(--app-text-muted);
  opacity: 0.6;
  font-size: 14px;
  flex-shrink: 0;
}
.priority-row:hover .priority-handle {
  opacity: 1;
}
.priority-index {
  font-variant-numeric: tabular-nums;
  font-size: 11px;
  font-weight: 600;
  color: var(--app-text-muted);
  width: 18px;
  text-align: right;
}
.priority-label {
  flex: 1;
  font-size: 13px;
  color: var(--app-text);
}
.priority-actions {
  display: flex;
  gap: 2px;
}
.priority-actions :deep(.n-button) {
  cursor: pointer;
}

/* 识别策略单选 */
.strategy-options {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.strategy-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 4px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}
.strategy-row:hover {
  background: rgba(255, 255, 255, 0.04);
}
.strategy-row.active {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.08);
}
.strategy-row input[type='radio'] {
  margin-top: 3px;
  accent-color: var(--app-primary);
}
.strategy-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.strategy-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
}
.strategy-desc {
  font-size: 11px;
  color: var(--app-text-muted);
}

/* 字段级优先级 */
.field-priority-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}
.field-priority-row {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: 10px;
  align-items: center;
  padding: 6px 10px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.02);
}
.field-priority-label {
  font-size: 12px;
  color: var(--app-text-muted);
}
.field-priority-pills {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.field-priority-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 4px 2px 8px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
  font-size: 11px;
  color: var(--app-text);
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

.switch-section {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

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

.card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 14px;
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
  margin-top: auto;
}

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

.stat-value.ok {
  color: #22c55e;
}

.stat-value.err {
  color: var(--app-danger, #e53935);
}

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
