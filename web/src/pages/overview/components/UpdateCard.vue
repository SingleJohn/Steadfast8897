<script setup lang="ts">
import { computed } from 'vue'
import { NAlert, NButton, NCard, NCollapse, NCollapseItem, NIcon, NProgress, NSelect, NTag } from 'naive-ui'
import { CloudDownloadOutline, ArrowForwardOutline } from '@vicons/ionicons5'
import type { UpdateStatus, UpdateVersion } from '@/api/client'
import type { UpdateChannel } from '../types'
import { formatUpdateTime, isUpdateBusy } from '../utils'

const props = defineProps<{
  updateStatus: UpdateStatus | null
  serverVersion?: string
  updateChannel: UpdateChannel
  updateChannelOptions: { label: string; value: UpdateChannel }[]
  checkingUpdate: boolean
  applyingUpdate: boolean
  loadingUpdateVersions: boolean
  updateVersions: UpdateVersion[]
  selectedUpdateVersion: string | null
  deploymentMode: string
  isManualUpdate: boolean
  updateConnectionLost: boolean
  updateBadgeType: string
  updateStatusText: string
  updateLogLines: string[]
}>()

const emit = defineEmits<{
  changeChannel: [value: UpdateChannel]
  'update:selectedUpdateVersion': [value: string | null]
  applyVersion: []
}>()

const versionOptions = computed(() => props.updateVersions.map((item) => ({
  label: versionOptionLabel(item),
  value: item.version,
  disabled: !item.installable || item.current,
})))

const selectedVersion = computed(() =>
  props.updateVersions.find((item) => item.version === props.selectedUpdateVersion) || null,
)

const applyDisabled = computed(() =>
  props.checkingUpdate ||
  props.applyingUpdate ||
  props.isManualUpdate ||
  props.loadingUpdateVersions ||
  !selectedVersion.value ||
  !selectedVersion.value.installable ||
  selectedVersion.value.current ||
  isUpdateBusy(props.updateStatus?.status),
)

const applyLabel = computed(() => {
  const selected = selectedVersion.value
  if (!selected) return '切换版本'
  if (selected.direction === 'upgrade') return `升级到 ${selected.version}`
  if (selected.direction === 'downgrade') return `降级到 ${selected.version}`
  return '当前版本'
})

function onChangeChannel(value: unknown) {
  if (value === 'stable' || value === 'nightly') emit('changeChannel', value)
}

function onChangeVersion(value: unknown) {
  emit('update:selectedUpdateVersion', typeof value === 'string' ? value : null)
}

function versionOptionLabel(item: UpdateVersion) {
  const suffix =
    item.current ? '当前' :
    item.direction === 'upgrade' ? '升级' :
    item.direction === 'downgrade' ? '降级' : '切换'
  return `${item.version} · ${suffix}`
}
</script>

<template>
  <n-card class="section-card" title="应用更新" size="small">
    <template #header-extra>
      <n-select
        :value="updateChannel"
        :options="updateChannelOptions"
        size="tiny"
        style="width: 96px"
        :disabled="checkingUpdate || applyingUpdate || isUpdateBusy(updateStatus?.status)"
        @update:value="onChangeChannel"
      />
    </template>

    <div class="update-ver">
      <div class="ver-item">
        <span class="ver-label">当前</span>
        <strong>{{ updateStatus?.currentVersion || serverVersion || 'dev' }}</strong>
      </div>
      <n-icon :component="ArrowForwardOutline" :size="18" class="ver-arrow" />
      <div class="ver-item">
        <span class="ver-label">最新</span>
        <strong>{{ updateStatus?.latestVersion || '-' }}</strong>
        <n-tag size="small" :type="updateBadgeType as any" round :bordered="false">{{ updateStatusText }}</n-tag>
      </div>
    </div>

    <n-alert v-if="deploymentMode === 'docker' && updateStatus?.needsDockerSocket" type="warning" size="small" class="update-alert">
      未检测到可用 Docker Socket。应用内自更新需要挂载 `/var/run/docker.sock`，并保证 `/app/data` 为持久化目录。
    </n-alert>
    <n-alert v-if="isManualUpdate" type="info" size="small" class="update-alert">
      当前平台（Windows）暂不支持应用内自动更新。请点击"下载更新包"获取压缩包，停止服务后替换 fyms.exe 再启动。
    </n-alert>
    <n-alert v-if="updateConnectionLost" type="info" size="small" class="update-alert">
      更新过程中连接短暂中断是正常现象，页面会持续轮询服务恢复状态。
    </n-alert>
    <n-alert v-if="updateStatus?.error" type="error" size="small" class="update-alert">
      {{ updateStatus.error }}
    </n-alert>

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

    <div class="update-actions">
      <n-select
        :value="selectedUpdateVersion"
        :options="versionOptions"
        size="small"
        placeholder="选择目标版本"
        :loading="loadingUpdateVersions"
        :disabled="checkingUpdate || applyingUpdate || isManualUpdate || isUpdateBusy(updateStatus?.status)"
        @update:value="onChangeVersion"
      />
      <n-button
        size="small"
        type="primary"
        :loading="applyingUpdate"
        :disabled="applyDisabled"
        @click="emit('applyVersion')"
      >
        <template #icon><n-icon :component="CloudDownloadOutline" /></template>
        {{ applyLabel }}
      </n-button>
    </div>
    <div v-if="selectedVersion?.reason" class="version-reason">{{ selectedVersion.reason }}</div>
  </n-card>
</template>

<style scoped>
.update-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
}

.update-actions :deep(.n-select) {
  flex: 1;
}

.version-reason {
  color: var(--text-muted);
  font-size: 12px;
  margin-top: 8px;
}
</style>
