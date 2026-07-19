<script setup lang="ts">
import { NButton, NInput, NInputNumber } from 'naive-ui'

defineProps<{
  latestLibrary: any
  latestName: string
  latestLimit: number
  globalEnabled: boolean
  saving: boolean
}>()

const emit = defineEmits<{
  updateLatestName: [value: string]
  updateLatestLimit: [value: number]
  save: []
}>()
</script>

<template>
  <section class="latest-panel">
    <div class="latest-copy">
      <h3 class="latest-title">最新媒体库</h3>
      <p class="latest-desc">动态聚合最近入库的电影和有新单集的剧集；剧集始终以完整剧集卡片显示。</p>
    </div>
    <div class="latest-form">
      <label class="latest-field">
        <span class="latest-label">显示名称</span>
        <n-input
          :value="latestName"
          size="small"
          placeholder="最新更新"
          @update:value="emit('updateLatestName', $event)"
        />
      </label>
      <label class="latest-field latest-limit-field">
        <span class="latest-label">聚合数量</span>
        <n-input-number
          :value="latestLimit"
          :min="1"
          :max="2000"
          :step="50"
          size="small"
          @update:value="emit('updateLatestLimit', $event || 1)"
        />
      </label>
      <n-button type="primary" size="small" :loading="saving" @click="emit('save')">
        {{ latestLibrary ? '保存设置' : '创建虚拟库' }}
      </n-button>
    </div>
    <div v-if="latestLibrary" class="latest-status">
      当前聚合 {{ latestLibrary.ItemCount }} / {{ latestLibrary.ItemLimit }} 项，启停、排序和封面可在下方列表管理。
      <span v-if="!globalEnabled"> 当前全局虚拟库开关未开启。</span>
    </div>
  </section>
</template>

<style scoped>
.latest-panel {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(360px, 1.25fr);
  gap: 18px 28px;
  align-items: end;
  padding: 18px 24px;
  margin-bottom: 16px;
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255, 255, 255, 0.06));
  border-radius: var(--app-radius, 8px);
}

.latest-title {
  margin: 0 0 6px;
  color: var(--app-text);
  font-size: 15px;
  font-weight: 600;
}

.latest-desc,
.latest-status {
  margin: 0;
  color: var(--app-text-muted);
  font-size: 12px;
  line-height: 1.6;
}

.latest-form {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) 130px auto;
  gap: 10px;
  align-items: end;
}

.latest-field {
  display: grid;
  gap: 6px;
}

.latest-label {
  color: var(--app-text-muted);
  font-size: 11px;
}

.latest-status {
  grid-column: 1 / -1;
  padding-top: 10px;
  border-top: 1px solid var(--app-border, rgba(255, 255, 255, 0.04));
}

@media (max-width: 760px) {
  .latest-panel,
  .latest-form {
    grid-template-columns: 1fr;
  }

  .latest-limit-field {
    max-width: 180px;
  }
}
</style>
