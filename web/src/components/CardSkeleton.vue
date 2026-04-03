<script setup lang="ts">
import Skeleton from './Skeleton.vue';

withDefaults(
  defineProps<{
    count?: number;
  }>(),
  {
    count: 6,
  }
);
</script>

<template>
  <div
    style="
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(175px, 1fr));
      gap: 20px;
    "
  >
    <div v-for="i in count" :key="i">
      <Skeleton
        :height="263"
        border-radius="var(--app-radius, 10px)"
        :style="{ marginBottom: '8px' }"
      />
      <Skeleton :height="14" width="80%" :style="{ marginBottom: '4px' }" />
      <Skeleton :height="12" width="50%" />
    </div>
  </div>
</template>
