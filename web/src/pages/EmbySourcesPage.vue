<script setup lang="ts">
import type { FormInst } from 'naive-ui'
import {
  NButton,
  NCard,
  NCollapse,
  NCollapseItem,
  NDivider,
  NDynamicTags,
  NEmpty,
  NForm,
  NFormItem,
  NGrid,
  NGridItem,
  NIcon,
  NInput,
  NInputNumber,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpace,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { CloseOutline, AddOutline, ArrowUpOutline, ArrowDownOutline, TvOutline, CheckmarkCircle, AlertCircle, RefreshOutline, GitNetworkOutline } from '@vicons/ionicons5'

import PageShell from '@/components/PageShell.vue'
import StickyActionBar from '@/components/StickyActionBar.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import { useConfigDraft } from '@/composables/useConfigDraft'
import { checkEmbySource, getEmbyHealth, type EmbyHealthItem } from '@/api/health'
import type { Config, EmbySourceConfig, RouteRuleConfig } from '@/types'
import { AppIcons } from '@/icons/appIcons'

const message = useMessage()
const { loading, saving, loadError, draft, hasChanges, refresh, resetDraft, saveDraft } = useConfigDraft(message)
const formRef = ref<FormInst | null>(null)
const healthBySourceId = ref<Record<string, EmbyHealthItem>>({})
const checking = ref<Record<string, boolean>>({})

const activeSourceId = ref('')

function sourceTabName(id: string, idx: number) {
  return id?.trim() || `source-${idx}`
}

const activeSource = computed(() => {
  const sources = draft.value?.sources || []
  if (sources.length === 0) return null
  const idx = sources.findIndex((item, index) => sourceTabName(item.id, index) === activeSourceId.value)
  if (idx >= 0) return sources[idx]
  return sources[0]
})

const activeSourceIndex = computed(() => {
  if (!activeSource.value || !draft.value) return -1
  return draft.value.sources.findIndex((item) => item === activeSource.value)
})

watch(
  () => (draft.value?.sources || []).map((item, index) => sourceTabName(item.id, index)),
  (names, oldNames) => {
    if (names.length === 0) {
      activeSourceId.value = ''
      return
    }
    if (!names.includes(activeSourceId.value)) {
      const prevIndex = oldNames?.indexOf(activeSourceId.value) ?? -1
      if (prevIndex >= 0) {
        activeSourceId.value = names[Math.min(prevIndex, names.length - 1)]
        return
      }
      activeSourceId.value = names[0]
    }
  },
  { immediate: true },
)

function removeActiveSource() {
  if (!draft.value || activeSourceIndex.value < 0) return
  draft.value.sources.splice(activeSourceIndex.value, 1)
}

function genId(prefix: string) {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
}

const pathRuleSetOptions = computed(() => {
  const rs = draft.value?.path_rule_sets || []
  return rs.map((it) => ({ label: it.name ? `${it.name} (${it.id})` : it.id, value: it.id }))
})

const poolOptions = computed(() => {
  const pools = draft.value?.resource_pools || []
  return pools.map((it) => ({ label: it.name ? `${it.name} (${it.id})` : it.id, value: it.id }))
})

function nextListenPort() {
  const used = new Set<number>()
  for (const s of draft.value?.sources || []) {
    const p = Number(s.listen_port || 0)
    if (p > 0) used.add(p)
  }
  let p = 18889
  while (used.has(p) && p < 65535) p++
  return p
}

function addSource() {
  if (!draft.value) return
  const s: EmbySourceConfig = {
    id: genId('emby'),
    name: '',
    enabled: true,
    listen_host: '0.0.0.0',
    listen_port: nextListenPort(),
    stream_path_prefix: '/stream',
    upstream: { mode: 'external', host: '', base_path: '', api_key: '' },
    routes: [],
  }
  draft.value.sources.push(s)
}

function addRoute(source: EmbySourceConfig) {
  const r: RouteRuleConfig = {
    id: genId('route'),
    enabled: true,
    priority: 10,
    match: { real_path_prefix: [], real_path_regex: [] },
    path_rule_set_id: '',
    pool_id: '',
    require_mapping: true,
  }
  source.routes.push(r)
}

function moveRoute(source: EmbySourceConfig, index: number, dir: 'up' | 'down') {
  if (dir === 'up' && index > 0) {
    [source.routes[index], source.routes[index - 1]] = [source.routes[index - 1], source.routes[index]]
  } else if (dir === 'down' && index < source.routes.length - 1) {
    [source.routes[index], source.routes[index + 1]] = [source.routes[index + 1], source.routes[index]]
  }
}

async function onSave() {
  if (!draft.value) return
  await formRef.value?.validate().catch(() => false)

  const enabled = (draft.value.sources || []).filter((s) => Boolean(s.enabled))
  const portToSources = new Map<number, string[]>()
  for (const s of enabled) {
    const port = Number(s.listen_port || 0)
    if (port <= 0) {
      message.error(`Source ${s.name || s.id}: Listen Port 未设置`)
      return
    }
    const key = port
    const arr = portToSources.get(key) || []
    arr.push(s.name || s.id)
    portToSources.set(key, arr)
  }
  const dup = Array.from(portToSources.entries()).filter(([, ids]) => ids.length > 1)
  if (dup.length > 0) {
    const detail = dup
      .map(([port, ids]) => `${port}: ${ids.join(', ')}`)
      .join('；')
    message.error(`Listen Port 重复（会导致启动失败）：${detail}`)
    return
  }

  const ok = await saveDraft({ successMessage: '保存成功（配置热更新已生效）' })
  if (ok) message.success('保存成功')
}

async function refreshHealth() {
  try {
    const resp = await getEmbyHealth()
    const items = Array.isArray(resp.items) ? resp.items : []
    const map: Record<string, EmbyHealthItem> = {}
    for (const it of items) {
      if (it && it.source_id) map[it.source_id] = it
    }
    healthBySourceId.value = map
  } catch {
    healthBySourceId.value = {}
  }
}

async function checkSource(id: string) {
  const sid = String(id || '')
  if (!sid) return
  if (checking.value[sid]) return
  checking.value = { ...checking.value, [sid]: true }
  try {
    const item = await checkEmbySource(sid)
    healthBySourceId.value = { ...healthBySourceId.value, [sid]: item }
    if (item.ok) message.success(`${sid} upstream 正常`)
    else message.error(`${sid} upstream 异常: ${item.error || 'unknown'}`)
  } catch (e) {
    message.error(`检测失败: ${(e as Error).message}`)
  } finally {
    checking.value = { ...checking.value, [sid]: false }
  }
}

onMounted(() => {
  refresh()
  refreshHealth()
})
</script>

<template>
  <page-shell title="Emby 源" :icon="AppIcons.emby" description="管理多个 Emby Source：监听、上游与路由（日志/安全/真实 IP 默认挂载）。" body-class="config-content">
    <error-banner v-if="loadError" :message="loadError" style="margin-bottom: 16px" />

    <n-form v-if="draft" ref="formRef" :model="draft as Config" label-placement="top" size="medium">
      <n-card :bordered="false" class="glass-card section-card" title="Sources">
        <template #header-extra>
          <n-button size="small" secondary @click="addSource">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            新增 Source
          </n-button>
        </template>

        <n-empty v-if="draft.sources.length === 0" description="暂无 Source 配置" />

        <div v-else class="source-tabs-layout">
          <!-- Capsule Tabs -->
          <n-tabs v-model:value="activeSourceId" type="card" animated class="source-tabs">
            <n-tab-pane
              v-for="(s, idx) in draft.sources"
              :key="s.id || idx"
              :name="sourceTabName(s.id, idx)"
            >
              <template #tab>
                <span class="tab-dot" :style="{ background: s.enabled ? '#10b981' : '#94a3b8' }" />
                <span class="tab-label-text">{{ s.name?.trim() || '未命名' }}</span>
                <n-tag v-if="!s.enabled" size="tiny" :bordered="false" type="warning">停用</n-tag>
              </template>
            </n-tab-pane>
          </n-tabs>

            <n-card
              v-if="activeSource"
              size="small"
              :bordered="false"
              class="glass-card source-card"
            >
              <template #header>
                <n-space align="center" :size="12">
                  <div class="icon-box">
                    <n-icon :size="20"><TvOutline /></n-icon>
                  </div>
                  <div class="header-info">
                    <n-input v-model:value="activeSource.name" placeholder="名称" size="small" class="name-input" />
                    <n-text depth="3" class="id-text">{{ activeSource.id }}</n-text>
                  </div>
                </n-space>
              </template>
              <template #header-extra>
                <n-space align="center" :size="8" class="card-actions">
                  <n-switch v-model:value="activeSource.enabled" size="small" />
                  <n-divider vertical />
                  <div class="health-status">
                    <n-tag
                      v-if="healthBySourceId[activeSource.id]"
                      size="small"
                      round
                      :bordered="false"
                      :type="healthBySourceId[activeSource.id].ok ? 'success' : 'error'"
                    >
                      <template #icon>
                        <n-icon><component :is="healthBySourceId[activeSource.id].ok ? CheckmarkCircle : AlertCircle" /></n-icon>
                      </template>
                      {{ healthBySourceId[activeSource.id].ok ? 'UP' : 'DOWN' }}
                    </n-tag>
                    <n-button
                      quaternary
                      circle
                      size="tiny"
                      :loading="checking[activeSource.id]"
                      @click="checkSource(activeSource.id)"
                    >
                      <template #icon><n-icon><RefreshOutline /></n-icon></template>
                    </n-button>
                  </div>
                  <n-divider vertical />
                  <n-button quaternary circle size="small" type="error" @click="removeActiveSource">
                    <template #icon><n-icon><CloseOutline /></n-icon></template>
                  </n-button>
                </n-space>
              </template>

              <n-form label-placement="left" label-width="150" size="small" class="source-body source-compact-form">
                <n-collapse :default-expanded-names="['listen', 'upstream']" display-directive="show">
                  <n-collapse-item title="监听配置" name="listen">
                    <n-grid cols="2" x-gap="12">
                      <n-grid-item>
                        <n-form-item label="监听 Host">
                          <n-input v-model:value="activeSource.listen_host" placeholder="0.0.0.0" />
                        </n-form-item>
                      </n-grid-item>
                      <n-grid-item>
                        <n-form-item label="监听端口">
                          <n-input-number v-model:value="activeSource.listen_port" style="width: 100%" :min="1" :max="65535" :show-button="false" />
                        </n-form-item>
                      </n-grid-item>
                    </n-grid>
                    <n-form-item label="直链路径前缀 (Stream Path)">
                      <n-input v-model:value="activeSource.stream_path_prefix" placeholder="/stream" />
                    </n-form-item>
                  </n-collapse-item>

                  <n-collapse-item title="Emby 上游" name="upstream">
                    <n-form-item label="上游模式">
                      <n-radio-group v-model:value="activeSource.upstream.mode">
                        <n-radio-button value="self">本端 (FYMS)</n-radio-button>
                        <n-radio-button value="external">外部 Emby</n-radio-button>
                      </n-radio-group>
                    </n-form-item>
                    <template v-if="activeSource.upstream.mode !== 'self'">
                      <n-form-item label="Host (例如 http://192.168.1.10:8096)">
                        <n-input v-model:value="activeSource.upstream.host" placeholder="http://127.0.0.1:8096" />
                      </n-form-item>
                      <n-grid cols="2" x-gap="12">
                        <n-grid-item>
                          <n-form-item label="API Key">
                            <n-input
                              v-model:value="activeSource.upstream.api_key"
                              type="password"
                              show-password-on="click"
                              placeholder="Required"
                            />
                          </n-form-item>
                        </n-grid-item>
                        <n-grid-item>
                          <n-form-item label="Base Path">
                            <n-input v-model:value="activeSource.upstream.base_path" placeholder="可选" />
                          </n-form-item>
                        </n-grid-item>
                      </n-grid>
                    </template>
                    <n-text v-else depth="3" style="font-size: 13px">
                      使用本端 FYMS 数据库直接解析媒体路径，无需配置外部 Emby 上游。
                    </n-text>
                  </n-collapse-item>
                </n-collapse>

                <!-- Routes -->
                <div class="routes-section">
                  <n-divider style="margin: 16px 0">
                    <n-space align="center">
                      <n-icon color="var(--app-primary)"><GitNetworkOutline /></n-icon>
                      <n-text strong>路由规则 (Routes)</n-text>
                    </n-space>
                  </n-divider>

                  <div v-if="activeSource.routes.length === 0" class="empty-routes">
                    <n-empty description="暂无路由规则，所有请求将回退到反代模式">
                      <template #extra>
                        <n-button size="small" type="primary" dashed @click="addRoute(activeSource)">
                          <template #icon><n-icon><AddOutline /></n-icon></template>
                          新增规则
                        </n-button>
                      </template>
                    </n-empty>
                  </div>

                  <div v-else class="routes-container">
                    <n-card
                      v-for="(r, rIdx) in activeSource.routes"
                      :key="r.id || rIdx"
                      size="small"
                      :bordered="false"
                      class="route-item"
                    >
                        <div class="route-layout">
                          <div class="route-meta">
                            <n-space vertical :size="2">
                              <n-tag :bordered="false" size="small" type="warning" class="priority-tag">P{{ r.priority }}</n-tag>
                              <n-switch v-model:value="r.enabled" size="small" />
                            </n-space>
                          </div>
                          
                          <div class="route-content">
                            <n-grid cols="1 l:2" x-gap="16" y-gap="8" responsive="screen">
                              <n-grid-item>
                                <n-form-item label="匹配条件" :show-label="false">
                                  <n-space vertical :size="8" style="width: 100%">
                                    <n-dynamic-tags v-model:value="r.match.real_path_prefix" :input-props="{ placeholder: 'Prefix...' }">
                                      <template #trigger="{ activate, disabled }">
                                        <n-button size="tiny" dashed block @click="activate" :disabled="disabled">
                                          <template #icon><n-icon><AddOutline /></n-icon></template>
                                          Prefix
                                        </n-button>
                                      </template>
                                    </n-dynamic-tags>
                                    <n-dynamic-tags v-model:value="r.match.real_path_regex" :input-props="{ placeholder: 'Regex...' }" type="info">
                                      <template #trigger="{ activate, disabled }">
                                        <n-button size="tiny" dashed block @click="activate" :disabled="disabled">
                                          <template #icon><n-icon><AddOutline /></n-icon></template>
                                          Regex
                                        </n-button>
                                      </template>
                                    </n-dynamic-tags>
                                  </n-space>
                                </n-form-item>
                              </n-grid-item>
                              <n-grid-item>
                                <n-space vertical :size="0">
                                  <n-form-item 
                                    label="映射集" 
                                    label-placement="left" 
                                    :label-style="{ width: '70px' }"
                                    size="small" 
                                    :show-feedback="false" 
                                    style="margin-bottom: 8px"
                                  >
                                    <n-select v-model:value="r.path_rule_set_id" :options="pathRuleSetOptions" placeholder="请选择映射集" />
                                  </n-form-item>
                                  <n-form-item 
                                    label="资源池" 
                                    label-placement="left" 
                                    :label-style="{ width: '70px' }"
                                    size="small" 
                                    :show-feedback="false"
                                  >
                                    <n-select v-model:value="r.pool_id" :options="poolOptions" placeholder="请选择资源池" />
                                  </n-form-item>
                                </n-space>
                              </n-grid-item>
                            </n-grid>
                            
                            <div class="route-options">
                              <n-space size="small">
                                <n-form-item label="Priority" label-placement="left" :show-feedback="false">
                                  <n-input-number v-model:value="r.priority" size="tiny" style="width: 60px" :show-button="false" />
                                </n-form-item>
                                <n-switch v-model:value="r.require_mapping" size="small" />
                                <n-tag
                                  size="small"
                                  round
                                  :bordered="false"
                                  :type="r.require_mapping ? 'warning' : undefined"
                                >
                                  {{ r.require_mapping ? '强制映射' : '可选映射' }}
                                </n-tag>
                              </n-space>
                            </div>
                          </div>

                          <div class="route-actions">
                            <n-space vertical>
                              <n-button quaternary circle size="tiny" :disabled="rIdx === 0" @click="moveRoute(activeSource, rIdx, 'up')">
                                <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
                              </n-button>
                              <n-button quaternary circle size="tiny" :disabled="rIdx === activeSource.routes.length - 1" @click="moveRoute(activeSource, rIdx, 'down')">
                                <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
                              </n-button>
                              <n-button quaternary circle size="tiny" type="error" @click="activeSource.routes.splice(rIdx, 1)">
                                <template #icon><n-icon><CloseOutline /></n-icon></template>
                              </n-button>
                            </n-space>
                          </div>
                        </div>
                      </n-card>
                      
                      <div style="text-align: center; margin-top: 12px">
                        <n-button size="small" dashed @click="addRoute(activeSource)">
                          <template #icon><n-icon><AddOutline /></n-icon></template>
                          添加新规则
                        </n-button>
                      </div>
                    </div>
                  </div>
              </n-form>
            </n-card>
          </div>
        </n-card>
      </n-form>

      <sticky-action-bar
        v-if="draft"
        :dirty="hasChanges"
        :disabled="loading || saving"
        :primary-loading="saving"
        @primary="onSave"
        @secondary="resetDraft"
      />
  </page-shell>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
  padding-bottom: 80px;
}

