<script setup lang="ts">
import Skeleton from './Skeleton.vue';
</script>

<template>
  <div>
    <Skeleton :height="400" :border-radius="0" />
    <div style="padding: 32px 40px">
      <Skeleton :height="28" width="40%" :style="{ marginBottom: '12px' }" />
      <Skeleton :height="16" width="60%" :style="{ marginBottom: '24px' }" />
      <Skeleton :height="80" width="70%" />
    </div>
  </div>
</template>
