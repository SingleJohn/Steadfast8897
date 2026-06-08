<script setup lang="ts">
import { NButton, NCard, NEmpty, NForm, NFormItem, NGrid, NGridItem, NIcon, NInput, NProgress, NSelect, NSwitch } from 'naive-ui'
import { AddOutline, ArrowForwardOutline, CloseOutline, VideocamOutline } from '@vicons/ionicons5'

const props = defineProps<{
  probeProgress: any | null
  probeThreads: string
  probeThreadsOptions: { label: string; value: string }[]
  probePathMappings: { from: string; to: string }[]
  probeOnIngest: boolean
  savingProbe: boolean
}>()

const emit = defineEmits<{
  'update:probeThreads': [value: string]
  'update:probePathMappings': [value: { from: string; to: string }[]]
  'update:probeOnIngest': [value: boolean]
  start: []
  stop: []
  save: []
}>()

function updateProbeThreads(value: unknown) {
  if (typeof value === 'string') emit('update:probeThreads', value)
}

function addMapping() {
  emit('update:probePathMappings', [...props.probePathMappings, { from: '', to: '' }])
}

function removeMapping(index: number) {
  emit('update:probePathMappings', props.probePathMappings.filter((_, idx) => idx !== index))
}

function updateMapping(index: number, key: 'from' | 'to', value: unknown) {
  const text = typeof value === 'string' ? value : ''
  const next = props.probePathMappings.map((mapping, idx) => (
    idx === index ? { ...mapping, [key]: text } : mapping
  ))
  emit('update:probePathMappings', next)
}
</script>

<template>
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
              <n-select :value="probeThreads" :options="probeThreadsOptions" :disabled="probeProgress?.status === 'running'" size="small" @update:value="updateProbeThreads" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item span="1">
            <n-form-item label="新入库自动探测">
              <n-switch :value="probeOnIngest" :disabled="probeProgress?.status === 'running'" @update:value="emit('update:probeOnIngest', $event)" />
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
              <n-input :value="m.from" placeholder="/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" @update:value="updateMapping(i, 'from', $event)" />
              <div class="arrow-slot">
                <n-icon depth="3"><ArrowForwardOutline /></n-icon>
              </div>
              <n-input :value="m.to" placeholder="/mnt/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" @update:value="updateMapping(i, 'to', $event)" />
              <div class="action-slot">
                <n-button quaternary circle type="error" size="small" :disabled="probeProgress?.status === 'running'" @click="removeMapping(i)">
                  <template #icon><n-icon><CloseOutline /></n-icon></template>
                </n-button>
              </div>
            </div>
          </div>

          <div class="mappings-footer">
            <n-button dashed size="small" block :disabled="probeProgress?.status === 'running'" @click="addMapping">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              添加映射
            </n-button>
          </div>
        </div>
      </div>
    </n-form>

    <div class="card-actions">
      <n-button v-if="probeProgress?.status !== 'running' && probeProgress?.status !== 'stopping'" type="primary" size="small" :loading="savingProbe" :disabled="savingProbe || (probeProgress?.missingCount === 0 && probeProgress?.status === 'idle')" @click="emit('start')">开始探测</n-button>
      <n-button v-else type="warning" size="small" :disabled="probeProgress?.status === 'stopping'" @click="emit('stop')">{{ probeProgress?.status === 'stopping' ? '停止中...' : '停止探测' }}</n-button>
      <n-button secondary size="small" :disabled="probeProgress?.status === 'running'" @click="emit('save')">保存设置</n-button>
    </div>
  </n-card>
</template>