.source-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  transition: all 0.2s ease;
}
.source-card:hover {
  border-color: var(--app-border-hover);
  box-shadow: var(--app-shadow-card);
}

.source-tabs-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.icon-box {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  background: var(--c-slate-100);
  color: var(--app-primary);
  display: grid;
  place-items: center;
}
.app-dark .icon-box { background: var(--c-slate-800); }

.header-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.source-tabs {
  margin-top: -4px;
}

.source-tabs :deep(.n-tabs-nav-scroll-content) {
  gap: 4px;
}

.source-tabs :deep(.n-tabs-tab) {
  max-width: 280px;
  border-radius: 8px !important;
  padding: 6px 14px !important;
  transition: all 0.2s ease;
  border: 1px solid transparent !important;
}

.source-tabs :deep(.n-tabs-tab--active) {
  background: var(--app-primary-alpha, rgba(99, 102, 241, 0.08)) !important;
  border-color: var(--app-primary-border, rgba(99, 102, 241, 0.2)) !important;
}

.source-tabs :deep(.n-tabs-tab:hover:not(.n-tabs-tab--active)) {
  background: var(--app-surface-1);
}

.source-tabs :deep(.n-tabs-tab__label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tab-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  flex-shrink: 0;
}

.tab-label-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 160px;
  display: inline-block;
  vertical-align: middle;
}

