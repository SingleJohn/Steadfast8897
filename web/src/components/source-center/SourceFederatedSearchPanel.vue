<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { NButton, NInput, NInputNumber, NSwitch, NTag, NTooltip, useMessage } from 'naive-ui'
import { getSourceItemLines, materializeSourceSearchItem, refreshSourceItemDetail, type FederatedSearchError, type FederatedSearchItem, type FederatedSearchResponse, type SourceItemLinesResponse } from '@/api/source'
import { copyText } from '@/utils/externalPlayers'

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

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
  autoDisableSearchEnabled: boolean
  savingAutoDisableSearch: boolean
  autoDisablePlayEnabled: boolean
  savingAutoDisablePlay: boolean
}>()

const emit = defineEmits<{
  'update:keyword': [value: string]
  'update:limit': [value: number]
  'update:dryRun': [value: boolean]
  'update:embyEnabled': [value: boolean]
  'update:liveEnabled': [value: boolean]
  'update:autoDisableSearchEnabled': [value: boolean]
  'update:autoDisablePlayEnabled': [value: boolean]
  search: []
}>()

const message = useMessage()
const items = computed(() => props.result?.items || [])
const errors = computed(() => props.result?.errors || [])
const successCount = computed(() => props.result?.provider.success || 0)
const failedCount = computed(() => props.result?.provider.failed || 0)
const totalProviders = computed(() => props.result?.provider.total || 0)
const expandedItemUUID = shallowRef('')
const lineLoadingUUID = shallowRef('')
const openingItemUUID = shallowRef('')
const refreshingItemId = shallowRef<number | null>(null)
const itemLinesByUUID = shallowRef<Record<string, SourceItemLinesResponse>>({})

function posterStyle(item: FederatedSearchItem) {
  if (!item.poster_url) return {}
  return { backgroundImage: `url("${item.poster_url}")` }
}

function providerLine(item: FederatedSearchItem) {
  return item.providers.map((provider) => provider.name).join(' / ')
}

function userDetailPath(item: FederatedSearchItem) {
  return `/item/${item.public_uuid}`
}

function userDetailURL(item: FederatedSearchItem) {
  return `${window.location.origin}${window.location.pathname}${window.location.search}#${userDetailPath(item)}`
}

function preferredProvider(item: FederatedSearchItem) {
  return item.providers.find((provider) => provider.item_uuid === item.public_uuid) || item.providers[0]
}

async function openUserDetail(item: FederatedSearchItem) {
  const provider = preferredProvider(item)
  if (!provider) {
    message.error('没有可用 Provider')
    return
  }
  const popup = window.open('', '_blank')
  if (popup) {
    popup.opener = null
    popup.document.title = '正在准备在线条目'
    popup.document.body.textContent = '正在准备在线条目...'
  }
  openingItemUUID.value = item.public_uuid
  try {
    const materialized = await materializeSourceSearchItem({
      provider_id: provider.id,
      source_item_id: provider.source_item_id,
      title: item.title,
      item_type: item.item_type,
      normalized_kind: item.normalized_kind,
      year: item.year,
      region: item.region,
      poster_url: item.poster_url,
      remarks: item.remarks,
    })
    const url = userDetailURL({ ...item, public_uuid: materialized.public_uuid })
    if (popup) popup.location.href = url
    else window.location.href = url
  } catch (e: any) {
    if (popup) popup.close()
    message.error(e?.message || '打开用户端详情失败')
  } finally {
    if (openingItemUUID.value === item.public_uuid) openingItemUUID.value = ''
  }
}

function expandedLines(item: FederatedSearchItem) {
  return itemLinesByUUID.value[item.public_uuid] || null
}

function lineHealthType(status: string): TagType {
  switch (status) {
    case 'ok':
      return 'success'
    case 'error':
    case 'unhealthy':
      return 'error'
    case 'unknown':
      return 'warning'
    default:
      return 'default'
  }
}

