<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NCard, NModal, NProgress, NIcon, NTag, NSelect, NAlert, NCollapse, NCollapseItem, NDivider, NSpin } from 'naive-ui'
import {
  BarChartOutline,
  FlashOutline,
  ServerOutline,
  WarningOutline,
  RefreshOutline,
  CloudDownloadOutline,
  ArrowForwardOutline,
  PeopleOutline,
  LibraryOutline,
  CopyOutline,
  ShieldCheckmarkOutline,
  HardwareChipOutline,
  FilmOutline,
} from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import MiniSparkline from '@/components/MiniSparkline.vue'
import SystemMetricsRow from '@/components/SystemMetricsRow.vue'
import TaskCenterCard from '@/components/TaskCenterCard.vue'
import { useTaskStream } from '@/composables/useTaskStream'
import {
  getDailyStats,
  getGatewayConfig,
  type DailyStat,
  type GatewayConfig,
} from '@/api/gateway'
import {
  getSystemInfo,
  getActiveSessions,
  getLibraries,
  getItemCounts,
  restartServer,
  shutdownServer,
  getUpdateStatus,
  checkForUpdate,
  applyUpdate,
  setUpdateChannel,
  type UpdateStatus,
  type ItemCounts,
} from '@/api/client'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const { showToast } = useToast()

const loading = ref(true)
const loadError = ref('')
const dailyStats = ref<DailyStat[]>([])
const gatewayConfig = ref<GatewayConfig | null>(null)

const serverInfo = ref<any>(null)
const sessions = ref<any[]>([])
const libraries = ref<any[]>([])
const itemCounts = ref<ItemCounts | null>(null)

// scan 进度由 SSE 流驱动；保留旧字段命名，让"扫描进度"卡片与 activeScanCount 无需改动。
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
const showRestart = ref(false)
const showShutdown = ref(false)
const showUpdateConfirm = ref(false)
const checkingUpdate = ref(false)
const applyingUpdate = ref(false)
const updateConnectionLost = ref(false)
const updateChannel = ref<'stable' | 'beta'>('stable')
const updateStatus = ref<UpdateStatus | null>(null)
const updateChannelOptions = [
  { label: '稳定版', value: 'stable' },
  { label: '测试版', value: 'beta' },
]

// ───────── Aggregates ─────────
const totalRequests = computed(() =>
  dailyStats.value.reduce((s, d) => s + (d.requests ?? 0), 0),
)
const totalRedirects = computed(() =>
  dailyStats.value.reduce((s, d) => s + (d.redirects302 ?? 0), 0),
)
const totalErrors = computed(() =>
  dailyStats.value.reduce((s, d) => s + (d.status4xx ?? 0) + (d.status5xx ?? 0), 0),
)
const errorRate = computed(() => {
  const total = totalRequests.value
  if (!total) return '0%'
  return ((totalErrors.value / total) * 100).toFixed(1) + '%'
})
const activeSources = computed(() => {
  const sources = gatewayConfig.value?.sources
  if (!Array.isArray(sources)) return 0
  return sources.filter((s) => s.enabled).length
})
const totalSources = computed(() => gatewayConfig.value?.sources?.length || 0)
const totalLibraries = computed(() => libraries.value.length)
const totalMediaItems = computed(() => {
  const c = itemCounts.value
  if (!c) return 0
  return (c.MovieCount || 0) + (c.SeriesCount || 0) + (c.EpisodeCount || 0)
})
const mediaBreakdown = computed(() => {
  const c = itemCounts.value
  if (!c) return '电影 0 · 剧集 0 · 单集 0'
  return `电影 ${c.MovieCount ?? 0} · 剧集 ${c.SeriesCount ?? 0} · 单集 ${c.EpisodeCount ?? 0}`
})

// ───────── Real-time counts ─────────
const activeSessionCount = computed(() => sessions.value.length)
const activeScanCount = computed(
  () => scanProgress.value.filter((sp) => sp.Status === 'scanning').length,
)

// ───────── Sparklines (7-day series) ─────────
const requestsSeries = computed(() => dailyStats.value.map((d) => d.requests || 0))
const redirectsSeries = computed(() => dailyStats.value.map((d) => d.redirects302 || 0))
const errorsSeries = computed(() =>
  dailyStats.value.map((d) => (d.status4xx || 0) + (d.status5xx || 0)),
)

// ───────── Hero status ─────────
const isRunning = computed(() => !!serverInfo.value)
const runStatusText = computed(() => (isRunning.value ? '已运行' : '未连接'))
const fullServerId = computed(() => {
  const id = serverInfo.value?.Id
  return id ? formatServerId(id) : ''
})

