<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NInput, NInputNumber, NSwitch, NTag } from 'naive-ui'
import type { FederatedSearchError, FederatedSearchItem, FederatedSearchResponse } from '@/api/source'

const props = defineProps<{
  keyword: string
  limit: number
  loading: boolean
  result: FederatedSearchResponse | null
  embyEnabled: boolean
  savingEmbyEnabled: boolean
}>()

const emit = defineEmits<{
  'update:keyword': [value: string]
  'update:limit': [value: number]
  'update:embyEnabled': [value: boolean]
  search: []
}>()

const items = computed(() => props.result?.items || [])
const errors = computed(() => props.result?.errors || [])
const successCount = computed(() => props.result?.provider.success || 0)
const failedCount = computed(() => props.result?.provider.failed || 0)
const totalProviders = computed(() => props.result?.provider.total || 0)

function posterStyle(item: FederatedSearchItem) {
  if (!item.poster_url) return {}
  return { backgroundImage: `url("${item.poster_url}")` }
}

function providerLine(item: FederatedSearchItem) {
  return item.providers.map((provider) => provider.name).join(' / ')
}

function detailPath(item: FederatedSearchItem) {
  return `/item/${item.public_uuid}`
}

function providerDetailPath(provider: FederatedSearchItem['providers'][number]) {
  return `/item/${provider.item_uuid}`
}

function playPath(item: FederatedSearchItem) {
  return item.item_type === 'Movie' ? `/play/${item.public_uuid}` : ''
}

function errorType(error: FederatedSearchError) {
  switch (error.error_type) {
    case 'timeout':
      return 'warning'
    case 'site_unavailable':
    case 'http_status':
      return 'error'
    default:
      return 'default'
  }
}
</script>

