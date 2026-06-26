<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NInput, NPopover, NSelect, NSpace, NTag, NTooltip } from 'naive-ui'

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

const kindHint = '配置类型决定如何解析：TVBox 为完整的 TVBox JSON（含 sites）；CMS 源清单为一批苹果 CMS 接口的列表（多种格式）。'
const formatHints: Record<string, string> = {
  auto: '自动按 LibreTV → JSON → CSV → TXT 依次尝试解析，识别不准时再手动指定。',
  libretv_settings: 'LibreTV 导出的设置 JSON，源放在 data.customAPIs（字符串化的 JSON 数组）里。',
  csv: '逗号分隔，首行可为表头 name,api；列顺序为 名称, 接口, detail, is_adult。',
  txt: '每行“名称,接口地址”（逗号或空格分隔）；# 或 // 开头为注释，可用 type:adult 标记成人源。',
  json: 'JSON 数组 [{name, api}]，或对象 {sources:{key:{name,url}}}。',
}
const formatHint = computed(() => formatHints[importFormat.value] || '')

const TVBOX_EXAMPLE = `{
  "sites": [
    {
      "key": "demo_cms",
      "name": "示例资源",
      "type": 1,
      "api": "https://example.com/api.php/provide/vod/",
      "searchable": 1,
      "quickSearch": 1,
      "filterable": 1
    }
  ]
}`
const CMS_EXAMPLES: Record<string, string> = {
  json: `[
  { "name": "示例资源A", "api": "https://a.example.com/api.php/provide/vod/" },
  { "name": "示例资源B", "api": "https://b.example.com/api.php/provide/vod/" }
]`,
  csv: `name,api
示例资源A,https://a.example.com/api.php/provide/vod/
示例资源B,https://b.example.com/api.php/provide/vod/`,
  txt: `# 每行：名称,接口地址
示例资源A,https://a.example.com/api.php/provide/vod/
示例资源B,https://b.example.com/api.php/provide/vod/`,
  libretv_settings: `{
  "name": "LibreTV 配置",
  "cfgVer": "1.0",
  "data": {
    "customAPIs": "[{\\"name\\":\\"示例资源A\\",\\"url\\":\\"https://a.example.com/api.php/provide/vod/\\"}]",
    "selectedAPIs": ["示例资源A"]
  }
}`,
}
const exampleText = computed(() => {
  if (importKind.value !== 'cms_list') return TVBOX_EXAMPLE
  if (importFormat.value === 'auto') return CMS_EXAMPLES.json
  return CMS_EXAMPLES[importFormat.value] || CMS_EXAMPLES.json
})

function fillExample() {
  importJson.value = exampleText.value
  if (!importName.value.trim()) importName.value = importKind.value === 'cms_list' ? '示例 CMS 源清单' : '示例 TVBox 配置'
}
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
      <label class="field">
        <span class="field-label">
          配置类型
          <NTooltip>
            <template #trigger><span class="lbl-info" aria-label="配置类型说明">?</span></template>
            {{ kindHint }}
          </NTooltip>
        </span>
        <NSelect v-model:value="importKind" :options="kindOptions" />
      </label>
      <label v-if="importKind === 'cms_list'" class="field">
        <span class="field-label">清单格式</span>
        <NSelect v-model:value="importFormat" :options="formatOptions" />
        <span v-if="formatHint" class="field-hint">{{ formatHint }}</span>
      </label>
      <label class="field">
        <span class="field-label">配置名称</span>
        <NInput v-model:value="importName" placeholder="配置名称" clearable />
      </label>
      <label class="field">
        <span class="field-label">配置 URL</span>
        <NInput v-model:value="importUrl" :placeholder="urlPlaceholder" clearable />
      </label>
      <label class="field full-field">
        <span class="field-label-row">
          <span class="field-label">配置内容</span>
          <span class="field-tools">
            <NPopover trigger="hover" placement="bottom-end">
              <template #trigger>
                <NButton size="tiny" quaternary>查看示例</NButton>
              </template>
              <pre class="example-pre">{{ exampleText }}</pre>
            </NPopover>
            <NButton size="tiny" @click="fillExample">填入示例</NButton>
          </span>
        </span>
        <NInput
          v-model:value="importJson"
          type="textarea"
          :placeholder="textPlaceholder"
          :autosize="{ minRows: 4, maxRows: 10 }"
        />
      </label>
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
.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.field-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
}
.field-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.field-tools {
  display: inline-flex;
  gap: 6px;
}
.field-hint {
  color: var(--app-text-muted);
  font-size: 12px;
  line-height: 1.4;
}
.lbl-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  border-radius: 8px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 11px;
  font-weight: 600;
  cursor: help;
}
.example-pre {
  max-width: 460px;
  max-height: 320px;
  margin: 0;
  overflow: auto;
  font-family: var(--app-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre;
}
.full-field {
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