async function copyServerId() {
  const id = fullServerId.value
  if (!id) return
  try {
    await navigator.clipboard.writeText(id)
    showToast('服务器 ID 已复制', 'success')
  } catch {
    showToast('复制失败，请手动选中', 'error')
  }
}

// ───────── Helpers ─────────
function libNameForScan(libId: string) {
  return libraries.value.find((l: any) => l.ItemId === libId)?.Name || libId
}

function formatVersion(info: any): string {
  const ver = info.Version || 'dev'
  const commit = info.BuildCommit
  if (commit) return `${ver} (${commit.substring(0, 7)})`
  return ver
}

function formatServerId(id: string | undefined): string {
  if (!id) return '-'
  if (id.length === 32 && !id.includes('-')) {
    return `${id.slice(0, 8)}-${id.slice(8, 12)}-${id.slice(12, 16)}-${id.slice(16, 20)}-${id.slice(20)}`
  }
  return id
}

function isUpdateBusy(status?: string) {
  return ['checking', 'backing_up', 'pulling', 'recreating', 'restarting'].includes(status || '')
}

function formatUpdateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const updateBadgeType = computed(() => {
  const status = updateStatus.value?.status
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'error'
  if (updateStatus.value?.hasUpdate) return 'warning'
  return 'info'
})

const updateStatusText = computed(() => {
  const status = updateStatus.value
  if (!status) return '未检查'
  return status.message || status.status || '未知状态'
})

const updateLogLines = computed(() => updateStatus.value?.logs || [])

// ───────── Actions ─────────
async function handleRestart() {
  showRestart.value = false
  try {
    await restartServer()
    showToast('服务器正在重启...', 'success')
  } catch {
    showToast('重启服务器失败', 'error')
  }
}

async function handleShutdown() {
  showShutdown.value = false
  try {
    await shutdownServer()
    showToast('服务器正在关闭...', 'success')
  } catch {
    showToast('关闭服务器失败', 'error')
  }
}

async function loadUpdateStatus() {
  try {
    const status = await getUpdateStatus()
    updateStatus.value = status
    updateChannel.value = (status.channel as 'stable' | 'beta') || 'stable'
    if (status.status === 'completed' || status.status === 'failed' || status.status === 'idle' || status.status === 'available') {
      updateConnectionLost.value = false
    }
  } catch (err: any) {
    if (isUpdateBusy(updateStatus.value?.status)) {
      updateConnectionLost.value = true
      return
    }
    throw err
  }
}

async function handleCheckUpdate() {
  checkingUpdate.value = true
  try {
    updateStatus.value = await checkForUpdate()
    updateChannel.value = (updateStatus.value.channel as 'stable' | 'beta') || 'stable'
    showToast(updateStatus.value.hasUpdate ? '发现新版本' : '当前已是最新版本', 'success')
  } catch (err: any) {
    showToast(err?.message || '检查更新失败', 'error')
  } finally {
    checkingUpdate.value = false
  }
}

async function handleChangeUpdateChannel(value: 'stable' | 'beta') {
  try {
    updateStatus.value = await setUpdateChannel(value)
    updateChannel.value = value
    showToast('更新通道已保存', 'success')
  } catch (err: any) {
    showToast(err?.message || '保存更新通道失败', 'error')
  }
}

async function handleApplyUpdate() {
  showUpdateConfirm.value = false
  applyingUpdate.value = true
  updateConnectionLost.value = false
  try {
    updateStatus.value = await applyUpdate(['settings', 'users', 'libraries', 'media'])
    showToast('更新任务已启动，服务会短暂重启', 'success')
  } catch (err: any) {
    showToast(err?.message || '启动更新失败', 'error')
  } finally {
    applyingUpdate.value = false
  }
}

// ───────── Data loading ─────────
const timers: ReturnType<typeof setInterval>[] = []

async function loadGatewayData() {
  const [stats, config] = await Promise.allSettled([
    getDailyStats({ days: '7' }),
    getGatewayConfig(),
  ])

  if (stats.status === 'fulfilled' && Array.isArray(stats.value)) {
    dailyStats.value = stats.value
  }
  if (config.status === 'fulfilled' && config.value) {
    gatewayConfig.value = config.value
  }

  const allFailed = [stats, config].every((r) => r.status === 'rejected')
  if (allFailed) {
    loadError.value = '无法连接到网关服务，部分数据可能不可用'
  }
}

