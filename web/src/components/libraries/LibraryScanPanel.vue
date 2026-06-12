<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NPopconfirm, NProgress, NSelect, NSwitch } from 'naive-ui'

import type { LibraryScanProgress } from '@/composables/useLibraryScanState'

const props = defineProps<{
  libraries: any[]
  scanProgress: LibraryScanProgress[]
  scanThreads: string
  scanThreadsOptions: { label: string; value: string }[]
  fileWatcherEnabled: boolean
  scanning: boolean
  savingConfig: boolean
}>()

const emit = defineEmits<{
  'update:scanThreads': [value: string]
  'update:fileWatcherEnabled': [value: boolean]
  scan: []
  forceScan: []
  saveSettings: []
}>()

const scanRows = computed(() =>
  props.libraries.map((lib) => ({
    id: lib.ItemId,
    name: lib.Name,
    type: lib.CollectionType,
    paths: Array.isArray(lib.Locations) ? lib.Locations : [],
    progress: props.scanProgress.find((s) => s.LibraryId === lib.ItemId),
  })),
)

const runningRows = computed(() => props.scanProgress.filter((s) => s.Status === 'scanning'))
const completedRows = computed(() => props.scanProgress.filter((s) => s.Status === 'completed'))
const failedRows = computed(() => props.scanProgress.filter((s) => s.Status === 'failed'))
const total = computed(() => props.scanProgress.reduce((sum, s) => sum + (s.TotalItems || 0), 0))
const processed = computed(() => props.scanProgress.reduce((sum, s) => sum + (s.ProcessedItems || 0), 0))
const percent = computed(() => (total.value > 0 ? Math.round((processed.value / total.value) * 100) : 0))
const isScanning = computed(() => runningRows.value.length > 0)

const statusTitle = computed(() => {
  if (isScanning.value) return `${runningRows.value.length} 个媒体库正在扫描`
  if (failedRows.value.length > 0) return `${failedRows.value.length} 个媒体库扫描失败`
  if (completedRows.value.length > 0) return '最近一次扫描已完成'
  return '当前没有扫描任务'
})

const statusText = computed(() => {
  if (isScanning.value) return `已处理 ${processed.value} / ${total.value} 项。扫描期间会发现新增、删除和改名的媒体文件。`
  if (failedRows.value.length > 0) return '请检查失败媒体库的路径是否可读，然后重新扫描。'
  if (completedRows.value.length > 0) return `完成 ${completedRows.value.length} 个媒体库，结果会短暂保留用于核对。`
  return props.fileWatcherEnabled
    ? '文件监听已开启，新增和删除会自动入库；手动扫描适合首次建库或批量整理后使用。'
    : '文件监听已关闭，新增和删除需要手动扫描后才会入库。'
})

const scanBlocked = computed(() => props.libraries.length === 0 || props.scanning || isScanning.value)

function collectionLabel(type: string) {
  if (type === 'movies') return '电影'
  if (type === 'tvshows') return '电视剧'
  if (type === 'mixed') return '混合'
  return '媒体'
}

function rowStatus(row: { progress?: LibraryScanProgress }) {
  const p = row.progress
  if (!p) return '空闲'
  if (p.Status === 'scanning') return `扫描中 ${p.Percentage || 0}%`
  if (p.Status === 'completed') return '扫描完成'
  if (p.Status === 'failed') return '扫描失败'
  return p.Status || '空闲'
}
</script>

