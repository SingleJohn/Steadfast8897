<script setup lang="ts">
import { NButton, NCard, NFormItem, NIcon, NInput } from 'naive-ui'
import {
  AddOutline,
  ArrowDownOutline,
  ArrowForwardOutline,
  ArrowUpOutline,
  CheckmarkCircle,
  CloseOutline,
  EllipseOutline,
  LayersOutline,
  ReorderFourOutline,
} from '@vicons/ionicons5'

const props = defineProps<{
  providerOrder: string[]
  providersEnabled: string[]
  selectedProvider: string
  providerMeta: Record<string, any>
  draggingIndex: number | null
  dragOverIndex: number | null
  tmdbApiKeys: string[]
  showApiKey: boolean
  tvdbApiKey: string
  tvdbPin: string
  bangumiUa: string
  doubanCookie: string
  fanartApiKey: string
  savingScrapeSources: boolean
}>()

const emit = defineEmits<{
  'update:selectedProvider': [value: string]
  'update:dragOverIndex': [value: number | null]
  'update:tmdbApiKeys': [value: string[]]
  'update:showApiKey': [value: boolean]
  'update:tvdbApiKey': [value: string]
  'update:tvdbPin': [value: string]
  'update:bangumiUa': [value: string]
  'update:doubanCookie': [value: string]
  'update:fanartApiKey': [value: string]
  toggleProvider: [name: string]
  dragStart: [index: number, event: DragEvent]
  dragOver: [index: number, event: DragEvent]
  drop: [index: number, event: DragEvent]
  dragEnd: []
  moveProvider: [index: number, delta: number]
  unmatched: []
  save: []
}>()

function isProviderEnabled(name: string) {
  return props.providersEnabled.includes(name)
}