async function loadServerData() {
  const [info, sess, libs, counts, update] = await Promise.allSettled([
    getSystemInfo(),
    getActiveSessions(),
    getLibraries(),
    getItemCounts(),
    getUpdateStatus(),
  ])

  if (info.status === 'fulfilled') serverInfo.value = info.value
  if (sess.status === 'fulfilled' && Array.isArray(sess.value)) sessions.value = sess.value
  if (libs.status === 'fulfilled' && Array.isArray(libs.value)) libraries.value = libs.value
  if (counts.status === 'fulfilled' && counts.value) itemCounts.value = counts.value
  if (update.status === 'fulfilled') {
    updateStatus.value = update.value
    updateChannel.value = (update.value.channel as 'stable' | 'beta') || 'stable'
  }
}

async function loadAll() {
  loading.value = true
  loadError.value = ''
  await Promise.allSettled([loadGatewayData(), loadServerData()])
  loading.value = false
}

onMounted(() => {
  loadAll()

  timers.push(
    setInterval(() => {
      getActiveSessions()
        .then((s) => { if (Array.isArray(s)) sessions.value = s })
        .catch(() => {})
    }, 5000),
    setInterval(() => {
      loadUpdateStatus().catch(() => {})
    }, 4000),
  )
})

onUnmounted(() => {
  timers.forEach((t) => clearInterval(t))
})
</script>