async function toggleLines(item: FederatedSearchItem) {
  if (expandedItemUUID.value === item.public_uuid) {
    expandedItemUUID.value = ''
    return
  }
  expandedItemUUID.value = item.public_uuid
  if (itemLinesByUUID.value[item.public_uuid]) return
  lineLoadingUUID.value = item.public_uuid
  try {
    const lines = await getSourceItemLines(item.public_uuid)
    itemLinesByUUID.value = { ...itemLinesByUUID.value, [item.public_uuid]: lines }
  } catch (e: any) {
    message.error(e?.message || '加载线路失败')
  } finally {
    if (lineLoadingUUID.value === item.public_uuid) lineLoadingUUID.value = ''
  }
}

async function refreshLines(item: FederatedSearchItem, sourceItemId: number) {
  refreshingItemId.value = sourceItemId
  try {
    await refreshSourceItemDetail(sourceItemId)
    const lines = await getSourceItemLines(item.public_uuid)
    itemLinesByUUID.value = { ...itemLinesByUUID.value, [item.public_uuid]: lines }
    message.success('线路已刷新')
  } catch (e: any) {
    message.error(e?.message || '刷新线路失败')
  } finally {
    refreshingItemId.value = null
  }
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
          并发搜索全部已启用 Provider。<template v-if="dryRun">快速测试：只验证连通与命中，<strong>不写入</strong> source_items。</template><template v-else>正式搜索：命中会写入 source_items，之后可被 Emby 搜索读取。</template>
        </p>
      </div>
      <div v-if="result" class="summary-strip">
        <span>{{ result.total }} 条</span>
        <span>{{ result.latency_ms }} ms</span>
        <span>{{ successCount }}/{{ totalProviders }} 源成功</span>
        <span>{{ result.provider.concurrency || 1 }} 并发</span>
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
        <div class="emby-switch">
          <NTooltip>
            <template #trigger>
              <span id="source-auto-disable-search-label" class="emby-switch-label">搜索失败禁用<span class="lbl-info" aria-label="搜索失败禁用说明">?</span></span>
            </template>
            默认关闭。开启后,TVBox/CMS/DRPY/CSP 等在线 Provider 连续搜索失败达到阈值会自动停用;成功返回会清零失败计数。
          </NTooltip>
          <NSwitch
            :value="autoDisableSearchEnabled"
            :loading="savingAutoDisableSearch"
            size="small"
            aria-labelledby="source-auto-disable-search-label"
            @update:value="emit('update:autoDisableSearchEnabled', $event)"
          />
        </div>
        <div class="emby-switch">
          <NTooltip>
            <template #trigger>
              <span id="source-auto-disable-play-label" class="emby-switch-label">播放失败禁用<span class="lbl-info" aria-label="播放失败禁用说明">?</span></span>
            </template>
            默认关闭。开启后,智能选路代理播放连续失败达到阈值会自动停用对应在线 Provider;只作用于在线源,本地媒体播放保持原样。
          </NTooltip>
          <NSwitch
            :value="autoDisablePlayEnabled"
            :loading="savingAutoDisablePlay"
            size="small"
            aria-labelledby="source-auto-disable-play-label"
            @update:value="emit('update:autoDisablePlayEnabled', $event)"
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
              <button
                class="result-title-link"
                type="button"
                :disabled="openingItemUUID === item.public_uuid"
                @click="openUserDetail(item)"
              >
                {{ openingItemUUID === item.public_uuid ? '准备中...' : item.title }}
              </button>
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
              <NButton
                size="tiny"
                secondary
                type="primary"
                :loading="lineLoadingUUID === item.public_uuid"
                @click="toggleLines(item)"
              >
                {{ expandedItemUUID === item.public_uuid ? '收起线路' : '详情/线路' }}
              </NButton>
              <span
                v-for="provider in item.providers"
                :key="provider.item_uuid"
                class="provider-chip"
              >
                {{ provider.name }}
              </span>
            </div>
            <div v-if="expandedItemUUID === item.public_uuid" class="line-panel">
              <div v-if="lineLoadingUUID === item.public_uuid" class="empty-state">正在读取已缓存线路</div>
              <template v-else-if="expandedLines(item)">
                <article
                  v-for="group in expandedLines(item)?.alternatives || []"
                  :key="group.public_uuid"
                  class="line-group"
                >
                  <header class="line-group-head">
                    <div class="line-provider">
                      <strong>{{ group.provider_name || `Provider ${group.provider_id}` }}</strong>
                      <span>{{ group.provider_key }}</span>
                    </div>
                    <div class="line-tags">
                      <NTag size="small" :type="lineHealthType(group.provider_health)" :bordered="false">
                        {{ group.provider_health || 'unknown' }}
                      </NTag>
                      <NTag size="small" :type="group.detail_loaded ? 'success' : 'warning'" :bordered="false">
                        {{ group.detail_loaded ? '已加载详情' : '未加载详情' }}
                      </NTag>
                      <NTag size="small" :bordered="false">{{ group.play_source_count }} 线路</NTag>
                    </div>
                  </header>
                  <div class="line-meta">
                    <span>{{ group.title }}</span>
                    <span>{{ group.source_item_id }}</span>
                    <span v-if="group.remarks">{{ group.remarks }}</span>
                  </div>
                  <div v-if="group.play_sources.length > 0" class="play-line-list">
                    <div v-for="line in group.play_sources" :key="line.public_uuid" class="play-line">
                      <span class="play-line-name">{{ line.line_name }}</span>
                      <span v-if="line.episode_title" class="play-line-episode">{{ line.episode_title }}</span>
                      <NTag size="small" :type="lineHealthType(line.health_status)" :bordered="false">
                        {{ line.health_status || 'unknown' }}
                      </NTag>
                      <span class="play-line-mode">{{ line.parse_mode }}</span>
                      <span class="play-line-score">成功 {{ line.success_count }} / 失败 {{ line.failure_count }}</span>
                    </div>
                  </div>
                  <div v-else class="line-empty-row">
                    <span>暂无已缓存线路</span>
                    <NButton
                      size="tiny"
                      secondary
                      :loading="refreshingItemId === group.id"
                      @click="refreshLines(item, group.id)"
                    >
                      加载线路
                    </NButton>
                  </div>
                </article>
              </template>
              <div v-else class="empty-state">暂无线路信息</div>
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
.result-title-link {
  min-width: 0;
  border: 0;
  padding: 0;
  background: transparent;
  overflow: hidden;
  color: var(--app-text);
  cursor: pointer;
  font-size: 15px;
  font-weight: 700;
  line-height: 1.4;
  text-align: left;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.result-title-link:hover {
  color: var(--app-primary);
}
.result-title-link:disabled {
  cursor: wait;
  opacity: 0.68;
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
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}
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
.provider-chip {
  overflow: hidden;
  color: var(--app-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-chip:hover {
  background: var(--app-surface-2);
}
.line-panel {
  display: grid;
  gap: 8px;
  margin-top: 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  padding: 10px;
}
.line-group {
  display: grid;
  gap: 6px;
  border-bottom: 1px solid var(--app-border);
  padding-bottom: 8px;
}
.line-group:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}
.line-group-head,
.line-tags,
.play-line,
.line-empty-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.line-group-head,
.line-empty-row {
  justify-content: space-between;
}
.line-provider {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.line-provider strong {
  font-size: 13px;
}
.line-provider span,
.line-meta,
.play-line-mode,
.play-line-score,
.play-line-episode,
.line-empty-row {
  color: var(--app-text-muted);
  font-size: 12px;
}
.line-tags {
  flex-wrap: wrap;
  justify-content: flex-end;
}
.line-meta,
.play-line-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 10px;
}
.play-line {
  min-width: 0;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-1);
  padding: 5px 8px;
}
.play-line-name {
  font-size: 12px;
  font-weight: 700;
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
  .line-group-head,
  .line-empty-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
