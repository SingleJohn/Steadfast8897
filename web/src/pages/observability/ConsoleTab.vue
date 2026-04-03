<script setup lang="ts">
import { NButton, NDivider, NGrid, NGridItem, NInput, NScrollbar, NSpace, NSwitch, NText } from 'naive-ui'

import ErrorBanner from '@/components/ErrorBanner.vue'
import PageSectionCard from '@/components/PageSectionCard.vue'

const consoleSearch = defineModel<string>('consoleSearch', { required: true })
const consoleIsLive = defineModel<boolean>('consoleIsLive', { required: true })

defineProps<{
  consoleError: string | null
  consoleLoading: boolean
  consoleOffset: number
  consoleSize: number
  consoleFilteredText: string
}>()

const emit = defineEmits<{
  (e: 'load-tail'): void
  (e: 'load-increment'): void
}>()
</script>

<template>
  <n-space vertical :size="16">
    <error-banner v-if="consoleError" :message="consoleError" />

    <page-section-card content-style="padding: 0;">
      <div class="filter-bar" style="padding-top: 12px; padding-bottom: 12px;">
        <n-grid cols="1 s:2 m:4" x-gap="12" y-gap="12" responsive="screen" align="center">
          <n-grid-item>
            <n-input v-model:value="consoleSearch" placeholder="搜索（包含匹配）" clearable />
          </n-grid-item>
          <n-grid-item>
            <n-space align="center">
              <n-text depth="3" size="small">实时追踪</n-text>
              <n-switch v-model:value="consoleIsLive" size="small" />
              <n-divider vertical />
              <n-button size="small" :loading="consoleLoading" @click="emit('load-tail')">刷新尾部</n-button>
            </n-space>
          </n-grid-item>
          <n-grid-item>
            <n-text depth="3" size="small">Offset: {{ consoleOffset }} / Size: {{ consoleSize }}</n-text>
          </n-grid-item>
          <n-grid-item>
            <n-space justify="end">
              <n-button size="small" :loading="consoleLoading" @click="emit('load-increment')">拉取增量</n-button>
            </n-space>
          </n-grid-item>
        </n-grid>
      </div>

      <n-divider style="margin: 0" />

      <n-scrollbar style="max-height: 70vh; padding: 12px 16px;">
        <pre class="console-log">{{ consoleFilteredText || (consoleLoading ? 'Loading...' : '') }}</pre>
      </n-scrollbar>
    </page-section-card>
  </n-space>
</template>

<style scoped>
.filter-bar {
  padding: 0 16px;
}

.console-log {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
  line-height: 1.45;
  color: var(--app-text);
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.35);
  border-radius: 12px;
  padding: 12px 14px;
  backdrop-filter: blur(var(--app-glass-blur));
  -webkit-backdrop-filter: blur(var(--app-glass-blur));
}
.app-dark .console-log {
  background: rgba(15, 23, 42, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.08);
}
</style>
