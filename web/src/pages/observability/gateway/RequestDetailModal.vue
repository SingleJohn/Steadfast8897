<script setup lang="ts">
import { computed } from 'vue'
import { NDescriptions, NDescriptionsItem, NModal, NScrollbar, NTag } from 'naive-ui'

import type { RequestLog } from '@/types'

const show = defineModel<boolean>('show', { required: true })

const props = defineProps<{
  selectedLog: RequestLog | null
  ipLocation: string
  sourceLabel: string
  sourceUpstream: string
}>()

const selectedTime = computed(() => {
  const raw = String(props.selectedLog?.created_at || '')
  return raw ? raw.replace('T', ' ').slice(0, 19) : '-'
})

const redirectTraceText = computed(() => {
  const raw = String(props.selectedLog?.redirect_trace || '').trim()
  if (!raw) return '-'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
})
</script>

<template>
  <n-modal v-model:show="show" preset="card" style="width: 850px" title="请求详情" class="glass-card glass-modal">
    <n-scrollbar style="max-height: 70vh">
      <n-descriptions class="glass-descriptions" bordered label-placement="left" :column="1" size="small" :label-style="{ width: '140px' }">
        <n-descriptions-item label="ID">{{ selectedLog?.id }}</n-descriptions-item>
        <n-descriptions-item label="Time">{{ selectedTime }}</n-descriptions-item>
        <n-descriptions-item label="Status">
          <n-tag :type="selectedLog?.status && selectedLog.status < 400 ? 'success' : 'error'" size="small" round>
            {{ selectedLog?.status }}
          </n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="Method">{{ selectedLog?.method }}</n-descriptions-item>
        <n-descriptions-item label="Path">
          <div class="code-wrap">{{ selectedLog?.path }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Query">
          <div class="code-wrap">{{ selectedLog?.query || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Emby User">
          <div class="code-wrap">{{ selectedLog?.emby_user_name ? `${selectedLog.emby_user_name} (${selectedLog.emby_user_id})` : (selectedLog?.emby_user_id || '-') }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Redirect Backend">
          <div class="code-wrap">{{ selectedLog?.redirect_backend || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Redirect Source">
          <div class="code-wrap">{{ selectedLog?.redirect_source || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Location">
          <div class="code-wrap">{{ selectedLog?.redirect_location || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Object Key">
          <div class="code-wrap">{{ selectedLog?.object_key || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Redirect Trace">
          <div class="code-wrap">{{ redirectTraceText }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Bytes Out">
          {{ ((selectedLog?.bytes_out || 0) / 1024).toFixed(2) }} KB
        </n-descriptions-item>
        <n-descriptions-item label="Latency">{{ selectedLog?.latency_ms }} ms</n-descriptions-item>
        <n-descriptions-item label="IP">{{ selectedLog?.client_ip }}</n-descriptions-item>
        <n-descriptions-item label="归属地">{{ ipLocation }}</n-descriptions-item>
        <n-descriptions-item label="Emby Source">{{ sourceLabel }}</n-descriptions-item>
        <n-descriptions-item label="Upstream">{{ sourceUpstream }}</n-descriptions-item>
        <n-descriptions-item label="User-Agent">
          <div class="code-wrap">{{ selectedLog?.user_agent }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Referer">
          <div class="code-wrap">{{ selectedLog?.referer || '-' }}</div>
        </n-descriptions-item>
        <n-descriptions-item label="Headers">
          <div class="code-wrap">{{ selectedLog?.headers || '-' }}</div>
        </n-descriptions-item>
      </n-descriptions>
    </n-scrollbar>
  </n-modal>
</template>

<style scoped>
.code-wrap {
  white-space: pre-wrap;
  word-break: break-all;
  font-family: monospace;
  font-size: 12px;
  background: var(--c-slate-100);
  padding: 6px 10px;
  border-radius: 6px;
  color: var(--c-slate-700);
}
.app-dark .code-wrap {
  background: var(--c-slate-800);
  color: var(--c-slate-300);
}

.glass-modal :deep(.n-card) {
  background: rgba(255, 255, 255, 0.55) !important;
  border: 1px solid rgba(255, 255, 255, 0.35) !important;
  box-shadow: var(--app-shadow-card);
  backdrop-filter: blur(var(--app-glass-blur)) saturate(1.5);
  -webkit-backdrop-filter: blur(var(--app-glass-blur)) saturate(1.5);
}
.app-dark .glass-modal :deep(.n-card) {
  background: rgba(15, 23, 42, 0.6) !important;
  border: 1px solid rgba(255, 255, 255, 0.08) !important;
}

.glass-modal :deep(.n-descriptions-table),
.glass-modal :deep(.n-descriptions-table td),
.glass-modal :deep(.n-descriptions-table th) {
  background: transparent !important;
}

.glass-descriptions {
  --n-th-color-modal: transparent !important;
  --n-td-color-modal: transparent !important;
  --n-th-color: transparent !important;
  --n-td-color: transparent !important;
  --n-border-color-modal: rgba(148, 163, 184, 0.28) !important;
  --n-border-color: rgba(148, 163, 184, 0.28) !important;
}
.app-dark .glass-descriptions {
  --n-border-color-modal: rgba(148, 163, 184, 0.18) !important;
  --n-border-color: rgba(148, 163, 184, 0.18) !important;
}

.glass-modal :deep(.n-descriptions-table td),
.glass-modal :deep(.n-descriptions-table th) {
  border-color: rgba(148, 163, 184, 0.28) !important;
}
.app-dark .glass-modal :deep(.n-descriptions-table td),
.app-dark .glass-modal :deep(.n-descriptions-table th) {
  border-color: rgba(148, 163, 184, 0.18) !important;
}

.glass-modal :deep(.n-descriptions-table th) {
  color: var(--app-text-muted) !important;
  font-weight: 600;
}

.glass-descriptions :deep(.n-descriptions-table-header),
.glass-descriptions :deep(.n-descriptions-table-content) {
  background-color: transparent !important;
}

.glass-modal .code-wrap {
  background: rgba(148, 163, 184, 0.18);
  color: var(--app-text);
}
.app-dark .glass-modal .code-wrap {
  background: rgba(148, 163, 184, 0.16);
  color: var(--app-text);
}
</style>