<template>
  <section class="source-panel federated-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">聚合搜索</h2>
        <p class="panel-subtitle">并发搜索已启用 Provider，结果写入 source_items 后可被 Emby 搜索读取。</p>
      </div>
      <div v-if="result" class="summary-strip">
        <span>{{ result.total }} 条</span>
        <span>{{ result.latency_ms }} ms</span>
        <span>{{ successCount }}/{{ totalProviders }} 源成功</span>
      </div>
      <div class="emby-switch">
        <span>Emby</span>
        <NSwitch
          :value="embyEnabled"
          :loading="savingEmbyEnabled"
          size="small"
          @update:value="emit('update:embyEnabled', $event)"
        />
      </div>
    </div>

    <div class="search-row">
      <NInput
        :value="keyword"
        placeholder="关键词"
        clearable
        @keyup.enter="emit('search')"
        @update:value="emit('update:keyword', $event)"
      />
      <NInputNumber
        :value="limit"
        :min="1"
        :max="100"
        :step="10"
        @update:value="emit('update:limit', Number($event || 50))"
      />
      <NButton type="primary" :loading="loading" @click="emit('search')">搜索</NButton>
    </div>

    <div v-if="result" class="federated-grid">
      <div class="result-list">
        <article v-for="item in items" :key="item.public_uuid" class="result-item">
          <div class="poster" :class="{ empty: !item.poster_url }" :style="posterStyle(item)">
            <span v-if="!item.poster_url">{{ item.item_type }}</span>
          </div>
          <div class="result-body">
            <div class="result-title-row">
              <h3 class="result-title">{{ item.title }}</h3>
              <NTag size="small" :bordered="false">{{ item.provider_count }} 源</NTag>
            </div>
            <div class="result-meta">
              <span>{{ item.item_type }}</span>
              <span v-if="item.year">{{ item.year }}</span>
              <span>{{ item.normalized_kind }}</span>
              <span v-if="item.region">{{ item.region }}</span>
            </div>
            <p v-if="item.remarks" class="result-remarks">{{ item.remarks }}</p>
            <div class="provider-line">{{ providerLine(item) }}</div>
            <div class="result-actions">
              <RouterLink class="action-link" :to="detailPath(item)">详情/线路</RouterLink>
              <RouterLink v-if="playPath(item)" class="action-link" :to="playPath(item)">播放</RouterLink>
              <RouterLink
                v-for="provider in item.providers"
                :key="provider.item_uuid"
                class="provider-chip"
                :to="providerDetailPath(provider)"
              >
                {{ provider.name }}
              </RouterLink>
            </div>
          </div>
        </article>
        <div v-if="items.length === 0" class="empty-state">暂无聚合结果</div>
      </div>

      <aside class="error-panel">
        <div class="error-head">
          <span>Provider 明细</span>
          <NTag size="small" :type="failedCount > 0 ? 'warning' : 'success'">{{ failedCount }} 失败</NTag>
        </div>
        <div v-if="errors.length > 0" class="error-list">
          <div v-for="error in errors" :key="`${error.provider_id}:${error.error_type}`" class="error-item">
            <div class="error-title">
              <span>{{ error.provider_name }}</span>
              <NTag size="small" :type="errorType(error)" :bordered="false">{{ error.error_type }}</NTag>
            </div>
            <div class="error-meta">{{ error.source_key }} · {{ error.latency_ms }} ms</div>
            <div class="error-message">{{ error.message }}</div>
          </div>
        </div>
        <div v-else class="empty-state">全部 Provider 正常返回</div>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.source-panel {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
  padding: 16px;
}
.panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}
.panel-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}
.panel-subtitle {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
}
.summary-strip,
.emby-switch {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  color: var(--app-text-muted);
  font-size: 12px;
}
.emby-switch {
  align-items: center;
  flex-wrap: nowrap;
}
.search-row {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 120px auto;
  gap: 10px;
}
.federated-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(260px, 34%);
  gap: 14px;
  margin-top: 14px;
}
.result-list,
.error-panel {
  min-width: 0;
}
.result-item {
  display: grid;
  grid-template-columns: 64px minmax(0, 1fr);
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--app-border);
}
.result-item:last-child {
  border-bottom: 0;
}
.poster {
  width: 64px;
  aspect-ratio: 2 / 3;
  border-radius: 6px;
  background-color: var(--app-surface-2);
  background-position: center;
  background-size: cover;
  display: grid;
  place-items: center;
  color: var(--app-text-muted);
  font-size: 11px;
}
.poster.empty {
  border: 1px dashed var(--app-border);
}
.result-body {
  min-width: 0;
}
.result-title-row,
.error-head,
.error-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.result-title {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
}
.result-meta,
.provider-line,
.error-meta,
.error-message {
  color: var(--app-text-muted);
  font-size: 12px;
}
.result-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 4px;
}
.result-remarks {
  margin: 6px 0 0;
  color: var(--app-text);
  font-size: 13px;
}
.provider-line {
  margin-top: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.result-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}
.action-link,
.provider-chip {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  max-width: 160px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 0 8px;
  color: var(--app-text);
  font-size: 12px;
  text-decoration: none;
}
.action-link {
  border-color: rgba(59, 130, 246, 0.45);
  color: #2563eb;
}
.provider-chip {
  overflow: hidden;
  color: var(--app-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.action-link:hover,
.provider-chip:hover {
  background: var(--app-surface-2);
}
.error-panel {
  border-left: 1px solid var(--app-border);
  padding-left: 14px;
}
.error-head {
  margin-bottom: 8px;
  font-weight: 700;
}
.error-list {
  display: grid;
  gap: 10px;
}
.error-item {
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 10px;
  background: var(--app-surface-2);
}
.error-title {
  font-size: 13px;
  font-weight: 700;
}
.error-meta,
.error-message {
  margin-top: 5px;
}
.error-message {
  overflow-wrap: anywhere;
}
.empty-state {
  padding: 18px 0;
  color: var(--app-text-muted);
  font-size: 13px;
}
@media (max-width: 900px) {
  .panel-head,
  .error-head,
  .error-title {
    align-items: flex-start;
  }
  .search-row,
  .federated-grid {
    grid-template-columns: 1fr;
  }
  .error-panel {
    border-left: 0;
    border-top: 1px solid var(--app-border);
    padding-top: 14px;
    padding-left: 0;
  }
}
</style>
