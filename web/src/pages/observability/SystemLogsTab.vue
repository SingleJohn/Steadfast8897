<script setup lang="ts">
import { ref, watch, watchEffect, nextTick, onMounted } from 'vue'
import { NButton, NSwitch, NSelect, NIcon, NTag } from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import { getSystemLogs } from '@/api/client'

const LOG_LEVELS = ['ALL', 'INFO', 'WARN', 'ERROR'] as const
const LOG_COLORS: Record<string, string> = {
  ERROR: '#ef4444',
  WARN: '#f59e0b',
  INFO: '#10b981',
  DEBUG: '#64748b',
  TRACE: '#475569',
}
const LOG_BG: Record<string, string> = {
  ERROR: 'rgba(239, 68, 68, 0.08)',
  WARN: 'rgba(245, 158, 11, 0.06)',
}

const logRetentionOptions = [3, 7, 14, 30].map((d) => ({
  label: `${d} 天`,
  value: String(d),
}))

const logEntries = ref<any[]>([])
const logLevel = ref<string>('ALL')
const logAutoRefresh = ref(true)
const logRetentionDays = ref('7')
const logContainerRef = ref<HTMLElement | null>(null)

async function fetchLogs() {
  try {
    const res = await getSystemLogs(logLevel.value, 500)
    logEntries.value = (res as any)?.entries || []
    await nextTick()
    const el = logContainerRef.value
    if (el) el.scrollTop = el.scrollHeight
  } catch {
    /* ignore */
  }
}

watch(logLevel, () => {
  void fetchLogs()
})

watchEffect((onCleanup) => {
  if (!logAutoRefresh.value) return
  void fetchLogs()
  const t = window.setInterval(fetchLogs, 3000)
  onCleanup(() => clearInterval(t))
})

watch(
  logEntries,
  async () => {
    await nextTick()
    const el = logContainerRef.value
    if (el) el.scrollTop = el.scrollHeight
  },
  { deep: true },
)

onMounted(() => {
  void fetchLogs()
})
</script>

<template>
  <div class="logs-tab">
    <!-- Toolbar -->
    <div class="logs-toolbar">
      <div class="logs-toolbar__left">
        <span class="logs-toolbar__label">级别</span>
        <div class="level-pills">
          <button
            v-for="l in LOG_LEVELS"
            :key="l"
            class="level-pill"
            :class="{ 'level-pill--active': logLevel === l }"
            @click="logLevel = l"
          >
            {{ l }}
          </button>
        </div>
      </div>

      <div class="logs-toolbar__right">
        <div class="logs-toolbar__switch">
          <n-switch v-model:value="logAutoRefresh" size="small" />
          <span class="logs-toolbar__switch-label">自动刷新</span>
        </div>
        <div class="logs-toolbar__retention">
          <span class="logs-toolbar__label">保留</span>
          <n-select
            v-model:value="logRetentionDays"
            :options="logRetentionOptions"
            size="small"
            style="width: 90px"
          />
        </div>
        <n-button text size="small" @click="fetchLogs">
          <template #icon><n-icon :component="RefreshOutline" /></template>
        </n-button>
      </div>
    </div>

    <!-- Log viewer -->
    <div
      ref="logContainerRef"
      class="logs-viewer"
    >
      <div v-if="logEntries.length === 0" class="logs-empty">
        <span class="logs-empty__icon">📋</span>
        <span>暂无日志</span>
      </div>
      <template v-else>
        <div
          v-for="(log, i) in logEntries"
          :key="i"
          class="log-line"
          :style="{ background: LOG_BG[log.level] || 'transparent' }"
        >
          <span class="log-line__time">{{ log.timestamp?.slice(11, 23) }}</span>
          <n-tag
            size="tiny"
            :bordered="false"
            round
            :style="{ color: LOG_COLORS[log.level] || '#64748b', background: (LOG_COLORS[log.level] || '#64748b') + '18', fontWeight: 600 }"
            class="log-line__level"
          >
            {{ log.level }}
          </n-tag>
          <span class="log-line__target">{{ log.target }}</span>
          <span class="log-line__msg">{{ log.message }}</span>
        </div>
      </template>
    </div>

    <!-- Footer info -->
    <div class="logs-footer">
      <span>{{ logEntries.length }} 条日志</span>
      <span v-if="logAutoRefresh" class="logs-footer__live">
        <span class="logs-footer__dot" />
        自动刷新中
      </span>
    </div>
  </div>
</template>

<style scoped>
.logs-tab {
  display: flex;
  flex-direction: column;
  gap: 0;
}

/* Toolbar */
.logs-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 14px 20px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-card) var(--app-radius-card) 0 0;
  backdrop-filter: blur(var(--app-glass-blur));
  -webkit-backdrop-filter: blur(var(--app-glass-blur));
}

.logs-toolbar__left,
.logs-toolbar__right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logs-toolbar__label {
  font-size: 13px;
  color: var(--app-text-muted);
  white-space: nowrap;
}

.logs-toolbar__switch {
  display: flex;
  align-items: center;
  gap: 6px;
}

.logs-toolbar__switch-label {
  font-size: 13px;
  color: var(--app-text-muted);
}

.logs-toolbar__retention {
  display: flex;
  align-items: center;
  gap: 6px;
}

/* Level pills */
.level-pills {
  display: flex;
  gap: 4px;
}

.level-pill {
  padding: 4px 14px;
  border-radius: 20px;
  border: 1px solid var(--app-border);
  background: transparent;
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.level-pill:hover {
  border-color: var(--app-border-hover);
  color: var(--app-text);
}

.level-pill--active {
  background: rgba(var(--app-primary-rgb), 0.12);
  border-color: rgba(var(--app-primary-rgb), 0.3);
  color: var(--app-primary);
}

/* Log viewer */
.logs-viewer {
  background: #0a0e17;
  border-left: 1px solid var(--app-border);
  border-right: 1px solid var(--app-border);
  padding: 4px 0;
  max-height: 600px;
  min-height: 300px;
  overflow: auto;
  font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.8;
}

.logs-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 200px;
  color: #334155;
  font-size: 14px;
}

.logs-empty__icon {
  font-size: 28px;
  opacity: 0.5;
}

.log-line {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 1px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.02);
  transition: background 0.1s ease;
}

.log-line:hover {
  background: rgba(148, 163, 184, 0.04) !important;
}

.log-line__time {
  color: #475569;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  user-select: all;
}

.log-line__level {
  flex-shrink: 0;
  min-width: 48px;
  text-align: center;
}

.log-line__target {
  color: #64748b;
  flex-shrink: 0;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-line__msg {
  color: #cbd5e1;
  word-break: break-all;
  user-select: all;
}

/* Footer */
.logs-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 20px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-top: none;
  border-radius: 0 0 var(--app-radius-card) var(--app-radius-card);
  font-size: 12px;
  color: var(--app-text-muted);
  backdrop-filter: blur(var(--app-glass-blur));
  -webkit-backdrop-filter: blur(var(--app-glass-blur));
}

.logs-footer__live {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--app-primary);
}

.logs-footer__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-primary);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* Responsive */
@media (max-width: 640px) {
  .logs-toolbar {
    padding: 12px 14px;
  }

  .log-line__target {
    display: none;
  }

  .logs-toolbar__retention {
    display: none;
  }
}
</style>
