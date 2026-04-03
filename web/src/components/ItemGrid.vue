<script setup lang="ts">
import MediaCard from './MediaCard.vue'

withDefaults(defineProps<{
  items: any[]
  shape?: 'portrait' | 'thumb' | 'square'
  showProgress?: boolean
}>(), {
  shape: 'portrait',
  showProgress: false,
})
</script>

<template>
  <div v-if="items.length" class="item-grid" :class="`item-grid-${shape}`">
    <MediaCard v-for="item in items" :key="item.Id" :item="item" :shape="shape" :show-progress="showProgress" />
  </div>
  <div v-else class="item-grid-empty">
    <slot>
      <p>暂无内容</p>
    </slot>
  </div>
</template>

<style scoped>
.item-grid {
  display: grid;
  gap: 22px 16px;
}

.item-grid-portrait {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

@media (min-width: 600px) {
  .item-grid-portrait { grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); }
}

@media (min-width: 960px) {
  .item-grid-portrait { grid-template-columns: repeat(auto-fill, minmax(165px, 1fr)); }
}

.item-grid-thumb {
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
}

.item-grid-square {
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
}

.item-grid-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 240px;
  color: rgba(148, 163, 184, 0.88);
  font-size: 15px;
}
</style>
