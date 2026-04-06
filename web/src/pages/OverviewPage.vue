<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NCard, NSpace, NSpin, NButton, NModal, NProgress, NIcon, NTag, NSelect, NAlert } from 'naive-ui'
import { BarChartOutline, FlashOutline, ServerOutline, WarningOutline, RefreshOutline, CloudDownloadOutline } from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import StatCard from '@/components/StatCard.vue'
import {
  getDailyStats,
  getGatewayConfig,
  type DailyStat,
  type GatewayConfig,
} from '@/api/gateway'
import {
  getSystemInfo,
  getActiveSessions,
  getScanProgress,
  getLibraries,
  restartServer,
  shutdownServer,
  getUpdateStatus,
  checkForUpdate,
  applyUpdate,
  setUpdateChannel,
  type UpdateStatus,
} from '@/api/client'
import { useToast } from '@/composables/useToast'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
const { showToast } = useToast()

const loading = ref(true)
const loadError = ref('')
const dailyStats = ref<DailyStat[]>([])
const gatewayConfig = ref<GatewayConfig | null>(null)

const serverInfo = ref<any>(null)
const sessions = ref<any[]>([])
const scanProgress = ref<any[]>([])
const libraries = ref<any[]>([])
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

const totalRequests = computed(() =>
  dailyStats.value.reduce((s, d) => s + (d.requests ?? 0), 0),
)
const totalRedirects = computed(() =>
  dailyStats.value.reduce((s, d) => s + (d.redirects302 ?? 0), 0),
)
const activeSources = computed(() => {
  const sources = gatewayConfig.value?.sources
  if (!Array.isArray(sources)) return 0
  return sources.filter((s) => s.enabled).length
})
const errorRate = computed(() => {
  const total = totalRequests.value
  if (!total) return '0%'
  const errors = dailyStats.value.reduce((s, d) => s + (d.status4xx ?? 0) + (d.status5xx ?? 0), 0)
  return ((errors / total) * 100).toFixed(1) + '%'
})

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
  const [info, sess, scan, libs, update] = await Promise.allSettled([
    getSystemInfo(),
    getActiveSessions(),
    getScanProgress(),
    getLibraries(),
    getUpdateStatus(),
  ])

  if (info.status === 'fulfilled') serverInfo.value = info.value
  if (sess.status === 'fulfilled' && Array.isArray(sess.value)) sessions.value = sess.value
  if (scan.status === 'fulfilled') {
    const items = scan.value?.Items
    scanProgress.value = Array.isArray(items) ? items : []
  }
  if (libs.status === 'fulfilled' && Array.isArray(libs.value)) libraries.value = libs.value
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
      getScanProgress()
        .then((r: any) => {
          const items = r?.Items
          scanProgress.value = Array.isArray(items) ? items : []
        })
        .catch(() => {})
    }, 3000),
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

    <n-spin :show="loading" style="min-height: 200px">
      <div class="stat-grid">
        <stat-card title="总请求数" :value="totalRequests.toLocaleString()" sub-title="最近 7 天" type="primary" :icon="BarChartOutline" />
        <stat-card title="302 重定向" :value="totalRedirects.toLocaleString()" sub-title="最近 7 天" type="success" :icon="FlashOutline" />
        <stat-card title="活跃源" :value="activeSources" sub-title="已启用的 Emby 源" type="info" :icon="ServerOutline" />
        <stat-card title="错误率" :value="errorRate" sub-title="4xx + 5xx" type="warning" :icon="WarningOutline" />
      </div>
    </n-spin>

    <!-- Server Info -->
    <n-card class="glass-card section-card" title="服务器信息">
      <div v-if="serverInfo" class="server-info-grid">
        <div v-for="row in [
          ['名称', serverInfo.ServerName],
          ['版本', formatVersion(serverInfo)],
          ['ID', formatServerId(serverInfo.Id)],
          ['操作系统', serverInfo.OperatingSystemDisplayName || serverInfo.OperatingSystem],
          ['本地地址', serverInfo.LocalAddress],
        ]" :key="row[0]" class="server-info-item">
          <span class="server-info-label">{{ row[0] }}</span>
          <span class="server-info-value">{{ row[1] || '-' }}</span>
        </div>
      </div>
      <div v-else class="empty-chart">加载服务器信息中...</div>
    </n-card>

    <n-card class="glass-card section-card" title="应用更新">
      <div class="update-header">
        <div>
          <div class="update-current">
            当前版本 <strong>{{ updateStatus?.currentVersion || serverInfo?.Version || 'dev' }}</strong>
            <n-tag size="small" :type="updateBadgeType as any">{{ updateStatusText }}</n-tag>
          </div>
          <div class="update-meta">
            最新版本：{{ updateStatus?.latestVersion || '-' }}，通道：{{ updateChannel === 'beta' ? '测试版' : '稳定版' }}
          </div>
        </div>
        <n-space align="center">
          <n-select
            :value="updateChannel"
            :options="updateChannelOptions"
            size="small"
            style="width: 110px"
            :disabled="checkingUpdate || applyingUpdate || isUpdateBusy(updateStatus?.status)"
            @update:value="handleChangeUpdateChannel"
          />
          <n-button secondary :loading="checkingUpdate" @click="handleCheckUpdate">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            检查更新
          </n-button>
          <n-button
            type="primary"
            :disabled="!updateStatus?.hasUpdate || isUpdateBusy(updateStatus?.status)"
            :loading="applyingUpdate"
            @click="showUpdateConfirm = true"
          >
            <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            立即更新
          </n-button>
        </n-space>
      </div>

      <n-alert
        v-if="updateStatus?.needsDockerSocket"
        type="warning"
        class="update-alert"
      >
        启用应用内自更新需要为 FYMS 容器挂载 Docker Socket，并保证 `/app/data` 为持久化目录。
      </n-alert>
      <n-alert
        v-if="updateConnectionLost"
        type="info"
        class="update-alert"
      >
        更新过程中连接短暂中断是正常现象，页面会持续轮询服务恢复状态。
      </n-alert>
      <n-alert
        v-if="updateStatus?.error"
        type="error"
        class="update-alert"
      >
        {{ updateStatus.error }}
      </n-alert>

      <div class="update-grid">
        <div class="update-item">
          <span class="update-label">目标镜像</span>
          <code class="update-code">{{ updateStatus?.targetImage || '-' }}</code>
        </div>
        <div class="update-item">
          <span class="update-label">最近检查</span>
          <span>{{ formatUpdateTime(updateStatus?.lastCheckedAt) }}</span>
        </div>
        <div class="update-item">
          <span class="update-label">最近完成</span>
          <span>{{ formatUpdateTime(updateStatus?.completedAt) }}</span>
        </div>
        <div class="update-item">
          <span class="update-label">更新说明</span>
          <a
            v-if="updateStatus?.releaseNotesUrl"
            :href="updateStatus.releaseNotesUrl"
            target="_blank"
            rel="noreferrer"
            class="update-link"
          >
            查看发布日志
          </a>
          <span v-else>-</span>
        </div>
      </div>

      <div v-if="isUpdateBusy(updateStatus?.status)" class="update-progress-wrap">
        <n-progress type="line" :percentage="85" :show-indicator="false" status="warning" />
      </div>

      <div v-if="updateLogLines.length" class="update-log">
        <div v-for="line in updateLogLines" :key="line" class="update-log-line">{{ line }}</div>
      </div>
    </n-card>

    <!-- Active Sessions -->
    <n-card class="glass-card section-card" title="活动会话">
      <div v-if="sessions.length === 0" class="empty-chart">暂无活动会话</div>
      <div v-else style="overflow-x: auto">
        <table class="session-table">
          <thead>
            <tr>
              <th>用户</th>
              <th>客户端</th>
              <th>设备</th>
              <th>IP</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in sessions" :key="s.Id">
              <td style="color: var(--app-text); font-weight: 500">{{ s.UserName }}</td>
              <td>{{ s.Client }}{{ s.ApplicationVersion ? ` ${s.ApplicationVersion}` : '' }}</td>
              <td>{{ s.DeviceName }}</td>
              <td style="font-family: monospace; font-size: 12px">{{ s.RemoteEndPoint || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </n-card>

    <!-- Scan Progress -->
    <n-card v-if="scanProgress.length > 0" class="glass-card section-card" title="扫描进度">
      <div class="scan-progress-grid">
        <div v-for="sp in scanProgress" :key="sp.LibraryId" class="scan-progress-item">
          <div class="scan-progress-name">{{ libNameForScan(sp.LibraryId) }}</div>
          <template v-if="sp.Status === 'scanning'">
            <div style="display: flex; align-items: center; gap: 8px">
              <n-progress type="line" :percentage="sp.Percentage" :show-indicator="false" style="flex: 1" />
              <span style="font-size: 11px; color: var(--app-primary)">{{ sp.Percentage }}% ({{ sp.ProcessedItems }}/{{ sp.TotalItems }})</span>
            </div>
          </template>
          <div v-else-if="sp.Status === 'completed'" style="font-size: 12px; color: #4caf50">扫描完成</div>
          <div v-else-if="sp.Status === 'failed'" style="font-size: 12px; color: var(--app-danger, #e53935)">扫描失败: {{ sp.Error }}</div>
          <div v-else style="font-size: 12px; color: var(--app-text-muted)">{{ sp.Status }}</div>
        </div>
      </div>
    </n-card>

    <!-- Server Control -->
    <n-card class="glass-card section-card" title="服务器控制">
      <n-space>
        <n-button type="warning" @click="showRestart = true">重启服务器</n-button>
        <n-button type="error" @click="showShutdown = true">关闭服务器</n-button>
      </n-space>
    </n-card>

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
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: var(--app-section-gap);
}

.section-card {
  margin-bottom: var(--app-section-gap);
}

.empty-chart {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 120px;
  color: var(--app-text-muted);
  font-size: 14px;
}

.server-info-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}

