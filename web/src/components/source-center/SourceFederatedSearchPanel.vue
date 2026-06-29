<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NInput, NInputNumber, NSwitch, NTag, NTooltip, useMessage } from 'naive-ui'
import type { FederatedSearchError, FederatedSearchItem, FederatedSearchResponse } from '@/api/source'
import { copyText } from '@/utils/externalPlayers'

const props = defineProps<{
  keyword: string
  limit: number
  loading: boolean
  result: FederatedSearchResponse | null
  dryRun: boolean
  embyEnabled: boolean
  savingEmbyEnabled: boolean
  liveEnabled: boolean
  savingLiveEnabled: boolean
}>()

const emit = defineEmits<{
  'update:keyword': [value: string]
  'update:limit': [value: number]
  'update:dryRun': [value: boolean]
  'update:embyEnabled': [value: boolean]
  'update:liveEnabled': [value: boolean]
  search: []
}>()

const message = useMessage()
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

async function copySearchError(error: FederatedSearchError) {
  const text = [
    `Provider: ${error.provider_name}`,
    `SourceKey: ${error.source_key}`,
    `ErrorType: ${error.error_type}`,
    `LatencyMS: ${error.latency_ms}`,
    `Message: ${error.message}`,
  ].join('\n')
  const ok = await copyText(text)
  if (ok) message.success('Provider 错误已复制')
  else message.error('复制失败，请手动选中')
}
</script>

<template>
  <section class="source-panel federated-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">聚合搜索</h2>
        <p class="panel-subtitle">
          并发搜索全部已启用 Provider。<template v-if="dryRun">测试模式：只验证连通与命中，<strong>不写入</strong> source_items。</template><template v-else>结果写入 source_items 后可被 Emby 搜索读取。</template>
        </p>
      </div>
      <div v-if="result" class="summary-strip">
        <span>{{ result.total }} 条</span>
        <span>{{ result.latency_ms }} ms</span>
        <span>{{ successCount }}/{{ totalProviders }} 源成功</span>
        <NTag size="small" :type="result.cache_write ? 'success' : 'warning'" :bordered="false">
          {{ result.cache_write ? '已写缓存' : '未写缓存' }}
        </NTag>
      </div>
      <div class="emby-switches">
        <div class="emby-switch">
          <NTooltip>
            <template #trigger>
              <span id="source-emby-switch-label" class="emby-switch-label">Emby 搜索可见<span class="lbl-info" aria-label="Emby 搜索可见说明">?</span></span>
            </template>
            默认开启。开启后,已写入 source_items 的在线结果会出现在 Emby/Infuse 客户端搜索里;关闭则客户端搜索不返回在线源结果(后台聚合搜索不受影响)。
          </NTooltip>
          <NSwitch
            :value="embyEnabled"
            :loading="savingEmbyEnabled"
            size="small"
            aria-labelledby="source-emby-switch-label"
            @update:value="emit('update:embyEnabled', $event)"
          />
        </div>
        <div class="emby-switch">
          <NTooltip>
            <template #trigger>
              <span id="source-live-switch-label" class="emby-switch-label">同步直搜<span class="lbl-info" aria-label="同步直搜说明">?</span></span>
            </template>
            默认关闭。开启后,Emby 客户端搜索会实时跑一次跨源聚合搜索把命中写进缓存(同关键词 45 秒内只跑一次,单次最多等 10 秒),会增加搜索延迟和外站请求量;关闭则只读已有缓存。依赖上方“Emby 搜索可见”开启才有意义。
          </NTooltip>
          <NSwitch
            :value="liveEnabled"
            :loading="savingLiveEnabled"
            :disabled="!embyEnabled"
            size="small"
            aria-labelledby="source-live-switch-label"
            @update:value="emit('update:liveEnabled', $event)"
          />
        </div>
      </div>
    </div>

    <div class="search-row">
      <label class="field">
        <span class="field-label">关键词</span>
        <NInput
          :value="keyword"
          placeholder="关键词"
          clearable
          @keyup.enter="emit('search')"
          @update:value="emit('update:keyword', $event)"
        />
      </label>
      <label class="field">
        <span class="field-label">返回上限</span>
        <NInputNumber
          :value="limit"
          :min="1"
          :max="100"
          :step="10"
          @update:value="emit('update:limit', Number($event || 50))"
        />
      </label>
      <div class="dryrun-switch">
        <NTooltip>
          <template #trigger>
            <span class="dryrun-label">测试模式<span class="lbl-info" aria-label="测试模式说明">?</span></span>
          </template>
          开启后只测试各源连通与命中,不把结果写入 source_items(不污染媒体库/缓存);关闭则正常落库并对 Emby 搜索可见。
        </NTooltip>
        <NSwitch
          :value="dryRun"
          size="small"
          @update:value="emit('update:dryRun', $event)"
        />
      </div>
      <NButton type="primary" :loading="loading" @click="emit('search')">
        {{ dryRun ? '搜索测试' : '搜索' }}
      </NButton>
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
            <NButton size="tiny" quaternary class="copy-error" @click="copySearchError(error)">复制错误</NButton>
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
.emby-switches {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px 16px;
}
.emby-switch {
  align-items: center;
  flex-wrap: nowrap;
}
.emby-switch-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--app-text);
}
.lbl-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 7px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 10px;
  font-weight: 600;
  cursor: help;
}
.search-row {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 120px auto auto;
  align-items: end;
  gap: 10px;
}
.dryrun-switch {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-bottom: 6px;
}
.dryrun-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
}
.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.field-label {
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
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
.copy-error {
  margin-top: 6px;
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
