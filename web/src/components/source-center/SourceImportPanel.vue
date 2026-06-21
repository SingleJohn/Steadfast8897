<script setup lang="ts">
import { NButton, NInput, NSpace, NTag } from 'naive-ui'

defineProps<{
  importing: boolean
  lastImport: any
}>()

const importName = defineModel<string>('name', { required: true })
const importUrl = defineModel<string>('url', { required: true })
const importJson = defineModel<string>('json', { required: true })

const emit = defineEmits<{
  submit: []
}>()
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">TVBox 配置导入</h2>
        <p class="panel-subtitle">支持远程 URL 或粘贴 JSON，导入后会解析可用的 JSON CMS Provider。</p>
      </div>
      <NButton type="primary" :loading="importing" @click="emit('submit')">导入配置</NButton>
    </div>

    <div class="import-grid">
      <NInput v-model:value="importName" placeholder="配置名称" clearable />
      <NInput v-model:value="importUrl" placeholder="https://example.com/tvbox.json" clearable />
      <NInput
        v-model:value="importJson"
        type="textarea"
        placeholder="粘贴 TVBox JSON，可留空"
        :autosize="{ minRows: 4, maxRows: 10 }"
      />
    </div>

    <div v-if="lastImport" class="import-result">
      <NSpace align="center">
        <NTag type="success">可用 {{ lastImport.accepted }}</NTag>
        <NTag>暂不可用 {{ lastImport.skipped }}</NTag>
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
.import-grid :deep(.n-input:nth-child(3)) {
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