.server-info-item {
  padding: 10px 0;
  border-bottom: 1px solid var(--app-border, rgba(255, 255, 255, 0.04));
}

.server-info-label {
  font-size: 12px;
  color: var(--app-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.server-info-value {
  display: block;
  font-size: 14px;
  color: var(--app-text);
  margin-top: 2px;
}

.session-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.session-table th {
  text-align: left;
  padding: 8px 6px;
  color: var(--app-text-muted);
  font-weight: 500;
  border-bottom: 1px solid var(--app-border, rgba(255, 255, 255, 0.1));
}

.session-table td {
  padding: 8px 6px;
  color: var(--app-text-secondary, var(--app-text-muted));
  border-bottom: 1px solid var(--app-border, rgba(255, 255, 255, 0.04));
}

.scan-progress-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}

.scan-progress-item {
  background: var(--app-surface-1);
  border-radius: var(--app-radius, 8px);
  padding: 12px 14px;
}

.scan-progress-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
  margin-bottom: 8px;
}

.overview-error-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  margin-bottom: var(--app-section-gap);
  background: rgba(239, 68, 68, 0.08);
  border: 1px solid rgba(239, 68, 68, 0.2);
  border-radius: var(--app-radius);
  color: #f87171;
  font-size: 14px;
}

.update-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}

.update-current {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 15px;
  color: var(--app-text);
}

.update-meta {
  margin-top: 6px;
  font-size: 13px;
  color: var(--app-text-muted);
}

.update-alert {
  margin-bottom: 12px;
}

.update-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.update-item {
  background: var(--app-surface-1);
  border-radius: var(--app-radius, 8px);
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.update-label {
  font-size: 12px;
  color: var(--app-text-muted);
  text-transform: uppercase;
}

.update-code {
  font-size: 12px;
  word-break: break-all;
  color: var(--app-text);
}

.update-link {
  color: var(--app-primary);
  text-decoration: none;
}

.update-progress-wrap {
  margin-top: 14px;
}

.update-log {
  margin-top: 14px;
  padding: 12px 14px;
  background: rgba(0, 0, 0, 0.18);
  border-radius: var(--app-radius, 8px);
  font-family: monospace;
  font-size: 12px;
  color: var(--app-text);
  max-height: 180px;
  overflow: auto;
}

.update-log-line + .update-log-line {
  margin-top: 6px;
}

@media (max-width: 900px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .server-info-grid {
    grid-template-columns: 1fr;
  }
  .update-header {
    flex-direction: column;
  }
  .update-grid {
    grid-template-columns: 1fr;
  }
}
</style>
