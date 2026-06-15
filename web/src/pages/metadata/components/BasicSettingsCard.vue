<script setup lang="ts">
import { NButton, NCard, NForm, NFormItem, NGrid, NGridItem, NIcon, NInput, NInputNumber, NSelect, NSlider, NSwitch } from 'naive-ui'
import { SearchOutline } from '@vicons/ionicons5'

defineProps<{
  tmdbLanguage: string
  tmdbLanguageOptions: { label: string; value: string }[]
  scrapeSaveMode: string
  scrapeSaveModeOptions: { label: string; value: string }[]
  tmdbProxy: string
  confidenceThreshold: number
  autoScrape: boolean
  autoApplyEnabled: boolean
  adultContentFilterEnabled: boolean
  imageDirectRead: boolean
  scrapeSummary: { missing_count: number; items_total: number } | null
  savingConfig: boolean
  scraping: boolean
}>()

const emit = defineEmits<{
  'update:tmdbLanguage': [value: string]
  'update:scrapeSaveMode': [value: string]
  'update:tmdbProxy': [value: string]
  'update:confidenceThreshold': [value: number]
  'update:autoScrape': [value: boolean]
  'update:autoApplyEnabled': [value: boolean]
  'update:adultContentFilterEnabled': [value: boolean]
  'update:imageDirectRead': [value: boolean]
  save: []
  scrape: []
}>()

function normalizedString(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function updateTmdbLanguage(value: unknown) {
  emit('update:tmdbLanguage', normalizedString(value))
}

function updateScrapeSaveMode(value: unknown) {
  emit('update:scrapeSaveMode', normalizedString(value))
}

function updateTmdbProxy(value: unknown) {
  emit('update:tmdbProxy', normalizedString(value))
}

function updateThreshold(value: unknown) {
  if (typeof value === 'number') emit('update:confidenceThreshold', value)
}
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card metadata-card">
    <template #header>
      <div class="card-header-wrap">
        <div class="icon-box tmdb">
          <n-icon :size="18"><SearchOutline /></n-icon>
        </div>
        <div class="header-copy">
          <div class="header-title">基础设置</div>
          <div class="header-desc">语言、保存位置、代理、自动化行为。</div>
        </div>
      </div>
    </template>
    <template #header-extra>
      <div v-if="scrapeSummary" class="inline-stat">
        <span class="inline-stat-value">{{ scrapeSummary.missing_count }}</span>
        <span class="inline-stat-name">待刮削<template v-if="scrapeSummary.items_total"> / {{ scrapeSummary.items_total }}</template></span>
      </div>
    </template>

    <n-form label-placement="top" size="small" class="config-form">
      <n-grid cols="1 m:3" x-gap="12" responsive="screen">
        <n-grid-item>
          <n-form-item label="元数据语言">
            <n-select :value="tmdbLanguage" :options="tmdbLanguageOptions" size="small" @update:value="updateTmdbLanguage" />
          </n-form-item>
        </n-grid-item>
        <n-grid-item>
          <n-form-item label="保存位置">
            <n-select :value="scrapeSaveMode" :options="scrapeSaveModeOptions" size="small" @update:value="updateScrapeSaveMode" />
          </n-form-item>
        </n-grid-item>
        <n-grid-item>
          <n-form-item label="代理">
            <n-input :value="tmdbProxy" placeholder="http:// 或 socks5://" size="small" @update:value="updateTmdbProxy" />
          </n-form-item>
        </n-grid-item>
      </n-grid>

      <n-form-item label="识别阈值">
        <div class="threshold-row">
          <n-slider :value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" :tooltip="true" style="flex: 1" @update:value="updateThreshold" />
          <n-input-number :value="confidenceThreshold" :min="0.5" :max="1" :step="0.01" size="small" style="width: 110px" @update:value="updateThreshold" />
        </div>
        <div class="hint-text">候选 ≥ 阈值直接采纳。推荐 0.72。</div>
      </n-form-item>

      <div class="switch-row-grid">
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">自动刮削</div>
            <div class="hint-text">新媒体入库时自动抓取元数据。</div>
          </div>
          <n-switch :value="autoScrape" :round="false" @update:value="emit('update:autoScrape', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">自动采纳</div>
            <div class="hint-text">低于阈值的候选自动采纳,否则进入人工确认队列。</div>
          </div>
          <n-switch :value="autoApplyEnabled" :round="false" @update:value="emit('update:autoApplyEnabled', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">成人内容过滤</div>
            <div class="hint-text">拦截成人影视内容候选；命中时按识别失败处理，不覆盖现有元数据。</div>
          </div>
          <n-switch :value="adultContentFilterEnabled" :round="false" @update:value="emit('update:adultContentFilterEnabled', $event)" />
        </div>
        <div class="switch-section-compact">
          <div class="switch-copy">
            <div class="switch-title">本地原图直读</div>
            <div class="hint-text">开启后媒体目录/挂载盘原图直接读取；关闭则缓存一份原图到 sources，适合网盘挂载。普通缩放图实时处理，不按客户端尺寸落盘。</div>
          </div>
          <n-switch :value="imageDirectRead" :round="false" @update:value="emit('update:imageDirectRead', $event)" />
        </div>
      </div>
    </n-form>

    <div class="card-actions">
      <n-button type="primary" size="small" :loading="savingConfig" @click="emit('save')">保存基础设置</n-button>
      <n-button secondary size="small" :loading="scraping" :disabled="scraping || scrapeSummary?.missing_count === 0" @click="emit('scrape')">刮削缺失元数据</n-button>
    </div>
  </n-card>
</template>
