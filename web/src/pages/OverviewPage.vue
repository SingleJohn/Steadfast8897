<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NIcon, NSpin } from 'naive-ui'
import {
  BarChartOutline,
  FilmOutline,
  FlashOutline,
  HardwareChipOutline,
  LibraryOutline,
  PeopleOutline,
  RefreshOutline,
  ServerOutline,
  ShieldCheckmarkOutline,
  WarningOutline,
} from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
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
  applyUpdate,
  checkForUpdate,
  getActiveSessions,
  getItemCounts,
  getLibraries,
  getSystemInfo,
  getUpdateStatus,
  getUpdateVersions,
  restartServer,
  applyUpdateVersion,
  setUpdateChannel,
  shutdownServer,
  type ItemCounts,
  type UpdateStatus,
  type UpdateVersion,
} from '@/api/client'
import { useToast } from '@/composables/useToast'
import { useVisibleInterval } from '@/composables/useVisibleInterval'
import KpiGroup from './overview/components/KpiGroup.vue'
import OverviewDialogs from './overview/components/OverviewDialogs.vue'
import OverviewHero from './overview/components/OverviewHero.vue'
import ScanProgressCard from './overview/components/ScanProgressCard.vue'
import SessionsCard from './overview/components/SessionsCard.vue'
import UpdateCard from './overview/components/UpdateCard.vue'
import type { OverviewKpiItem, ScanProgressItem, UpdateChannel } from './overview/types'
import { formatServerId, isUpdateBusy } from './overview/utils'

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
const scanProgress = computed<ScanProgressItem[]>(() => {
  const s = snapshots.scan
  if (!s || !s.children) return []
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
const loadingUpdateVersions = ref(false)
const updateConnectionLost = ref(false)
const updateChannel = ref<UpdateChannel>('stable')
const updateStatus = ref<UpdateStatus | null>(null)
const updateVersions = ref<UpdateVersion[]>([])
const selectedUpdateVersion = ref<string | null>(null)
const updateChannelOptions: { label: string; value: UpdateChannel }[] = [
  { label: '稳定版', value: 'stable' },
  { label: '开发版', value: 'nightly' },
]

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

// 只统计"正在播放"(有 NowPlayingItem)的会话,排除仅在线/保活的连接
const playingSessions = computed(() => sessions.value.filter((s: any) => s.NowPlayingItem))
const activeSessionCount = computed(() => playingSessions.value.length)
const activeScanCount = computed(
  () => scanProgress.value.filter((sp) => sp.Status === 'scanning').length,
)

const requestsSeries = computed(() => dailyStats.value.map((d) => d.requests || 0))
const redirectsSeries = computed(() => dailyStats.value.map((d) => d.redirects302 || 0))
const errorsSeries = computed(() =>
  dailyStats.value.map((d) => (d.status4xx || 0) + (d.status5xx || 0)),
)

const isRunning = computed(() => !!serverInfo.value)
const runStatusText = computed(() => (isRunning.value ? '已运行' : '未连接'))
const fullServerId = computed(() => {
  const id = serverInfo.value?.Id
  return id ? formatServerId(id) : ''
})

const gatewayKpis = computed<OverviewKpiItem[]>(() => [
  {
    key: 'requests',
    title: '总请求数',
    value: totalRequests.value.toLocaleString(),
    hint: '7 天',
    type: 'primary',
    icon: BarChartOutline,
    routeName: 'obs_gw_traffic',
    sparkline: requestsSeries.value,
    sparklineColor: 'var(--app-primary)',
  },
  {
    key: 'redirects',
    title: '302 重定向',
    value: totalRedirects.value.toLocaleString(),
    hint: '7 天',
    type: 'success',
    icon: FlashOutline,
    routeName: 'obs_gw_redirect',
    sparkline: redirectsSeries.value,
    sparklineColor: 'var(--app-success)',
  },
  {
    key: 'errors',
    title: '错误数',
    value: totalErrors.value.toLocaleString(),
    hint: `${errorRate.value} · 4xx+5xx`,
    type: 'warning',
    icon: WarningOutline,
    routeName: 'obs_gw_traffic',
    sparkline: errorsSeries.value,
    sparklineColor: 'var(--app-warning)',
  },
  {
    key: 'sources',
    title: '活跃源',
    value: activeSources.value,
    valueSub: `/ ${totalSources.value}`,
    hint: '已启用 Emby 源',
    type: 'info',
    icon: ServerOutline,
    routeName: 'gateway_emby_sources',
  },
])

const serviceKpis = computed<OverviewKpiItem[]>(() => [
  {
    key: 'sessions',
    title: '活跃会话',
    value: activeSessionCount.value,
    hint: activeSessionCount.value > 0 ? '正在播放' : '无活动连接',
    type: 'success',
    icon: PeopleOutline,
    live: true,
  },
  {
    key: 'scans',
    title: '扫描中',
    value: activeScanCount.value,
    hint: activeScanCount.value > 0 ? '正在扫描媒体库' : '无扫描任务',
    type: activeScanCount.value > 0 ? 'warning' : 'info',
    icon: LibraryOutline,
    routeName: 'media_libraries',
  },
  {
    key: 'libraries',
    title: '媒体库数',
    value: totalLibraries.value,
    hint: '已配置媒体库',
    type: 'info',
    icon: LibraryOutline,
    routeName: 'media_libraries',
  },
  {
    key: 'media',
    title: '媒体项总数',
    value: totalMediaItems.value.toLocaleString(),
    hint: mediaBreakdown.value,
    type: 'primary',
    icon: FilmOutline,
  },
])

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
const deploymentMode = computed(() => updateStatus.value?.deploymentMode || 'docker')
const isManualUpdate = computed(() => deploymentMode.value === 'manual')
const selectedVersionInfo = computed(() =>
  updateVersions.value.find((item) => item.version === selectedUpdateVersion.value) || null,
)
const updateConfirmText = computed(() => {
  const selected = selectedVersionInfo.value
  if (selected) {
    const action = selected.direction === 'downgrade' ? '降级' : '切换'
    return `将${action}到 ${selected.version} 程序版本。配置、媒体库和用户数据不会恢复或修改，服务会短暂重启。`
  }
  if (deploymentMode.value === 'binary') {
    return '更新前会备份当前二进制，然后下载新版本并替换，进程会自动重启。过程中服务会短暂中断。'
  }
  return '更新前会自动创建备份，并通过 Docker 拉取新镜像后重建当前容器。过程中服务会短暂中断。'
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

function libNameForScan(libId: string) {
  return libraries.value.find((l: any) => l.ItemId === libId)?.Name || libId
}

function navigate(routeName: string) {
  router.push({ name: routeName })
}

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
    updateChannel.value = (status.channel as UpdateChannel) || 'stable'
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

async function loadUpdateVersions(channel: UpdateChannel = updateChannel.value) {
  loadingUpdateVersions.value = true
  try {
    const resp = await getUpdateVersions(channel)
    updateVersions.value = resp.versions || []
    const currentSelected = updateVersions.value.find((item) => item.version === selectedUpdateVersion.value)
    if (!currentSelected || currentSelected.current || !currentSelected.installable) {
      selectedUpdateVersion.value = updateVersions.value.find((item) => item.installable && !item.current)?.version || null
    }
  } catch (err: any) {
    updateVersions.value = []
    selectedUpdateVersion.value = null
    showToast(err?.message || '加载版本列表失败', 'error')
  } finally {
    loadingUpdateVersions.value = false
  }
}

async function handleCheckUpdate() {
  checkingUpdate.value = true
  try {
    updateStatus.value = await checkForUpdate()
    updateChannel.value = (updateStatus.value.channel as UpdateChannel) || 'stable'
    await loadUpdateVersions(updateChannel.value)
    showToast(updateStatus.value.hasUpdate ? '发现新版本' : '当前已是最新版本', 'success')
  } catch (err: any) {
    showToast(err?.message || '检查更新失败', 'error')
  } finally {
    checkingUpdate.value = false
  }
}

async function handleChangeUpdateChannel(value: UpdateChannel) {
  try {
    updateStatus.value = await setUpdateChannel(value)
    updateChannel.value = value
    await loadUpdateVersions(value)
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
    if (selectedUpdateVersion.value) {
      updateStatus.value = await applyUpdateVersion(updateChannel.value, selectedUpdateVersion.value)
    } else {
      updateStatus.value = await applyUpdate(['settings', 'users', 'libraries', 'media'])
    }
    showToast('版本切换任务已启动，服务会短暂重启', 'success')
  } catch (err: any) {
    showToast(err?.message || '启动版本切换失败', 'error')
  } finally {
    applyingUpdate.value = false
  }
}

function openManualDownload() {
  const url = updateStatus.value?.downloadUrl
  if (!url) {
    showToast('未获取到下载链接，请先点击检查更新', 'warning')
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

let refreshingSessions = false
let refreshingUpdateStatus = false

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
    updateChannel.value = (update.value.channel as UpdateChannel) || 'stable'
    await loadUpdateVersions(updateChannel.value)
  }
}

async function loadAll() {
  loading.value = true
  loadError.value = ''
  await Promise.allSettled([loadGatewayData(), loadServerData()])
  loading.value = false
}

async function refreshActiveSessions() {
  if (refreshingSessions) return
  refreshingSessions = true
  try {
    const s = await getActiveSessions()
    if (Array.isArray(s)) sessions.value = s
  } catch {
    // Keep the last successful snapshot when polling fails.
  } finally {
    refreshingSessions = false
  }
}

async function refreshUpdateStatus() {
  if (refreshingUpdateStatus) return
  refreshingUpdateStatus = true
  try {
    await loadUpdateStatus()
  } catch {
    // Update status is opportunistic; the card will refresh on the next tick.
  } finally {
    refreshingUpdateStatus = false
  }
}

useVisibleInterval(refreshActiveSessions, 5000)
useVisibleInterval(refreshUpdateStatus, 4000)

onMounted(() => {
  loadAll()
})
</script>

<template>
  <page-shell title="总览" description="系统与网关状态一览">
    <div class="overview-page">
      <div v-if="loadError" class="overview-error-banner">
        <n-icon :component="WarningOutline" :size="18" />
        <span>{{ loadError }}</span>
        <n-button text size="small" @click="loadAll" style="margin-left: auto">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          重试
        </n-button>
      </div>

      <n-spin :show="loading">
        <OverviewHero
          :server-info="serverInfo"
          :update-status="updateStatus"
          :is-running="isRunning"
          :run-status-text="runStatusText"
          :full-server-id="fullServerId"
          :checking-update="checkingUpdate"
          :applying-update="applyingUpdate"
          :is-manual-update="isManualUpdate"
          @check-update="handleCheckUpdate"
          @apply-update="showUpdateConfirm = true"
          @manual-download="openManualDownload"
          @restart="showRestart = true"
          @shutdown="showShutdown = true"
          @copy-server-id="copyServerId"
        />

        <system-metrics-row />

        <KpiGroup
          title="网关观测"
          :icon="ShieldCheckmarkOutline"
          detail-route="obs_gw_traffic"
          :items="gatewayKpis"
          @navigate="navigate"
        />

        <KpiGroup
          title="服务观测"
          :icon="HardwareChipOutline"
          detail-route="obs_svc_playback"
          :items="serviceKpis"
          @navigate="navigate"
        />

        <div class="main-grid">
          <div class="main-col">
            <SessionsCard :sessions="playingSessions" />
            <task-center-card />
            <ScanProgressCard :items="scanProgress" :library-name-for="libNameForScan" />
          </div>

          <div class="main-col">
            <UpdateCard
              :update-status="updateStatus"
              :server-version="serverInfo?.Version"
              :update-channel="updateChannel"
              :update-channel-options="updateChannelOptions"
              :checking-update="checkingUpdate"
              :applying-update="applyingUpdate"
              :loading-update-versions="loadingUpdateVersions"
              :update-versions="updateVersions"
              :selected-update-version="selectedUpdateVersion"
              :deployment-mode="deploymentMode"
              :is-manual-update="isManualUpdate"
              :update-connection-lost="updateConnectionLost"
              :update-badge-type="updateBadgeType"
              :update-status-text="updateStatusText"
              :update-log-lines="updateLogLines"
              @change-channel="handleChangeUpdateChannel"
              @update:selected-update-version="selectedUpdateVersion = $event"
              @apply-version="showUpdateConfirm = true"
            />
          </div>
        </div>
      </n-spin>

      <OverviewDialogs
        v-model:show-restart="showRestart"
        v-model:show-shutdown="showShutdown"
        v-model:show-update-confirm="showUpdateConfirm"
        :update-confirm-text="updateConfirmText"
        @restart="handleRestart"
        @shutdown="handleShutdown"
        @apply-update="handleApplyUpdate"
      />
    </div>
  </page-shell>
</template>

<style src="./overview/styles.css"></style>
