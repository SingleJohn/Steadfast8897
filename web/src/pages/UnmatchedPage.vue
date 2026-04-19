<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton, NCard, NCheckbox, NEmpty, NIcon, NSelect, NSpin, NTag, NEllipsis,
} from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { RefreshOutline, CheckmarkCircleOutline, AlertCircleOutline } from '@vicons/ionicons5'
import {
  listUnmatchedItems,
  batchApplyIdentifyCandidates,
  applyItemIdentifyCandidate,
} from '@/api/client'

type Candidate = {
  id: string
  provider: string
  external_id: string
  title: string
  year?: number
  poster_url?: string
  score?: number
}

type UnmatchedItem = {
  id: string
  name: string
  type: string
  production_year?: number
  file_path?: string
  scan_status: string
  scan_error?: string | null
  scanned_at?: string | null
  identify_cooldown_until?: string | null
  candidates: Candidate[]
}

const { showToast } = useToast()

const typeOptions = [
  { label: '全部类型', value: '' },
  { label: '电影', value: 'Movie' },
  { label: '剧集', value: 'Series' },
]
const typeFilter = ref<string>('')
const loading = ref(false)
const items = ref<UnmatchedItem[]>([])
// Map<itemId, candidateId>; 未选为 undefined
const selected = ref<Map<string, string>>(new Map())
const applyingBatch = ref(false)
const applyingOne = ref<string>('')

const selectedCount = computed(() => selected.value.size)

async function refresh() {
  loading.value = true
  try {
    const resp = await listUnmatchedItems({ type: typeFilter.value || undefined, limit: 200 })
    items.value = (resp.items || []) as UnmatchedItem[]
    // 把已不存在于列表的选择清掉
    const presentIds = new Set(items.value.map((it) => it.id))
    for (const key of Array.from(selected.value.keys())) {
      if (!presentIds.has(key)) selected.value.delete(key)
    }
  } catch (err: any) {
    showToast(err?.message || '加载失败', 'error')
  } finally {
    loading.value = false
  }
}

function toggleCandidate(itemId: string, candidateId: string) {
  if (selected.value.get(itemId) === candidateId) {
    selected.value.delete(itemId)
  } else {
    selected.value.set(itemId, candidateId)
  }
}

function isSelected(itemId: string, candidateId: string) {
  return selected.value.get(itemId) === candidateId
}

async function applyOne(itemId: string, candidateId: string) {
  applyingOne.value = itemId
  try {
    await applyItemIdentifyCandidate(itemId, candidateId)
    showToast('已采纳,正在刮削', 'success')
    selected.value.delete(itemId)
    items.value = items.value.filter((it) => it.id !== itemId)
  } catch (err: any) {
    showToast(err?.message || '采纳失败', 'error')
  } finally {
    applyingOne.value = ''
  }
}

async function applyBatch() {
  if (selected.value.size === 0) return
  applyingBatch.value = true
  try {
    const payload = Array.from(selected.value.entries()).map(([item_id, candidate_id]) => ({ item_id, candidate_id }))
    const resp = await batchApplyIdentifyCandidates(payload)
    const okCount = resp.results.filter((r) => r.ok).length
    const failCount = resp.results.length - okCount
    if (failCount === 0) {
      showToast(`已采纳 ${okCount} 条`, 'success')
    } else {
      showToast(`采纳 ${okCount} 条成功,${failCount} 条失败`, failCount > okCount ? 'error' : 'success')
    }
    // 成功的从列表移除
    const failedIds = new Set(resp.results.filter((r) => !r.ok).map((r) => r.item_id))
    items.value = items.value.filter((it) => failedIds.has(it.id))
    // 已成功的清选
    for (const [itemId] of Array.from(selected.value.entries())) {
      if (!failedIds.has(itemId)) selected.value.delete(itemId)
    }
  } catch (err: any) {
    showToast(err?.message || '批量采纳失败', 'error')
  } finally {
    applyingBatch.value = false
  }
}

function formatDate(s?: string | null) {
  if (!s) return ''
  try {
    return new Date(s).toLocaleString('zh-CN', { hour12: false })
  } catch { return s }
}

function providerColor(name: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  switch (name) {
    case 'tmdb': return 'info'
    case 'tvdb': return 'success'
    case 'bangumi': return 'warning'
    case 'douban': return 'error'
    case 'fanart': return 'default'
    default: return 'default'
  }
}

onMounted(() => {
  void refresh()
})
</script>