<template>
  <page-shell title="总览" description="系统与网关状态一览">
    <div v-if="loadError" class="overview-error-banner">
      <n-icon :component="WarningOutline" :size="18" />
      <span>{{ loadError }}</span>
      <n-button text size="small" @click="loadAll" style="margin-left: auto">
        <template #icon><n-icon :component="RefreshOutline" /></template>
        重试
      </n-button>
    </div>

    <n-spin :show="loading">
      <!-- ① Hero 信息带：服务器信息 + 运行状态 + 全局操作按钮 -->
      <section class="hero-bar">
        <div class="hero-left">
          <div class="hero-icon">
            <n-icon :component="ServerOutline" :size="22" />
          </div>
          <div class="hero-meta">
            <div class="hero-title">
              <span class="hero-name">{{ serverInfo?.ServerName || 'FYMS' }}</span>
              <code class="hero-version">{{ serverInfo ? formatVersion(serverInfo) : 'dev' }}</code>
              <span v-if="serverInfo?.OperatingSystemDisplayName || serverInfo?.OperatingSystem" class="hero-os">
                · {{ serverInfo.OperatingSystemDisplayName || serverInfo.OperatingSystem }}
              </span>
              <span class="hero-status">
                <span class="status-dot" :class="isRunning ? 'is-online' : 'is-error'"></span>
                {{ runStatusText }}
              </span>
            </div>
            <div class="hero-sub">
              <span v-if="fullServerId" class="hero-sub-item hero-id">
                <span class="hero-id-label">ID</span>
                <code class="hero-id-value">{{ fullServerId }}</code>
                <button
                  class="hero-id-copy"
                  type="button"
                  aria-label="复制服务器 ID"
                  @click.stop="copyServerId"
                >
                  <n-icon :component="CopyOutline" :size="13" />
                </button>
              </span>
              <span v-if="serverInfo?.LocalAddress" class="hero-sub-item">· {{ serverInfo.LocalAddress }}</span>
              <span v-if="updateStatus?.hasUpdate" class="hero-update-hint">
                · <n-icon :component="CloudDownloadOutline" :size="13" /> 有新版本 v{{ updateStatus.latestVersion }}
              </span>
            </div>
          </div>
        </div>
        <div class="hero-actions">
          <n-button size="small" secondary :loading="checkingUpdate" @click="handleCheckUpdate">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            检查更新
          </n-button>
          <n-button
            size="small"
            type="primary"
            :disabled="!updateStatus?.hasUpdate || isUpdateBusy(updateStatus?.status)"
            :loading="applyingUpdate"
            @click="showUpdateConfirm = true"
          >
            <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            立即更新
          </n-button>
          <n-divider vertical />
          <n-button size="small" secondary type="warning" @click="showRestart = true">重启</n-button>
          <n-button size="small" secondary type="error" @click="showShutdown = true">关闭</n-button>
        </div>
      </section>

      <!-- ①b 系统资源（CPU / RAM / Network） -->
      <system-metrics-row />

      <!-- ②a 网关观测 -->
      <section class="kpi-group">
        <header class="kpi-group-head">
          <span class="kpi-group-title">
            <n-icon :component="ShieldCheckmarkOutline" :size="15" />
            网关观测
          </span>
          <n-button
            text
            size="tiny"
            class="kpi-group-more"
            @click="router.push({ name: 'obs_gw_traffic' })"
          >
            查看详情
            <template #icon><n-icon :component="ArrowForwardOutline" /></template>
          </n-button>
        </header>
        <div class="kpi-row">
          <div
            class="kpi-cell kpi-clickable"
            data-type="primary"
            @click="router.push({ name: 'obs_gw_traffic' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">总请求数</span>
              <span class="kpi-icon-box"><n-icon :component="BarChartOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ totalRequests.toLocaleString() }}</div>
            <div class="kpi-foot">
              <mini-sparkline :data="requestsSeries" color="var(--app-primary)" :width="76" :height="22" />
              <span class="kpi-hint">7 天</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="BarChartOutline" /></span>
          </div>

          <div
            class="kpi-cell kpi-clickable"
            data-type="success"
            @click="router.push({ name: 'obs_gw_redirect' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">302 重定向</span>
              <span class="kpi-icon-box"><n-icon :component="FlashOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ totalRedirects.toLocaleString() }}</div>
            <div class="kpi-foot">
              <mini-sparkline :data="redirectsSeries" color="var(--app-success)" :width="76" :height="22" />
              <span class="kpi-hint">7 天</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="FlashOutline" /></span>
          </div>

          <div
            class="kpi-cell kpi-clickable"
            data-type="warning"
            @click="router.push({ name: 'obs_gw_traffic' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">错误数</span>
              <span class="kpi-icon-box"><n-icon :component="WarningOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ totalErrors.toLocaleString() }}</div>
            <div class="kpi-foot">
              <mini-sparkline :data="errorsSeries" color="var(--app-warning)" :width="76" :height="22" />
              <span class="kpi-hint">{{ errorRate }} · 4xx+5xx</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="WarningOutline" /></span>
          </div>

          <div
            class="kpi-cell kpi-clickable"
            data-type="info"
            @click="router.push({ name: 'gateway_emby_sources' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">活跃源</span>
              <span class="kpi-icon-box"><n-icon :component="ServerOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">
              {{ activeSources }}<span class="kpi-value-sub">/ {{ totalSources }}</span>
            </div>
            <div class="kpi-foot">
              <span class="kpi-hint">已启用 Emby 源</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="ServerOutline" /></span>
          </div>
        </div>
      </section>

      <!-- ②b 服务观测 -->
      <section class="kpi-group">
        <header class="kpi-group-head">
          <span class="kpi-group-title">
            <n-icon :component="HardwareChipOutline" :size="15" />
            服务观测
          </span>
          <n-button
            text
            size="tiny"
            class="kpi-group-more"
            @click="router.push({ name: 'obs_svc_playback' })"
          >
            查看详情
            <template #icon><n-icon :component="ArrowForwardOutline" /></template>
          </n-button>
        </header>
        <div class="kpi-row">
          <div class="kpi-cell" data-type="success">
            <div class="kpi-head">
              <span class="kpi-title">
                活跃会话
                <span class="live-pill">LIVE</span>
              </span>
              <span class="kpi-icon-box"><n-icon :component="PeopleOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ activeSessionCount }}</div>
            <div class="kpi-foot">
              <span class="kpi-hint">{{ activeSessionCount > 0 ? '正在播放' : '无活动连接' }}</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="PeopleOutline" /></span>
          </div>

          <div
            class="kpi-cell kpi-clickable"
            :data-type="activeScanCount > 0 ? 'warning' : 'info'"
            @click="router.push({ name: 'media_libraries' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">扫描中</span>
              <span class="kpi-icon-box"><n-icon :component="LibraryOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ activeScanCount }}</div>
            <div class="kpi-foot">
              <span class="kpi-hint">{{ activeScanCount > 0 ? '正在扫描媒体库' : '无扫描任务' }}</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="LibraryOutline" /></span>
          </div>

          <div
            class="kpi-cell kpi-clickable"
            data-type="info"
            @click="router.push({ name: 'media_libraries' })"
          >
            <div class="kpi-head">
              <span class="kpi-title">媒体库数</span>
              <span class="kpi-icon-box"><n-icon :component="LibraryOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ totalLibraries }}</div>
            <div class="kpi-foot">
              <span class="kpi-hint">已配置媒体库</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="LibraryOutline" /></span>
          </div>

          <div class="kpi-cell" data-type="primary">
            <div class="kpi-head">
              <span class="kpi-title">媒体项总数</span>
              <span class="kpi-icon-box"><n-icon :component="FilmOutline" :size="16" /></span>
            </div>
            <div class="kpi-value">{{ totalMediaItems.toLocaleString() }}</div>
            <div class="kpi-foot">
              <span class="kpi-hint">{{ mediaBreakdown }}</span>
            </div>
            <span class="kpi-bg-icon"><n-icon :component="FilmOutline" /></span>
          </div>
        </div>
      </section>

      <!-- ③ 双栏主区 -->
      <div class="main-grid">
        <!-- 左栏：会话 + 扫描进度 -->
        <div class="main-col">
          <n-card class="section-card" title="活动会话" size="small">
            <template #header-extra>
              <span class="subtle-count">{{ activeSessionCount }}</span>
            </template>
            <div v-if="sessions.length === 0" class="empty-state">
              <n-icon :component="PeopleOutline" :size="22" />
              <span>暂无活动会话</span>
            </div>
            <ul v-else class="session-list">
              <li v-for="s in sessions" :key="s.Id" class="session-item">
                <div class="session-avatar">{{ (s.UserName || '?')[0].toUpperCase() }}</div>
                <div class="session-main">
                  <div class="session-row">
                    <span class="session-name">{{ s.UserName }}</span>
                    <span class="session-meta">{{ s.Client }}{{ s.ApplicationVersion ? ' · ' + s.ApplicationVersion : '' }} · {{ s.DeviceName }}</span>
                  </div>
                </div>
                <code class="session-ip">{{ s.RemoteEndPoint || '-' }}</code>
              </li>
            </ul>
          </n-card>

          <task-center-card />

          <n-card v-if="scanProgress.length > 0" class="section-card" title="扫描进度" size="small">
            <template #header-extra>
              <span class="subtle-count">{{ scanProgress.length }}</span>
            </template>
            <ul class="scan-list">
              <li v-for="sp in scanProgress" :key="sp.LibraryId" class="scan-item">
                <div class="scan-name">{{ libNameForScan(sp.LibraryId) }}</div>
                <template v-if="sp.Status === 'scanning'">
                  <n-progress type="line" :percentage="sp.Percentage" :show-indicator="false" class="scan-bar" />
                  <span class="scan-pct">{{ sp.Percentage }}%</span>
                  <span class="scan-detail">{{ sp.ProcessedItems }}/{{ sp.TotalItems }}</span>
                </template>
                <span v-else-if="sp.Status === 'completed'" class="scan-tag scan-tag-ok">已完成</span>
                <span v-else-if="sp.Status === 'failed'" class="scan-tag scan-tag-err">失败 · {{ sp.Error }}</span>
                <span v-else class="scan-tag">{{ sp.Status }}</span>
              </li>
            </ul>
          </n-card>
        </div>

        <!-- 右栏：应用更新 -->
        <div class="main-col">
          <n-card class="section-card" title="应用更新" size="small">
            <template #header-extra>
              <n-select
                :value="updateChannel"
                :options="updateChannelOptions"
                size="tiny"
                style="width: 96px"
                :disabled="checkingUpdate || applyingUpdate || isUpdateBusy(updateStatus?.status)"
                @update:value="handleChangeUpdateChannel"
              />
            </template>

            <!-- 版本号对比 -->
            <div class="update-ver">
              <div class="ver-item">
                <span class="ver-label">当前</span>
                <strong>{{ updateStatus?.currentVersion || serverInfo?.Version || 'dev' }}</strong>
              </div>
              <n-icon :component="ArrowForwardOutline" :size="18" class="ver-arrow" />
              <div class="ver-item">
                <span class="ver-label">最新</span>
                <strong>{{ updateStatus?.latestVersion || '-' }}</strong>
                <n-tag size="small" :type="updateBadgeType as any" round :bordered="false">{{ updateStatusText }}</n-tag>
              </div>
            </div>

            <n-alert v-if="updateStatus?.needsDockerSocket" type="warning" size="small" class="update-alert">
              启用应用内自更新需要为容器挂载 Docker Socket，并保证 `/app/data` 为持久化目录。
            </n-alert>
            <n-alert v-if="updateConnectionLost" type="info" size="small" class="update-alert">
              更新过程中连接短暂中断是正常现象，页面会持续轮询服务恢复状态。
            </n-alert>
            <n-alert v-if="updateStatus?.error" type="error" size="small" class="update-alert">
              {{ updateStatus.error }}
            </n-alert>

            <!-- 紧凑 meta：横向 key-value -->
            <dl class="update-meta-row">
              <div>
                <dt>镜像</dt>
                <dd><code>{{ updateStatus?.targetImage || '-' }}</code></dd>
              </div>
              <div>
                <dt>最近检查</dt>
                <dd>{{ formatUpdateTime(updateStatus?.lastCheckedAt) }}</dd>
              </div>
              <div>
                <dt>最近完成</dt>
                <dd>{{ formatUpdateTime(updateStatus?.completedAt) }}</dd>
              </div>
              <div>
                <dt>更新日志</dt>
                <dd>
                  <a
                    v-if="updateStatus?.releaseNotesUrl"
                    :href="updateStatus.releaseNotesUrl"
                    target="_blank"
                    rel="noreferrer"
                    class="update-link"
                  >查看</a>
                  <span v-else>-</span>
                </dd>
              </div>
            </dl>

            <n-progress
              v-if="isUpdateBusy(updateStatus?.status)"
              type="line"
              :percentage="85"
              :show-indicator="false"
              status="warning"
              class="update-progress"
            />

            <n-collapse v-if="updateLogLines.length" class="update-log-collapse">
              <n-collapse-item title="实时日志" name="log">
                <div class="update-log">
                  <div v-for="line in updateLogLines" :key="line" class="update-log-line">{{ line }}</div>
                </div>
              </n-collapse-item>
            </n-collapse>
          </n-card>
        </div>
      </div>
    </n-spin>

    <!-- Modals -->
    <n-modal v-model:show="showRestart" preset="dialog" title="重启服务器" type="warning" positive-text="确认重启" negative-text="取消" @positive-click="handleRestart" @negative-click="showRestart = false">
      确定要重启服务器吗？所有活动连接将被断开。
    </n-modal>
    <n-modal v-model:show="showShutdown" preset="dialog" title="关闭服务器" type="error" positive-text="确认关闭" negative-text="取消" @positive-click="handleShutdown" @negative-click="showShutdown = false">
      确定要关闭服务器吗？服务器将完全停止运行，您需要手动重新启动。
    </n-modal>
    <n-modal v-model:show="showUpdateConfirm" preset="dialog" title="立即更新" type="warning" positive-text="开始更新" negative-text="取消" @positive-click="handleApplyUpdate" @negative-click="showUpdateConfirm = false">
      更新前会自动创建备份，并通过 Docker 拉取新镜像后重建当前容器。过程中服务会短暂中断。
    </n-modal>
  </page-shell>
