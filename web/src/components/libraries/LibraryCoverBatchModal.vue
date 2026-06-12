<script setup lang="ts">
import { NButton, NCheckbox, NModal, NSpace, NSelect } from 'naive-ui'

defineProps<{
  show: boolean
  batchCoverStyle: string
  coverStyleOptions: { label: string; value: string }[]
  coverStylesLoaded: boolean
  batchIsShowcase: boolean
  showcaseIconOptions: { label: string; value: string }[]
  batchShowcaseIcon: string
  batchShowcaseShowPosterTitles: boolean
  batchShowcaseShowCount: boolean
  batchCoverResult: any
  batchCoverIssues: any[]
  generatingAllCovers: boolean
  canGenerateAllCovers: boolean
  solidModalMenuProps: Record<string, any>
  forceSolidModalStyle: Record<string, string>
}>()

const emit = defineEmits<{
  updateShow: [value: boolean]
  updateBatchCoverStyle: [value: string]
  updateBatchShowcaseIcon: [value: string]
  updateBatchShowcaseShowPosterTitles: [value: boolean]
  updateBatchShowcaseShowCount: [value: boolean]
  generate: []
}>()
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="生成所有媒体库封面"
    :style="[forceSolidModalStyle, { width: '520px', maxWidth: '92vw' }]"
    class="solid-modal-card force-solid-modal"
    @update:show="emit('updateShow', $event)"
  >
    <div class="form-group">
      <label class="form-label">封面风格</label>
      <n-select
        :value="batchCoverStyle"
        :options="coverStyleOptions"
        :loading="!coverStylesLoaded"
        :menu-props="solidModalMenuProps"
        placeholder="选择风格"
        @update:value="emit('updateBatchCoverStyle', $event)"
      />
    </div>
    <div v-if="batchIsShowcase" class="batch-cover-options">
      <div class="form-group">
        <label class="form-label">预制图标</label>
        <n-select
          :value="batchShowcaseIcon"
          :options="showcaseIconOptions"
          :menu-props="solidModalMenuProps"
          @update:value="emit('updateBatchShowcaseIcon', $event)"
        />
      </div>
      <div class="batch-cover-checks">
        <n-checkbox :checked="batchShowcaseShowPosterTitles" @update:checked="emit('updateBatchShowcaseShowPosterTitles', $event)">显示海报标题</n-checkbox>
        <n-checkbox :checked="batchShowcaseShowCount" @update:checked="emit('updateBatchShowcaseShowCount', $event)">显示媒体数量</n-checkbox>
      </div>
    </div>
    <div v-if="batchCoverResult" class="batch-cover-result">
      <div>共 {{ batchCoverResult.Total }} 个媒体库，成功 {{ batchCoverResult.Success }}，跳过 {{ batchCoverResult.Skipped }}，失败 {{ batchCoverResult.Failed }}</div>
      <div v-if="batchCoverIssues.length > 0" class="batch-cover-result-list">
        <div v-for="item in batchCoverIssues" :key="item.Id" class="batch-cover-result-item">
          {{ item.Name }}：{{ item.Message || item.Status }}
        </div>
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShow', false)">关闭</n-button>
        <n-button type="primary" :loading="generatingAllCovers" :disabled="!canGenerateAllCovers" @click="emit('generate')">
          生成全部
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.form-group { margin-bottom: 20px; }
.form-label { display: block; font-size: 12px; color: var(--app-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500; }
.batch-cover-options { padding-top: 4px; }
.batch-cover-checks {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.batch-cover-result {
  margin-top: 14px;
  padding: 10px 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: 6px;
  background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04));
  color: var(--app-text);
  font-size: 13px;
}
.batch-cover-result-list {
  margin-top: 8px;
  max-height: 160px;
  overflow: auto;
  color: var(--app-text-muted);
}
.batch-cover-result-item + .batch-cover-result-item {
  margin-top: 4px;
}
</style>
