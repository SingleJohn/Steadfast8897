<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NInput, NSelect, NSpace, NTag } from 'naive-ui'

defineProps<{
  importing: boolean
  lastImport: any
}>()

const importName = defineModel<string>('name', { required: true })
const importUrl = defineModel<string>('url', { required: true })
const importJson = defineModel<string>('json', { required: true })
const importKind = defineModel<'tvbox' | 'cms_list'>('kind', { required: true })
const importFormat = defineModel<'auto' | 'libretv_settings' | 'csv' | 'txt' | 'json'>('format', { required: true })

const emit = defineEmits<{
  submit: []
}>()

const kindOptions = [
  { label: 'TVBox', value: 'tvbox' },
  { label: 'CMS 源清单', value: 'cms_list' },
]

const formatOptions = [
  { label: '自动识别', value: 'auto' },
  { label: 'LibreTV settings', value: 'libretv_settings' },
  { label: 'CSV', value: 'csv' },
  { label: 'TXT', value: 'txt' },
  { label: 'JSON', value: 'json' },
]

const urlPlaceholder = computed(() => importKind.value === 'cms_list' ? 'https://example.com/cms-list.json' : 'https://example.com/tvbox.json')
const textPlaceholder = computed(() => importKind.value === 'cms_list' ? '粘贴 LibreTV settings、CSV、TXT 或 JSON 源清单，可留空' : '粘贴 TVBox JSON，可留空')
const availableLabel = computed(() => importKind.value === 'cms_list' ? '导入' : '可用')
const skippedLabel = computed(() => importKind.value === 'cms_list' ? '跳过' : '暂不可用')
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">来源配置导入</h2>
        <p class="panel-subtitle">支持 TVBox JSON 或 CMS 源清单，导入后在 Provider 列表中启停与探活。</p>
      </div>
      <NButton type="primary" :loading="importing" @click="emit('submit')">导入配置</NButton>
    </div>

    <div class="import-grid">
      <NSelect v-model:value="importKind" :options="kindOptions" />
      <NSelect v-if="importKind === 'cms_list'" v-model:value="importFormat" :options="formatOptions" />
      <NInput v-model:value="importName" placeholder="配置名称" clearable />
      <NInput v-model:value="importUrl" :placeholder="urlPlaceholder" clearable />
      <NInput
        v-model:value="importJson"
        type="textarea"
        :placeholder="textPlaceholder"
        :autosize="{ minRows: 4, maxRows: 10 }"
      />
    </div>

    <div v-if="lastImport" class="import-result">
      <NSpace align="center">
        <NTag type="success">{{ availableLabel }} {{ lastImport.accepted }}</NTag>
        <NTag>{{ skippedLabel }} {{ lastImport.skipped }}</NTag>
        <span class="muted">Provider {{ lastImport.providers?.length || 0 }} 个</span>
      </NSpace>
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
  margin-bottom: 14px;
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
.import-grid {
  display: grid;
  grid-template-columns: minmax(160px, 0.6fr) minmax(240px, 1.4fr);
  gap: 10px;
}
.import-grid :deep(.n-input--textarea) {
  grid-column: 1 / -1;
}
.import-result {
  margin-top: 12px;
}
.muted {
  color: var(--app-text-muted);
  font-size: 13px;
}
@media (max-width: 760px) {
  .panel-head,
  .import-grid {
    grid-template-columns: 1fr;
  }
  .panel-head {
    flex-direction: column;
  }
}
</style>
