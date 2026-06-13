<script setup lang="ts">
import Skeleton from './Skeleton.vue';

withDefaults(
  defineProps<{
    count?: number;
    shape?: 'portrait' | 'thumb' | 'square';
    density?: 'default' | 'compact';
  }>(),
  {
    count: 6,
    shape: 'portrait',
    density: 'default',
  }
);
</script>

<template>
  <div
    class="card-skeleton-grid"
    :class="[`card-skeleton-grid-${shape}`, `card-skeleton-grid-${density}`]"
  >
    <div v-for="i in count" :key="i">
      <Skeleton
        class="card-skeleton-poster"
        height="auto"
        border-radius="var(--app-radius, 10px)"
        :style="{ marginBottom: '8px' }"
      />
      <Skeleton :height="14" width="80%" :style="{ marginBottom: '4px' }" />
      <Skeleton :height="12" width="50%" />
    </div>
  </div>
</template>

<style scoped>
.card-skeleton-grid {
  display: grid;
  gap: 20px;
}

.card-skeleton-grid-portrait.card-skeleton-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

.card-skeleton-grid-portrait.card-skeleton-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 18px 12px;
}

.card-skeleton-grid-thumb.card-skeleton-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
}

.card-skeleton-grid-thumb.card-skeleton-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px 12px;
}

.card-skeleton-grid-square.card-skeleton-grid-default {
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
}

.card-skeleton-grid-square.card-skeleton-grid-compact {
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 18px 12px;
}

.card-skeleton-poster {
  aspect-ratio: 2 / 3;
}

.card-skeleton-grid-thumb .card-skeleton-poster {
  aspect-ratio: 16 / 9;
}

.card-skeleton-grid-square .card-skeleton-poster {
  aspect-ratio: 1 / 1;
}

@media (min-width: 600px) {
  .card-skeleton-grid-portrait.card-skeleton-grid-default {
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  }

  .card-skeleton-grid-portrait.card-skeleton-grid-compact {
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  }
}

@media (min-width: 960px) {
  .card-skeleton-grid-portrait.card-skeleton-grid-default {
    grid-template-columns: repeat(auto-fill, minmax(165px, 1fr));
  }

  .card-skeleton-grid-portrait.card-skeleton-grid-compact {
    grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  }

  .card-skeleton-grid-thumb.card-skeleton-grid-compact {
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  }
}
</style>
