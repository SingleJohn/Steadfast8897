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

<style scoped>
.skeleton {
  position: relative;
  overflow: hidden;
  background:
    linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.12), transparent),
    rgba(148, 163, 184, 0.14);
  background-size: 240px 100%, 100% 100%;
  animation: skeleton-shimmer 1.35s ease-in-out infinite;
}

:global(html:not(.app-dark)) .skeleton {
  background:
    linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.72), transparent),
    rgba(148, 163, 184, 0.2);
  background-size: 240px 100%, 100% 100%;
}

@keyframes skeleton-shimmer {
  0% { background-position: -240px 0, 0 0; }
  100% { background-position: calc(100% + 240px) 0, 0 0; }
}

@media (prefers-reduced-motion: reduce) {
  .skeleton {
    animation: none;
  }
}
</style>