function normalizedString(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function updateTmdbKey(index: number, value: unknown) {
  const next = [...props.tmdbApiKeys]
  next[index] = normalizedString(value)
  emit('update:tmdbApiKeys', next)
}

function removeTmdbKey(index: number) {
  emit('update:tmdbApiKeys', props.tmdbApiKeys.filter((_, i) => i !== index))
}

function addTmdbKey() {
  emit('update:tmdbApiKeys', [...props.tmdbApiKeys, ''])
}

function clearDragOver(index: number) {
  if (props.dragOverIndex === index) emit('update:dragOverIndex', null)
}

function updateTvdbApiKey(value: unknown) {
  emit('update:tvdbApiKey', normalizedString(value))
}

function updateTvdbPin(value: unknown) {
  emit('update:tvdbPin', normalizedString(value))
}

function updateBangumiUa(value: unknown) {
  emit('update:bangumiUa', normalizedString(value))
}

function updateDoubanCookie(value: unknown) {
  emit('update:doubanCookie', normalizedString(value))
}

function updateFanartApiKey(value: unknown) {
  emit('update:fanartApiKey', normalizedString(value))
}
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card metadata-card">
    <template #header>
      <div class="card-header-wrap">
        <div class="icon-box tmdb">
          <n-icon :size="18"><LayersOutline /></n-icon>
        </div>
        <div class="header-copy">
          <div class="header-title">刮削源</div>
          <div class="header-desc">识别按列表顺序逐源尝试,首个命中即停。拖拽调整顺序、勾选启用/停用。</div>
        </div>
      </div>
    </template>
    <template #header-extra>
      <n-button quaternary size="small" @click="emit('unmatched')">
        <template #icon><n-icon><ArrowForwardOutline /></n-icon></template>
        未匹配面板
      </n-button>
    </template>

    <div class="provider-split">
      <div class="provider-sidebar">
        <div
          v-for="(name, idx) in providerOrder"
          :key="name"
          class="provider-row"
          :class="{
            dragging: draggingIndex === idx,
            'drag-over': dragOverIndex === idx && draggingIndex !== idx,
            selected: selectedProvider === name,
            disabled: !isProviderEnabled(name),
          }"
          :style="{ '--accent': providerMeta[name]?.accent }"
          draggable="true"
          @click="emit('update:selectedProvider', name)"
          @dragstart="emit('dragStart', idx, $event)"
          @dragover="emit('dragOver', idx, $event)"
          @drop="emit('drop', idx, $event)"
          @dragend="emit('dragEnd')"
          @dragleave="clearDragOver(idx)"
        >
          <div class="provider-handle" title="拖拽调序">
            <n-icon><ReorderFourOutline /></n-icon>
          </div>

          <label class="provider-check" @click.stop>
            <input type="checkbox" :checked="isProviderEnabled(name)" @change="emit('toggleProvider', name)" />
          </label>

          <div class="provider-info">
            <div class="provider-name">
              {{ providerMeta[name]?.label }}
              <span v-if="providerMeta[name]?.badge" class="provider-badge">{{ providerMeta[name]?.badge }}</span>
            </div>
            <div class="provider-desc">{{ providerMeta[name]?.desc }}</div>
          </div>

          <span class="provider-index">{{ idx + 1 }}</span>

          <div class="provider-move">
            <n-button quaternary circle size="tiny" :disabled="idx === 0" @click.stop="emit('moveProvider', idx, -1)">
              <template #icon><n-icon><ArrowUpOutline /></n-icon></template>
            </n-button>
            <n-button quaternary circle size="tiny" :disabled="idx === providerOrder.length - 1" @click.stop="emit('moveProvider', idx, 1)">
              <template #icon><n-icon><ArrowDownOutline /></n-icon></template>
            </n-button>
          </div>
        </div>
      </div>

      <div class="provider-detail">
        <div class="detail-header">
          <n-icon :size="16" :color="providerMeta[selectedProvider]?.accent">
            <component :is="isProviderEnabled(selectedProvider) ? CheckmarkCircle : EllipseOutline" />
          </n-icon>
          <div class="detail-title">{{ providerMeta[selectedProvider]?.label }}</div>
          <div class="detail-badge" v-if="providerMeta[selectedProvider]?.badge">{{ providerMeta[selectedProvider]?.badge }}</div>
        </div>

        <div v-if="selectedProvider === 'tmdb'" class="detail-body">
          <div class="subsection-title-inline">API Key</div>
          <div v-for="(key, idx) in tmdbApiKeys" :key="idx" class="api-key-row">
            <span class="row-index">{{ idx + 1 }}</span>
            <n-input :value="key" :type="showApiKey ? 'text' : 'password'" :placeholder="`TMDB API Key ${idx + 1}`" size="small" @update:value="updateTmdbKey(idx, $event)" />
            <n-button v-if="tmdbApiKeys.length > 1" quaternary circle type="error" size="small" @click="removeTmdbKey(idx)">
              <template #icon><n-icon><CloseOutline /></n-icon></template>
            </n-button>
          </div>
          <div class="inline-actions">
            <n-button secondary size="tiny" @click="addTmdbKey">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              添加 Key
            </n-button>
            <n-button quaternary size="tiny" @click="emit('update:showApiKey', !showApiKey)">{{ showApiKey ? '隐藏' : '显示' }}</n-button>
          </div>
          <div class="hint-text">支持多个 Key 轮询,避免单 Key 风控。未配置时 TMDB 源自动跳过。</div>
        </div>

        <div v-else-if="selectedProvider === 'tvdb'" class="detail-body">
          <n-form-item label="API Key">
            <n-input :value="tvdbApiKey" type="password" placeholder="订阅 TVDB 后填入,留空则禁用" size="small" show-password-on="click" @update:value="updateTvdbApiKey" />
          </n-form-item>
          <n-form-item label="Pin (可选)">
            <n-input :value="tvdbPin" placeholder="TVDB 用户 Pin" size="small" @update:value="updateTvdbPin" />
          </n-form-item>
          <div class="hint-text">未配置 API Key 时 TVDB 源自动跳过。</div>
        </div>

        <div v-else-if="selectedProvider === 'bangumi'" class="detail-body">
          <n-form-item label="User-Agent">
            <n-input :value="bangumiUa" placeholder="留空使用默认 fyms/1.0" size="small" @update:value="updateBangumiUa" />
          </n-form-item>
          <div class="hint-text">Bangumi 要求请求带 UA 注明来源;填 GitHub/邮箱标识更友好。</div>
        </div>

        <div v-else-if="selectedProvider === 'douban'" class="detail-body">
          <n-form-item label="Cookie (可选)">
            <n-input :value="doubanCookie" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" placeholder="粘贴已登录账号的 Cookie,提高搜索配额" size="small" @update:value="updateDoubanCookie" />
          </n-form-item>
          <div class="hint-text">非官方 API,仅作中文补全。触发风控会自动熔断 10 分钟。停用豆瓣请在左侧取消勾选。</div>
        </div>

        <div v-else-if="selectedProvider === 'fanart'" class="detail-body">
          <n-form-item label="API Key">
            <n-input :value="fanartApiKey" type="password" placeholder="留空则禁用图片补充" size="small" show-password-on="click" @update:value="updateFanartApiKey" />
          </n-form-item>
          <div class="hint-text">只参与图片补充(poster / backdrop / seasonposter),不参与识别。</div>
        </div>
      </div>
    </div>

    <div class="card-actions">
      <n-button type="primary" size="small" :loading="savingScrapeSources" @click="emit('save')">保存刮削源</n-button>
    </div>
  </n-card>
</template>