<template>
  <page-shell title="未匹配" :icon="AppIcons.metadata" description="刮削识别失败或冷却中的项目。选择候选后单条或批量采纳。" body-class="unmatched-body">
    <template #actions>
      <n-select v-model:value="typeFilter" :options="typeOptions" size="small" style="width: 130px" @update:value="refresh" />
      <n-button size="small" :loading="loading" @click="refresh">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新
      </n-button>
      <n-button type="primary" size="small" :disabled="selectedCount === 0" :loading="applyingBatch" @click="applyBatch">
        批量采纳 ({{ selectedCount }})
      </n-button>
    </template>

    <n-spin :show="loading" style="min-height: 200px">
      <n-empty v-if="!loading && items.length === 0" description="没有未匹配项目" />

      <div v-else class="unmatched-list">
        <n-card v-for="item in items" :key="item.id" :bordered="false" class="glass-card unmatched-card">
          <div class="item-head">
            <div class="item-head-main">
              <div class="item-title">
                <span class="title-text">{{ item.name }}</span>
                <n-tag size="tiny" :bordered="false" :type="item.type === 'Movie' ? 'info' : 'success'">{{ item.type }}</n-tag>
                <n-tag v-if="item.production_year" size="tiny" :bordered="false">{{ item.production_year }}</n-tag>
              </div>
              <div v-if="item.file_path" class="item-path">
                <n-ellipsis style="max-width: 100%">{{ item.file_path }}</n-ellipsis>
              </div>
              <div class="item-meta">
                <span class="meta-pill err">
                  <n-icon :size="12"><AlertCircleOutline /></n-icon>
                  {{ item.scan_status || 'unidentified' }}
                </span>
                <span v-if="item.identify_cooldown_until" class="meta-pill">冷却至 {{ formatDate(item.identify_cooldown_until) }}</span>
                <span v-else-if="item.scanned_at" class="meta-pill">上次扫描 {{ formatDate(item.scanned_at) }}</span>
                <span v-if="item.scan_error" class="meta-pill">{{ item.scan_error }}</span>
              </div>
            </div>
          </div>

          <n-empty v-if="!item.candidates || item.candidates.length === 0" size="small" description="无候选,请前往详情手动搜索 TMDB" style="margin-top: 12px" />
          <div v-else class="candidate-grid">
            <div
              v-for="cand in item.candidates"
              :key="cand.id"
              class="candidate-card"
              :class="{ selected: isSelected(item.id, cand.id) }"
              @click="toggleCandidate(item.id, cand.id)"
            >
              <div class="candidate-poster">
                <img v-if="cand.poster_url" :src="cand.poster_url" :alt="cand.title" loading="lazy" @error="($event.target as HTMLImageElement).style.visibility = 'hidden'" />
              </div>
              <div class="candidate-body">
                <div class="candidate-title">
                  <n-ellipsis>{{ cand.title || cand.external_id }}</n-ellipsis>
                </div>
                <div class="candidate-meta">
                  <n-tag size="tiny" :bordered="false" :type="providerColor(cand.provider)">{{ cand.provider }}</n-tag>
                  <span v-if="cand.year" class="cand-year">{{ cand.year }}</span>
                  <span v-if="typeof cand.score === 'number'" class="cand-score">{{ (cand.score * 100).toFixed(0) }}%</span>
                </div>
              </div>
              <div class="candidate-actions">
                <n-checkbox :checked="isSelected(item.id, cand.id)" @click.stop @update:checked="toggleCandidate(item.id, cand.id)" />
                <n-button
                  size="tiny"
                  type="primary"
                  :loading="applyingOne === item.id"
                  @click.stop="applyOne(item.id, cand.id)"
                >
                  <template #icon><n-icon><CheckmarkCircleOutline /></n-icon></template>
                  采纳
                </n-button>
              </div>
            </div>
          </div>
        </n-card>
      </div>
    </n-spin>
  </page-shell>
</template>

<style scoped>
:deep(.unmatched-body) {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.unmatched-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.unmatched-card {
  padding: 4px 0;
}

.item-head-main { min-width: 0; }

.item-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.title-text {
  font-weight: 600;
  font-size: 14px;
  color: var(--app-text);
}
.item-path {
  font-size: 12px;
  color: var(--app-text-muted);
  margin-top: 4px;
}
.item-meta {
  margin-top: 6px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 11px;
  color: var(--app-text-muted);
}
.meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 8px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.04);
}
.meta-pill.err {
  background: rgba(239, 68, 68, 0.12);
  color: #f87171;
}

.candidate-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.candidate-card {
  display: flex;
  gap: 10px;
  padding: 8px;
  border-radius: 6px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}
.candidate-card:hover {
  border-color: rgba(var(--app-primary-rgb), 0.4);
  background: rgba(var(--app-primary-rgb), 0.04);
}
.candidate-card.selected {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.08);
}

.candidate-poster {
  width: 56px;
  min-width: 56px;
  height: 84px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.05);
  overflow: hidden;
  flex-shrink: 0;
}
.candidate-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.candidate-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.candidate-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
  line-height: 1.3;
}
.candidate-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--app-text-muted);
}
.cand-year { font-variant-numeric: tabular-nums; }
.cand-score {
  font-weight: 600;
  color: var(--app-primary);
  font-variant-numeric: tabular-nums;
}

.candidate-actions {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  align-items: flex-end;
  gap: 6px;
}
</style>