</template>

<style scoped>
/* ───────── Shared ───────── */
.overview-error-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  margin-bottom: 12px;
  background: rgba(239, 68, 68, 0.08);
  border: 1px solid rgba(239, 68, 68, 0.2);
  border-radius: var(--app-radius);
  color: #f87171;
  font-size: 14px;
}

/* ───────── ① Hero bar ───────── */
.hero-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 14px 20px;
  margin-bottom: 16px;
  background: var(--app-card-bg);
  border: 1px solid var(--app-card-border);
  border-radius: var(--app-radius-card);
  box-shadow: var(--app-shadow-1);
  position: relative;
  overflow: hidden;
}
.hero-bar::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: linear-gradient(180deg, var(--app-primary), transparent);
}

.hero-left {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  flex: 1;
}
.hero-icon {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: var(--app-primary-soft);
  color: var(--app-primary);
  flex-shrink: 0;
}
.hero-meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.hero-title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-size: 15px;
  color: var(--app-text);
}
.hero-name { font-weight: 700; font-size: 16px; letter-spacing: -0.01em; }
.hero-version {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--app-primary-soft);
  color: var(--app-primary);
  font-weight: 600;
}
.hero-os {
  font-size: 13px;
  color: var(--app-text-muted);
}
.hero-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-text-muted);
  padding-left: 4px;
}
.hero-sub {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12px;
  color: var(--app-text-muted);
}
.hero-sub-item { white-space: nowrap; }
.hero-update-hint {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--app-warning);
  font-weight: 500;
}

