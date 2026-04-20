<script setup lang="ts">
import MediaCard from './MediaCard.vue'

withDefaults(defineProps<{
  items: any[]
  shape?: 'portrait' | 'thumb' | 'square'
  showProgress?: boolean
  density?: 'default' | 'compact'
}>(), {
  shape: 'portrait',
  showProgress: false,
  density: 'default',
})
</script>

<template>
  <div v-if="items.length" class="item-grid" :class="[`item-grid-${shape}`, `item-grid-${density}`]">
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

/* default density */
.item-grid-portrait.item-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

@media (min-width: 600px) {
  .item-grid-portrait.item-grid-default { grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); }
}

@media (min-width: 960px) {
  .item-grid-portrait.item-grid-default { grid-template-columns: repeat(auto-fill, minmax(165px, 1fr)); }
}

.item-grid-thumb.item-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
}

.item-grid-square.item-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
}

/* compact density: 更小的最小宽度,一行显示更多 */
.item-grid-portrait.item-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 18px 12px;
}

@media (min-width: 600px) {
  .item-grid-portrait.item-grid-compact { grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); }
}

@media (min-width: 960px) {
  .item-grid-portrait.item-grid-compact { grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); }
}

.item-grid-thumb.item-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px 12px;
}

@media (min-width: 960px) {
  .item-grid-thumb.item-grid-compact { grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); }
}

.item-grid-square.item-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 18px 12px;
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
