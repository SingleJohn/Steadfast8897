<script setup lang="ts">
import {
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NSelect,
  NSpace,
  NTabPane,
  NTabs,
  NText,
} from 'naive-ui'
import { ArrowDownOutline, CloseOutline, CubeOutline, AddOutline } from '@vicons/ionicons5'
import { computed, ref, watch } from 'vue'

import type { Config } from '@/types'

const props = defineProps<{
  draft: Config
  backendOptions: Array<{ label: string; value: string }>
}>()

const emit = defineEmits<{
  (e: 'add-pool'): void
}>()

const activePoolId = ref('')

const activePool = computed(() => {
  if (props.draft.resource_pools.length === 0) return null
  const idx = props.draft.resource_pools.findIndex((item, index) => poolTabName(item.id, index) === activePoolId.value)
  if (idx >= 0) return props.draft.resource_pools[idx]
  return props.draft.resource_pools[0]
})

const activePoolIndex = computed(() => {
  if (!activePool.value) return -1
  return props.draft.resource_pools.findIndex((item) => item === activePool.value)
})

watch(
  () => props.draft.resource_pools.map((item, index) => poolTabName(item.id, index)),
  (names, oldNames) => {
    if (names.length === 0) {
      activePoolId.value = ''
      return
    }
    if (!names.includes(activePoolId.value)) {
      const prevIndex = oldNames?.indexOf(activePoolId.value) ?? -1
      if (prevIndex >= 0) {
        activePoolId.value = names[Math.min(prevIndex, names.length - 1)]
        return
      }
      activePoolId.value = names[0]
    }
  },
  { immediate: true },
)

function poolTabName(id: string, idx: number) {
  return id?.trim() || `pool-${idx}`
}

function poolTabLabel(name: string) {
  const safeName = name?.trim() || '未命名 Pool'
  return safeName
}

function removeActivePool() {
  if (activePoolIndex.value < 0) return
  props.draft.resource_pools.splice(activePoolIndex.value, 1)
}
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card" title="资源池 (Resource Pools)">
    <template #header-extra>
      <n-button size="small" secondary @click="emit('add-pool')">
        <template #icon><n-icon><AddOutline /></n-icon></template>
        新增 Pool
      </n-button>
    </template>

    <n-space v-if="draft.resource_pools.length === 0" vertical :size="8">
      <n-empty description="暂无 Resource Pool 配置" />
    </n-space>

    <div v-else class="pool-tabs-layout">
      <n-tabs v-model:value="activePoolId" type="card" animated class="pool-tabs" placement="top">
        <n-tab-pane
          v-for="(p, idx) in draft.resource_pools"
          :key="p.id || idx"
          :name="poolTabName(p.id, idx)"
        >
          <template #tab>
            <span class="tab-dot" />
            <span class="tab-label-text">{{ p.name?.trim() || '未命名 Pool' }}</span>
          </template>
        </n-tab-pane>
      </n-tabs>

      <n-card
        v-if="activePool"
        size="small"
        :bordered="false"
        class="glass-card pool-card"
      >
        <template #header>
          <n-space align="center" :size="12">
            <div class="icon-box pool">
              <n-icon :size="20"><CubeOutline /></n-icon>
            </div>
            <div class="header-info">
              <n-input v-model:value="activePool.name" placeholder="Pool 名称" size="small" class="name-input" />
              <n-text depth="3" class="id-text">{{ activePool.id }}</n-text>
            </div>
          </n-space>
        </template>
        <template #header-extra>
          <n-button quaternary circle size="small" type="error" @click="removeActivePool">
            <template #icon><n-icon><CloseOutline /></n-icon></template>
          </n-button>
        </template>

        <n-form label-placement="left" label-width="120" size="small" class="pool-body pool-compact-form">
          <n-form-item label="主后端 (Primary)">
            <n-select
              v-model:value="activePool.primary_backend_id"
              :options="backendOptions"
              placeholder="首选后端"
              filterable
              clearable
            />
          </n-form-item>

          <div class="fallback-arrow">
            <n-icon size="16" depth="3"><ArrowDownOutline /></n-icon>
            <n-text depth="3" size="small">失败自动切换</n-text>
          </div>

          <n-form-item label="备后端 (Standby)">
            <n-select
              v-model:value="activePool.standby_backend_id"
              :options="backendOptions"
              placeholder="备用后端"
              filterable
              clearable
            />
          </n-form-item>
        </n-form>
      </n-card>
    </div>
  </n-card>
</template>

<style scoped>
.pool-tabs-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pool-tabs {
  margin-top: -4px;
}

.pool-tabs :deep(.n-tabs-nav-scroll-content) {
  gap: 4px;
}

.pool-tabs :deep(.n-tabs-tab) {
  max-width: 320px;
  border-radius: 8px !important;
  padding: 6px 14px !important;
  transition: all 0.2s ease;
  border: 1px solid transparent !important;
}

.pool-tabs :deep(.n-tabs-tab--active) {
  background: var(--app-primary-alpha, rgba(99, 102, 241, 0.08)) !important;
  border-color: var(--app-primary-border, rgba(99, 102, 241, 0.2)) !important;
}

.pool-tabs :deep(.n-tabs-tab:hover:not(.n-tabs-tab--active)) {
  background: var(--app-surface-1);
}

.pool-tabs :deep(.n-tabs-tab__label) {
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
  background: #10b981;
}

.tab-label-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 180px;
  display: inline-block;
  vertical-align: middle;
}

.icon-box {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  font-size: 18px;
  background: var(--c-slate-100);
  color: var(--c-slate-500);
}
.app-dark .icon-box {
  background: var(--c-slate-800);
}

.icon-box.pool {
  color: #10b981;
  background: rgba(16, 185, 129, 0.1);
}

.header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.id-text {
  font-family: monospace;
  font-size: 11px;
  opacity: 0.5;
}

.pool-body {
  padding-top: 4px;
}

.pool-compact-form :deep(.n-form-item) {
  margin-bottom: 10px;
}

.pool-compact-form :deep(.n-form-item-label) {
  font-size: 12px;
}

.fallback-arrow {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  margin: 4px 0;
  opacity: 0.6;
}

.name-input {
  width: clamp(120px, 26vw, 220px);
}

@media (max-width: 768px) {
  .header-info {
    min-width: 0;
  }

  .name-input {
    width: min(52vw, 220px);
  }

  .pool-card :deep(.n-form-item) {
    margin-bottom: 12px;
  }

  .pool-compact-form :deep(.n-form-item-label) {
    width: 96px !important;
  }

  .pool-tabs :deep(.n-tabs-tab) {
    max-width: 240px;
  }
}
</style>
