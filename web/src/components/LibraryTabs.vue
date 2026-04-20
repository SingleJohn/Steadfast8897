<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ItemGrid from './ItemGrid.vue'

const props = withDefaults(
  defineProps<{
    items: any[]
    title?: string
    shape?: 'portrait' | 'thumb' | 'square'
  }>(),
  {
    title: '媒体库',
    shape: 'thumb',
  },
)

const regular = computed(() => props.items.filter((i: any) => !i.PlatformLibrary))
const platform = computed(() => props.items.filter((i: any) => i.PlatformLibrary))

const hasTabs = computed(() => regular.value.length > 0 && platform.value.length > 0)

const activeTab = ref<'regular' | 'platform'>('regular')

watch(
  [regular, platform],
  () => {
    if (activeTab.value === 'regular' && regular.value.length === 0 && platform.value.length > 0) {
      activeTab.value = 'platform'
    } else if (activeTab.value === 'platform' && platform.value.length === 0 && regular.value.length > 0) {
      activeTab.value = 'regular'
    }
  },
  { immediate: true },
)

const displayItems = computed(() => {
  if (!hasTabs.value) {
    return regular.value.length > 0 ? regular.value : platform.value
  }
  return activeTab.value === 'regular' ? regular.value : platform.value
})
</script>

<template>
  <section v-if="items.length" class="library-tabs-section">
    <div class="lt-header">
      <h2 class="lt-title"><span>{{ title }}</span></h2>
      <div v-if="hasTabs" class="lt-tabs" role="tablist">
        <button
          class="lt-tab"
          :class="{ active: activeTab === 'regular' }"
          role="tab"
          :aria-selected="activeTab === 'regular'"
          @click="activeTab = 'regular'"
        >媒体库 <span class="lt-count">{{ regular.length }}</span></button>
        <button
          class="lt-tab"
          :class="{ active: activeTab === 'platform' }"
          role="tab"
          :aria-selected="activeTab === 'platform'"
          @click="activeTab = 'platform'"
        >平台库 <span class="lt-count">{{ platform.length }}</span></button>
      </div>
    </div>
    <ItemGrid :items="displayItems" :shape="shape" density="compact" />
  </section>
</template>

<style scoped>
.library-tabs-section {
  width: 100%;
  min-width: 0;
}

.lt-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 8px;
  padding: 0 8px;
}

.lt-title {
  display: flex;
  align-items: center;
  gap: 14px;
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 1.25rem;
  font-weight: 800;
  line-height: 1.2;
  letter-spacing: -0.01em;
  color: var(--app-text);
  margin: 0;
}

.lt-title::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 1.1em;
  border-radius: 2px;
  background: var(--app-primary, #e50914);
  flex-shrink: 0;
}

.lt-tabs {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
}

.lt-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: rgba(255, 255, 255, 0.7);
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.005em;
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease;
}

.lt-tab:hover {
  color: rgba(255, 255, 255, 0.95);
}

.lt-tab.active {
  background: var(--app-primary, #e50914);
  color: #fff;
}

.lt-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  padding: 0 6px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.3);
  color: inherit;
  font-size: 11px;
  font-weight: 700;
}

.lt-tab:not(.active) .lt-count {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.6);
}
</style>
