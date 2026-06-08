<script setup lang="ts">
import { NButton, NCard, NForm, NIcon, NProgress, NSwitch } from 'naive-ui'
import { LayersOutline } from '@vicons/ionicons5'
import type { BackfillStage } from '@/api/client'

defineProps<{
  backfillProgress: any | null
  backfillConfig: { enabled_on_startup: boolean; episode_still_fetch: boolean }
  backfillBusy: boolean
  isRunning: boolean
  stageLabel: (stage: string) => string
}>()

const emit = defineEmits<{
  toggleStartup: [value: boolean]
  toggleEpisodeStill: [value: boolean]
  start: [stages?: BackfillStage[]]
  stop: []
  resetQuality: []
  resetEpisodeImage: []
}>()
</script>

<template>
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
          <n-switch :value="backfillConfig.enabled_on_startup" size="small" @update:value="emit('toggleStartup', $event)" />
          <div class="switch-copy">
            <div class="switch-title">启动时自动回填</div>
            <div class="switch-desc">服务启动时按 画质 → 标题 → 封面 顺序跑一次。24h 内不重复触发。</div>
          </div>
        </div>
        <div class="switch-row">
          <n-switch :value="backfillConfig.episode_still_fetch" size="small" @update:value="emit('toggleEpisodeStill', $event)" />
          <div class="switch-copy">
            <div class="switch-title">拉取 TMDB 分集封面</div>
            <div class="switch-desc">关闭后,分集封面只读本地 thumb,不再打 TMDB。</div>
          </div>
        </div>
      </div>
    </n-form>

    <div class="card-actions">
      <n-button v-if="!isRunning" type="primary" size="small" :loading="backfillBusy" @click="emit('start')">全部执行</n-button>
      <n-button v-if="!isRunning" secondary size="small" :disabled="backfillBusy" @click="emit('start', ['quality'])">仅画质(快)</n-button>
      <n-button v-if="!isRunning" secondary size="small" :disabled="backfillBusy" @click="emit('start', ['name'])">仅 Episode 标题</n-button>
      <n-button v-if="!isRunning" secondary size="small" :disabled="backfillBusy" @click="emit('start', ['image'])">仅分集封面(慢)</n-button>
      <n-button v-if="isRunning" type="warning" size="small" :disabled="backfillProgress?.status === 'stopping'" @click="emit('stop')">{{ backfillProgress?.status === 'stopping' ? '停止中...' : '停止回填' }}</n-button>
    </div>

    <div class="card-actions" style="margin-top: 8px">
      <n-button quaternary size="small" :disabled="isRunning" @click="emit('resetQuality')">重置画质字段</n-button>
      <n-button quaternary size="small" :disabled="isRunning" @click="emit('resetEpisodeImage')">重置 Episode 封面</n-button>
    </div>
  </n-card>
</template>