/* Hero server ID with copy button */
.hero-id {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.hero-id-label { color: var(--app-text-muted); }
.hero-id-value {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
  color: var(--app-text-muted);
  padding: 1px 7px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: 4px;
  user-select: all;
}
.hero-id-copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--app-text-muted);
  cursor: pointer;
  opacity: 0.55;
  transition: opacity 0.15s ease, background 0.15s ease, color 0.15s ease;
}
.hero-id-copy:hover {
  opacity: 1;
  background: var(--app-primary-soft);
  color: var(--app-primary);
}
.hero-id-copy:active { transform: scale(0.94); }

.hero-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

/* ───────── ② KPI groups ───────── */
.kpi-group {
  margin-bottom: 16px;
}
.kpi-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2px;
  margin-bottom: 10px;
}
.kpi-group-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text-muted);
  letter-spacing: 0.02em;
}
.kpi-group-title :deep(.n-icon) {
  color: var(--app-primary);
  opacity: 0.85;
}
.kpi-group-more :deep(.n-icon) {
  font-size: 12px;
}
.kpi-group-more :deep(.n-button__content) {
  gap: 4px;
}

.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.kpi-cell {
  position: relative;
  overflow: hidden;
  background: var(--app-card-bg);
  border: 1px solid var(--app-card-border);
  border-radius: 12px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1),
              border-color 0.25s ease,
              box-shadow 0.25s ease;
  min-width: 0;
}
.kpi-cell.kpi-clickable { cursor: pointer; }

