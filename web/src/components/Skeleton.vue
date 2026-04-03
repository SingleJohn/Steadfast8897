<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    width?: string | number;
    height?: string | number;
    borderRadius?: string | number;
    style?: Record<string, string | number>;
  }>(),
  {
    width: '100%',
    height: 16,
  }
);

const rootStyle = computed(() => {
  const w = props.width;
  const h = props.height;
  const br = props.borderRadius ?? 'var(--app-radius, 10px)';
  return {
    width: typeof w === 'number' ? `${w}px` : w,
    height: typeof h === 'number' ? `${h}px` : h,
    borderRadius: typeof br === 'number' ? `${br}px` : br,
    ...props.style,
  };
});
</script>

<template>
  <div class="skeleton" :style="rootStyle" />
</template>