<template>
  <div class="scan-layout">
    <section class="scan-hero">
      <div>
        <p class="scan-kicker">扫库中心</p>
        <h3 class="scan-title">{{ statusTitle }}</h3>
        <p class="scan-desc">{{ statusText }}</p>
      </div>
      <div class="scan-meter">
        <n-progress
          type="circle"
          :percentage="isScanning ? percent : (completedRows.length > 0 ? 100 : 0)"
          :status="failedRows.length > 0 ? 'error' : 'success'"
        />
      </div>
    </section>

    <section class="scan-actions">
      <div class="scan-action-copy">
        <h4>手动扫描</h4>
        <p>发现媒体库路径里的新增、删除、改名文件，并同步到媒体条目。</p>
      </div>
      <div class="scan-action-buttons">
        <n-button type="primary" :loading="scanning" :disabled="scanBlocked" @click="emit('scan')">
          {{ isScanning ? '扫描中...' : '扫描所有媒体库' }}
        </n-button>
        <n-popconfirm positive-text="强制重扫" negative-text="取消" @positive-click="emit('forceScan')">
          <template #trigger>
            <n-button secondary type="warning" :disabled="scanBlocked">
              强制重扫
            </n-button>
          </template>
          会先扫描所有库，然后重新读取本地 NFO 和图片，并刷新元数据/图片队列。
        </n-popconfirm>
      </div>
    </section>

    <section class="scan-settings">
      <div class="setting-row">
        <div>
          <div class="setting-label">并发扫描线程数</div>
          <div class="setting-desc">同时处理的媒体文件数量，值越大扫描越快但占用资源越多。</div>
        </div>
        <n-select
          :value="scanThreads"
          :options="scanThreadsOptions"
          class="scan-setting-control"
          @update:value="(value) => emit('update:scanThreads', value)"
        />
      </div>
      <div class="setting-row">
        <div>
          <div class="setting-label">文件监听</div>
          <div class="setting-desc">实时监听媒体库目录变动。关闭后，新增和删除需要手动扫描。</div>
        </div>
        <n-switch
          :value="fileWatcherEnabled"
          @update:value="(value) => emit('update:fileWatcherEnabled', value)"
        />
      </div>
      <div class="settings-actions">
        <n-button type="primary" :loading="savingConfig" @click="emit('saveSettings')">保存扫库设置</n-button>
      </div>
    </section>

    <section class="scan-list">
      <div class="scan-list-head">
        <div>
          <h4>媒体库明细</h4>
          <p>每个媒体库独立扫描；当前后端不支持中途取消。</p>
        </div>
      </div>
      <div v-if="scanRows.length === 0" class="scan-empty">尚未配置媒体库。</div>
      <div v-else class="scan-row-list">
        <div v-for="row in scanRows" :key="row.id" class="scan-row" :class="{ 'is-failed': row.progress?.Status === 'failed' }">
          <div class="scan-row-main">
            <div class="scan-row-title">
              <span>{{ row.name }}</span>
              <span class="scan-type">{{ collectionLabel(row.type) }}</span>
            </div>
            <div class="scan-row-meta">{{ row.paths.length }} 个文件夹 · {{ rowStatus(row) }}</div>
            <div v-if="row.progress?.Error" class="scan-row-error">{{ row.progress.Error }}</div>
          </div>
          <div class="scan-row-progress">
            <n-progress
              type="line"
              :percentage="row.progress?.Percentage || 0"
              :status="row.progress?.Status === 'failed' ? 'error' : 'success'"
              :show-indicator="false"
            />
            <span v-if="row.progress?.TotalItems" class="scan-row-count">
              {{ row.progress.ProcessedItems }} / {{ row.progress.TotalItems }}
            </span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.scan-layout {
  display: grid;
  gap: 16px;
}

.scan-hero,
.scan-actions,
.scan-settings,
.scan-list {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 20px 24px;
}

.scan-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}

.scan-kicker {
  margin: 0 0 6px;
  color: var(--app-primary, #10b981);
  font-size: 12px;
  font-weight: 700;
}

.scan-title {
  margin: 0;
  color: var(--app-text);
  font-size: 20px;
  line-height: 1.25;
}

.scan-desc {
  max-width: 720px;
  margin: 8px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.scan-meter {
  flex: 0 0 auto;
}

.scan-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.scan-action-copy h4,
.scan-list h4 {
  margin: 0;
  color: var(--app-text);
  font-size: 15px;
}

.scan-action-copy p,
.scan-list-head p {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 12px;
}

.scan-action-buttons {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 0;
}
.setting-row + .setting-row { border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.setting-label { font-size: 14px; color: var(--app-text); }
.setting-desc { font-size: 12px; color: var(--app-text-muted); margin-top: 2px; }
.settings-actions { margin-top: 16px; }
.scan-setting-control { width: 112px; }

.scan-list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.scan-empty {
  padding: 16px 0;
  color: var(--app-text-muted);
  font-size: 13px;
}

.scan-row-list {
  display: grid;
  gap: 8px;
}

.scan-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(160px, 240px);
  align-items: center;
  gap: 16px;
  padding: 12px 0;
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
}

.scan-row-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--app-text);
  font-size: 14px;
  font-weight: 600;
}

.scan-type {
  flex: 0 0 auto;
  border: 1px solid rgba(var(--app-primary-rgb), 0.36);
  border-radius: 4px;
  padding: 1px 6px;
  color: var(--app-primary, #10b981);
  font-size: 10px;
}

.scan-row-meta,
.scan-row-count {
  margin-top: 3px;
  color: var(--app-text-muted);
  font-size: 12px;
}

.scan-row-error {
  margin-top: 5px;
  color: #d03050;
  font-size: 12px;
  word-break: break-word;
}

.scan-row-progress {
  display: grid;
  gap: 4px;
  font-variant-numeric: tabular-nums;
}

@media (max-width: 720px) {
  .scan-hero,
  .scan-actions,
  .setting-row {
    flex-direction: column;
    align-items: stretch;
  }

  .scan-row {
    grid-template-columns: 1fr;
  }

  .scan-meter {
    align-self: center;
  }
}
</style>