.kpi-head {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: var(--app-text-muted);
  font-weight: 500;
  min-height: 28px;
}
.kpi-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

/* 小图标框：不同 type 不同色，hover 放大并强化彩色阴影 */
.kpi-icon-box {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: var(--c-slate-100);
  color: var(--c-slate-500);
  flex-shrink: 0;
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1),
              box-shadow 0.3s ease;
}
.app-dark .kpi-icon-box {
  background: var(--c-slate-800);
  color: var(--c-slate-400);
}

.kpi-cell:hover .kpi-icon-box {
  transform: scale(1.1) rotate(5deg);
}

.kpi-value {
  position: relative;
  z-index: 2;
  font-size: 24px;
  font-weight: 700;
  color: var(--app-text);
  line-height: 1.1;
  letter-spacing: -0.02em;
  word-break: break-word;
  transition: transform 0.25s ease;
}
.kpi-cell:hover .kpi-value {
  transform: scale(1.03);
  transform-origin: left center;
}
.kpi-value-sub {
  margin-left: 4px;
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text-muted);
}
.kpi-foot {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 22px;
}
.kpi-hint {
  font-size: 11px;
  color: var(--app-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.live-pill {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.5px;
  padding: 1px 6px;
  border-radius: 4px;
  color: var(--app-success);
  background: rgba(16, 185, 129, 0.14);
}

/* 装饰大图标：右下角超大、低透明度、旋转。hover 时变大变亮 */
.kpi-bg-icon {
  position: absolute;
  right: -14px;
  bottom: -18px;
  display: block;
  pointer-events: none;
  z-index: 1;
  opacity: 0.06;
  color: currentColor;
  transform: rotate(-12deg);
  transition: opacity 0.3s ease, transform 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.kpi-bg-icon :deep(.n-icon),
.kpi-bg-icon > svg { font-size: 78px; }
.kpi-cell:hover .kpi-bg-icon {
  opacity: 0.11;
  transform: rotate(-8deg) scale(1.06);
}

/* ───── Type variants：渐变底、彩色图标框、彩色阴影 ───── */
.kpi-cell[data-type="primary"] {
  background: linear-gradient(135deg, rgba(var(--app-primary-rgb), 0.04) 0%, var(--app-card-bg) 100%);
  border-color: rgba(var(--app-primary-rgb), 0.22);
}
.kpi-cell[data-type="primary"] .kpi-icon-box {
  color: var(--app-primary);
  background: linear-gradient(135deg, rgba(var(--app-primary-rgb), 0.18), rgba(var(--app-primary-rgb), 0.08));
  box-shadow: 0 3px 10px rgba(var(--app-primary-rgb), 0.22);
}
.kpi-cell[data-type="primary"] .kpi-bg-icon { color: var(--app-primary); }
.kpi-cell[data-type="primary"]:hover {
  transform: translateY(-2px);
  border-color: rgba(var(--app-primary-rgb), 0.42);
  box-shadow: 0 12px 22px -6px rgba(var(--app-primary-rgb), 0.18);
}

.kpi-cell[data-type="success"] {
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.04) 0%, var(--app-card-bg) 100%);
  border-color: rgba(16, 185, 129, 0.22);
}
.kpi-cell[data-type="success"] .kpi-icon-box {
  color: var(--app-success);
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.18), rgba(16, 185, 129, 0.08));
  box-shadow: 0 3px 10px rgba(16, 185, 129, 0.22);
}
.kpi-cell[data-type="success"] .kpi-bg-icon { color: var(--app-success); }
.kpi-cell[data-type="success"]:hover {
  transform: translateY(-2px);
  border-color: rgba(16, 185, 129, 0.42);
  box-shadow: 0 12px 22px -6px rgba(16, 185, 129, 0.18);
}

.kpi-cell[data-type="warning"] {
  background: linear-gradient(135deg, rgba(245, 158, 11, 0.04) 0%, var(--app-card-bg) 100%);
  border-color: rgba(245, 158, 11, 0.22);
}
.kpi-cell[data-type="warning"] .kpi-icon-box {
  color: var(--app-warning);
  background: linear-gradient(135deg, rgba(245, 158, 11, 0.18), rgba(245, 158, 11, 0.08));
  box-shadow: 0 3px 10px rgba(245, 158, 11, 0.22);
}
.kpi-cell[data-type="warning"] .kpi-bg-icon { color: var(--app-warning); }
.kpi-cell[data-type="warning"]:hover {
  transform: translateY(-2px);
  border-color: rgba(245, 158, 11, 0.42);
  box-shadow: 0 12px 22px -6px rgba(245, 158, 11, 0.18);
}