.health-status {
  display: flex;
  align-items: center;
  gap: 4px;
}

.name-input {
  width: clamp(120px, 30vw, 220px);
}

.id-text {
  font-family: monospace;
  font-size: 11px;
  opacity: 0.5;
}

.source-body {
  padding-top: 4px;
}

.source-compact-form :deep(.n-form-item) {
  margin-bottom: 10px;
}

.source-compact-form :deep(.n-form-item-label) {
  font-size: 12px;
}

.routes-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.route-item {
  background: var(--app-bg);
  border: 1px dashed var(--app-border);
  transition: all 0.2s ease;
}
.route-item:hover {
  border-color: var(--app-border-hover);
  border-style: solid;
  box-shadow: var(--app-shadow-0);
}

.route-layout {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.route-meta {
  width: 40px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.priority-tag {
  width: 100%;
  text-align: center;
  font-weight: 700;
}

.route-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.route-options {
  display: flex;
  justify-content: flex-end;
}

.route-actions {
  width: 32px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.empty-routes {
  padding: 20px;
  text-align: center;
  border: 1px dashed var(--app-border);
  border-radius: 8px;
}

@media (max-width: 768px) {
  .header-info {
    min-width: 0;
  }

  .name-input {
    width: min(52vw, 220px);
  }

  .card-actions {
    flex-wrap: nowrap;
    justify-content: flex-end;
  }

  .source-card :deep(.n-card-header__main) {
    min-width: 0;
  }

  .source-card :deep(.n-form-item) {
    margin-bottom: 12px;
  }

  .source-compact-form :deep(.n-form-item-label) {
    width: 110px !important;
  }

  .source-tabs :deep(.n-tabs-tab) {
    max-width: 220px;
  }
}
</style>
