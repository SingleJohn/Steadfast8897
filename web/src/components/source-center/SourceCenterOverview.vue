<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NTag } from 'naive-ui'
import type { SourceConfig, SourceParser, SourceProvider, SourceRuntimeInvocation, SourceView } from '@/api/source'

const props = defineProps<{
  configs: SourceConfig[]
  providers: SourceProvider[]
  parsers: SourceParser[]
  views: SourceView[]
  invocations: SourceRuntimeInvocation[]
  loading: boolean
}>()

const emit = defineEmits<{
  navigate: [tab: 'configs' | 'providers' | 'views' | 'parsers' | 'audit']
  refresh: []
}>()

const enabledProviders = computed(() => props.providers.filter((provider) => provider.Enabled).length)
const exposedViews = computed(() => props.views.filter((view) => view.ExposeToEmby).length)
const enabledParsers = computed(() => props.parsers.filter((parser) => parser.Enabled).length)
const recentErrors = computed(() => props.invocations.filter((item) => item.Status === 'error').slice(0, 5))

const providerHealth = computed(() => {
  const summary = {
    ok: 0,
    error: 0,
    unknown: 0,
  }
  for (const provider of props.providers) {
    if (provider.HealthStatus === 'ok') summary.ok += 1
    else if (provider.HealthStatus === 'error') summary.error += 1
    else summary.unknown += 1
  }
  return summary
})

const metrics = computed(() => [
  { key: 'configs', label: '配置包', value: props.configs.length, helper: 'TVBox/CMS 导入' },
  { key: 'providers', label: '站点', value: props.providers.length, helper: `${enabledProviders.value} 启用` },
  { key: 'health', label: '健康', value: providerHealth.value.ok, helper: `${providerHealth.value.error} 失败 / ${providerHealth.value.unknown} 未探活` },
  { key: 'views', label: '在线虚拟库', value: props.views.length, helper: `${exposedViews.value} 暴露给 Emby` },
  { key: 'parsers', label: '解析器', value: props.parsers.length, helper: `${enabledParsers.value} 启用` },
])

function formatTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>

<template>
  <section class="source-overview" aria-label="在线媒体总览">
    <div class="overview-toolbar">
      <div>
        <h2 class="overview-title">在线媒体总览</h2>
        <p class="overview-copy">从配置包到站点、在线虚拟库与运行审计的当前管理视图。</p>
      </div>
      <div class="overview-actions">
        <NButton :loading="loading" @click="emit('refresh')">刷新</NButton>
        <NButton type="primary" @click="emit('navigate', 'configs')">导入配置</NButton>
        <NButton secondary @click="emit('navigate', 'audit')">查看运行审计</NButton>
      </div>
    </div>

    <div class="metric-grid">
      <button
        v-for="metric in metrics"
        :key="metric.key"
        class="metric-tile"
        type="button"
        @click="metric.key === 'health' ? emit('navigate', 'providers') : emit('navigate', metric.key as any)"
      >
        <span class="metric-label">{{ metric.label }}</span>
        <strong class="metric-value">{{ metric.value }}</strong>
        <span class="metric-helper">{{ metric.helper }}</span>
      </button>
    </div>

    <div class="overview-split">
      <section class="overview-section">
        <div class="section-head">
          <h3 class="section-title">站点健康</h3>
          <NButton text size="small" @click="emit('navigate', 'providers')">进入站点</NButton>
        </div>
        <div class="health-row">
          <NTag type="success" :bordered="false">可用 {{ providerHealth.ok }}</NTag>
          <NTag type="error" :bordered="false">失败 {{ providerHealth.error }}</NTag>
          <NTag :bordered="false">未探活 {{ providerHealth.unknown }}</NTag>
        </div>
      </section>

      <section class="overview-section">
        <div class="section-head">
          <h3 class="section-title">最近错误</h3>
          <NButton text size="small" @click="emit('navigate', 'audit')">进入运行审计</NButton>
        </div>
        <div v-if="recentErrors.length > 0" class="error-list">
          <div v-for="item in recentErrors" :key="item.ID" class="error-line">
            <span class="error-method">{{ item.Method }}</span>
            <span class="error-meta">{{ item.RuntimeKind }} / {{ item.ProviderName || `Provider ${item.ProviderID || '-'}` }}</span>
            <span class="error-type">{{ item.ErrorType || item.Status }}</span>
            <span class="error-time">{{ formatTime(item.InvokedAt) }}</span>
          </div>
        </div>
        <div v-else class="empty-line">暂无最近运行时错误</div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.source-overview {
  display: grid;
  gap: 16px;
}

.overview-toolbar,
.overview-split,
.section-head,
.error-line {
  display: flex;
  gap: 16px;
}

.overview-toolbar {
  align-items: flex-start;
  justify-content: space-between;
}

.overview-title,
.section-title {
  margin: 0;
  font-weight: 700;
}

.overview-title {
  font-size: 18px;
}

.overview-copy {
  margin: 5px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
}

.overview-actions,
.health-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px;
}

.metric-tile {
  display: grid;
  min-width: 0;
  gap: 5px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
  padding: 14px;
  color: var(--app-text);
  text-align: left;
  cursor: pointer;
}

.metric-tile:hover,
.metric-tile:focus-visible {
  border-color: rgba(59, 130, 246, 0.55);
  background: var(--app-surface-2);
}

.metric-label,
.metric-helper,
.error-meta,
.error-type,
.error-time,
.empty-line {
  color: var(--app-text-muted);
  font-size: 12px;
}

.metric-value {
  font-size: 24px;
  line-height: 1.1;
}

.overview-split {
  align-items: stretch;
}

.overview-section {
  min-width: 0;
  flex: 1;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
  padding: 14px;
}

.section-head {
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.section-title {
  font-size: 14px;
}

.error-list {
  display: grid;
  gap: 8px;
}

.error-line {
  align-items: center;
  min-width: 0;
  border-bottom: 1px solid var(--app-border);
  padding-bottom: 8px;
  font-size: 12px;
}

.error-line:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}

.error-method {
  min-width: 52px;
  font-weight: 700;
}

.error-meta {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.error-type {
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1080px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .overview-split,
  .overview-toolbar {
    flex-direction: column;
  }
}

@media (max-width: 640px) {
  .metric-grid {
    grid-template-columns: 1fr;
  }

  .error-line {
    align-items: flex-start;
    flex-direction: column;
    gap: 3px;
  }
}
</style>
