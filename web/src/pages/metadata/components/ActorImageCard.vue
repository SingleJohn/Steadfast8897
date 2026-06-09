<script setup lang="ts">
import { NButton, NCard, NForm, NFormItem, NIcon, NInput, NSwitch } from 'naive-ui'
import { PeopleOutline } from '@vicons/ionicons5'
import type { ActorImageSummary } from '@/api/client'

defineProps<{
  nfoThumb: boolean
  localActors: boolean
  localLib: boolean
  localLibPath: string
  extSource: boolean
  extUrl: string
  summary: ActorImageSummary | null
  savingConfig: boolean
  backfilling: boolean
}>()

const emit = defineEmits<{
  'update:nfoThumb': [value: boolean]
  'update:localActors': [value: boolean]
  'update:localLib': [value: boolean]
  'update:localLibPath': [value: string]
  'update:extSource': [value: boolean]
  'update:extUrl': [value: string]
  save: []
  backfill: []
}>()

function str(value: unknown) {
  return typeof value === 'string' ? value : ''
}
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card metadata-card">
    <template #header>
      <div class="card-header-wrap">
        <div class="icon-box tmdb">
          <n-icon :size="18"><PeopleOutline /></n-icon>
        </div>
        <div class="header-copy">
          <div class="header-title">演员头像</div>
          <div class="header-desc">从 NFO/本地读取、按名匹配头像库,或批量补全。</div>
        </div>
      </div>
    </template>
    <template #header-extra>
      <div v-if="summary" class="inline-stat">
        <span class="inline-stat-value">{{ summary.with_image }}</span>
        <span class="inline-stat-name">已有 / {{ summary.total }} 演员</span>
      </div>
    </template>

    <n-form label-placement="top" size="small" class="config-form">
      <div class="switch-row-grid">
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">解析 NFO 头像</div>
            <div class="hint-text">读取 NFO 演员块里的 &lt;thumb&gt; 头像(http 或本地路径)。</div>
          </div>
          <n-switch :value="nfoThumb" :round="false" @update:value="emit('update:nfoThumb', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">扫描 .actors 目录</div>
            <div class="hint-text">就近读取媒体目录下 .actors/&lt;演员名&gt;.jpg(Emby/Kodi 约定)。</div>
          </div>
          <n-switch :value="localActors" :round="false" @update:value="emit('update:localActors', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">本地头像库(按名)</div>
            <div class="hint-text">按演员姓名从一个公共目录匹配头像,适合番号/JAV 等不在 TMDB 的演员。</div>
          </div>
          <n-switch :value="localLib" :round="false" @update:value="emit('update:localLib', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">外部按名头像源</div>
            <div class="hint-text">按姓名从外部 URL 拉头像(best-effort,默认关)。</div>
          </div>
          <n-switch :value="extSource" :round="false" @update:value="emit('update:extSource', $event)" />
        </div>
      </div>

      <n-form-item v-if="localLib" label="本地头像库目录">
        <n-input
          :value="localLibPath"
          placeholder="data/actor_avatars"
          size="small"
          @update:value="emit('update:localLibPath', str($event))"
        />
      </n-form-item>
      <n-form-item v-if="extSource" label="外部源 URL 模板">
        <n-input
          :value="extUrl"
          placeholder="https://host/avatar/{name}.jpg"
          size="small"
          @update:value="emit('update:extUrl', str($event))"
        />
      </n-form-item>
      <div v-if="extSource" class="hint-text">URL 中用 {name} 占位演员姓名(自动 URL 编码)。</div>
    </n-form>

    <div class="card-actions">
      <n-button type="primary" size="small" :loading="savingConfig" @click="emit('save')">保存头像设置</n-button>
      <n-button secondary size="small" :loading="backfilling" @click="emit('backfill')">批量补演员头像</n-button>
    </div>
  </n-card>
</template>