.kpi-cell[data-type="info"] {
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.04) 0%, var(--app-card-bg) 100%);
  border-color: rgba(59, 130, 246, 0.22);
}
.kpi-cell[data-type="info"] .kpi-icon-box {
  color: var(--app-info);
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.18), rgba(59, 130, 246, 0.08));
  box-shadow: 0 3px 10px rgba(59, 130, 246, 0.22);
}
.kpi-cell[data-type="info"] .kpi-bg-icon { color: var(--app-info); }
.kpi-cell[data-type="info"]:hover {
  transform: translateY(-2px);
  border-color: rgba(59, 130, 246, 0.42);
  box-shadow: 0 12px 22px -6px rgba(59, 130, 246, 0.18);
}

/* ───────── ③ Main 2-col grid ───────── */
.main-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(320px, 1fr);
  gap: 16px;
}
.main-col {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}
.section-card { }
.subtle-count {
  font-size: 12px;
  color: var(--app-text-muted);
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--app-surface-2);
  font-weight: 500;
}
.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 16px;
  color: var(--app-text-muted);
  font-size: 13px;
}

/* Session list */
.session-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.session-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 6px;
  border-radius: 8px;
  transition: background 0.15s ease;
}
.session-item:hover { background: var(--app-surface-2); }
.session-avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--app-primary-soft);
  color: var(--app-primary);
  display: grid;
  place-items: center;
  font-size: 13px;
  font-weight: 700;
  flex-shrink: 0;
}
.session-main { flex: 1; min-width: 0; }
.session-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}
.session-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
}
.session-meta {
  font-size: 12px;
  color: var(--app-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.session-ip {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
  color: var(--app-text-muted);
  padding: 2px 6px;
  background: var(--app-surface-2);
  border-radius: 4px;
  flex-shrink: 0;
}

/* Scan list */
.scan-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.scan-item {
  display: grid;
  grid-template-columns: minmax(90px, 1fr) minmax(0, 2fr) auto auto;
  align-items: center;
  gap: 10px;
  font-size: 12px;
}
.scan-item > .scan-tag,
.scan-item > .n-progress {
  grid-column: 2 / -1;
}
.scan-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.scan-bar { grid-column: 2; }
.scan-pct { font-weight: 600; color: var(--app-primary); font-variant-numeric: tabular-nums; }
.scan-detail { color: var(--app-text-muted); font-variant-numeric: tabular-nums; }
.scan-tag {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  justify-self: start;
}
.scan-tag-ok { background: rgba(16, 185, 129, 0.15); color: var(--app-success); }
.scan-tag-err { background: rgba(239, 68, 68, 0.12); color: var(--app-error); }

/* Application update */
.update-ver {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0 4px;
}
.ver-item {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--app-text);
}
.ver-item strong { font-weight: 700; font-size: 15px; }
.ver-label {
  font-size: 11px;
  color: var(--app-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.ver-arrow { color: var(--app-text-muted); opacity: 0.7; }

.update-alert { margin-top: 8px; }
.update-meta-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 10px 14px;
  margin: 10px 0 0;
  padding: 10px 0 0;
  border-top: 1px solid var(--app-border);
}
.update-meta-row > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.update-meta-row dt {
  font-size: 11px;
  color: var(--app-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.update-meta-row dd {
  margin: 0;
  font-size: 12px;
  color: var(--app-text);
  word-break: break-all;
}
.update-meta-row code {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
}
.update-link { color: var(--app-primary); text-decoration: none; }
.update-link:hover { text-decoration: underline; }
.update-progress { margin-top: 12px; }
.update-log-collapse { margin-top: 12px; }
.update-log {
  padding: 10px 12px;
  background: rgba(0, 0, 0, 0.18);
  border-radius: var(--app-radius, 8px);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
  color: var(--app-text);
  max-height: 160px;
  overflow: auto;
}
.update-log-line + .update-log-line { margin-top: 4px; }

/* ───────── Responsive ───────── */
@media (max-width: 1200px) {
  .kpi-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 960px) {
  .main-grid { grid-template-columns: 1fr; }
}
@media (max-width: 720px) {
  .hero-bar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .hero-actions { flex-wrap: wrap; justify-content: flex-start; }
  .kpi-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .scan-item {
    grid-template-columns: 1fr auto;
    row-gap: 6px;
  }
  .scan-bar { grid-column: 1 / -1; order: 3; }
}
</style>
