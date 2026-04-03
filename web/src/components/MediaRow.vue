<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue';
import MediaCard from './MediaCard.vue';

const props = withDefaults(
  defineProps<{
    title: string;
    items: any[];
    showProgress?: boolean;
    linkTo?: string;
  }>(),
  {
    showProgress: false,
  },
);

const CARD_WIDTH = 175;
const GAP = 20;
const SCROLL_CARDS = 3;
const SCROLL_AMOUNT = (CARD_WIDTH + GAP) * SCROLL_CARDS;

const scrollRef = ref<HTMLDivElement | null>(null);
const canScrollLeft = ref(false);
const canScrollRight = ref(false);

let scrollEl: HTMLDivElement | null = null;
let resizeObserver: ResizeObserver | null = null;

function checkScroll() {
  const el = scrollRef.value;
  if (!el) return;
  canScrollLeft.value = el.scrollLeft > 0;
  canScrollRight.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
}

function scroll(dir: number) {
  scrollRef.value?.scrollBy({ left: dir * SCROLL_AMOUNT, behavior: 'smooth' });
}

onMounted(() => {
  scrollEl = scrollRef.value;
  if (!scrollEl) return;
  checkScroll();
  scrollEl.addEventListener('scroll', checkScroll, { passive: true });
  resizeObserver = new ResizeObserver(checkScroll);
  resizeObserver.observe(scrollEl);
});

onUnmounted(() => {
  if (scrollEl) {
    scrollEl.removeEventListener('scroll', checkScroll);
  }
  resizeObserver?.disconnect();
});

watch(
  () => props.items,
  () => {
    nextTick(() => checkScroll());
  },
  { deep: true },
);
</script>

<template>
  <section
    v-if="items.length"
    :style="{ marginBottom: 44, position: 'relative' }"
    class="fyms-row"
  >
    <!-- 区域标题栏 -->
    <div
      :style="{
        display: 'flex',
        alignItems: 'baseline',
        justifyContent: 'space-between',
        marginBottom: 16,
        padding: '0 4px',
      }"
    >
      <h2
        :style="{
          fontSize: 20,
          fontWeight: 600,
          color: 'var(--app-text)',
          margin: 0,
          letterSpacing: '0.3px',
        }"
      >
        {{ title }}
      </h2>
      <router-link
        v-if="linkTo"
        :to="linkTo"
        :style="{
          fontSize: 13,
          color: 'var(--app-primary, #10b981)',
          textDecoration: 'none',
          fontWeight: 500,
          whiteSpace: 'nowrap',
          transition: 'opacity 0.15s',
        }"
        @mouseenter="(e) => ((e.currentTarget as HTMLElement).style.opacity = '0.8')"
        @mouseleave="(e) => ((e.currentTarget as HTMLElement).style.opacity = '1')"
      >
        查看全部 &gt;
      </router-link>
    </div>

    <!-- 滚动区域容器 -->
    <div :style="{ position: 'relative' }">
      <!-- 左侧渐变遮罩 -->
      <div
        v-if="canScrollLeft"
        :style="{
          position: 'absolute',
          top: 0,
          left: 0,
          bottom: 0,
          width: 40,
          background:
            'linear-gradient(to right, var(--app-bg, #020617) 0%, transparent 40px)',
          pointerEvents: 'none',
          zIndex: 2,
        }"
      />

      <!-- 右侧渐变遮罩 -->
      <div
        v-if="canScrollRight"
        :style="{
          position: 'absolute',
          top: 0,
          right: 0,
          bottom: 0,
          width: 40,
          background:
            'linear-gradient(to left, var(--app-bg, #020617) 0%, transparent 40px)',
          pointerEvents: 'none',
          zIndex: 2,
        }"
      />

      <!-- 左滚动按钮 -->
      <button
        v-if="canScrollLeft"
        class="fyms-row-btn"
        type="button"
        aria-label="向左滚动"
        :style="{
          position: 'absolute',
          left: 8,
          top: '50%',
          transform: 'translateY(-60%)',
          zIndex: 3,
          width: 36,
          height: 36,
          borderRadius: '50%',
          border: '1px solid rgba(255,255,255,0.12)',
          background: 'rgba(0,0,0,0.55)',
          backdropFilter: 'blur(12px)',
          WebkitBackdropFilter: 'blur(12px)',
          color: '#fff',
          fontSize: 18,
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: 0,
          transition: 'background 0.15s, transform 0.15s',
        }"
        @click="scroll(-1)"
        @mouseenter="(e) => ((e.currentTarget as HTMLButtonElement).style.background = 'rgba(0,0,0,0.75)')"
        @mouseleave="(e) => ((e.currentTarget as HTMLButtonElement).style.background = 'rgba(0,0,0,0.55)')"
      >
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
          <path
            d="M10 3L5 8L10 13"
            stroke="#fff"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>

      <!-- 右滚动按钮 -->
      <button
        v-if="canScrollRight"
        class="fyms-row-btn"
        type="button"
        aria-label="向右滚动"
        :style="{
          position: 'absolute',
          right: 8,
          top: '50%',
          transform: 'translateY(-60%)',
          zIndex: 3,
          width: 36,
          height: 36,
          borderRadius: '50%',
          border: '1px solid rgba(255,255,255,0.12)',
          background: 'rgba(0,0,0,0.55)',
          backdropFilter: 'blur(12px)',
          WebkitBackdropFilter: 'blur(12px)',
          color: '#fff',
          fontSize: 18,
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: 0,
          transition: 'background 0.15s, transform 0.15s',
        }"
        @click="scroll(1)"
        @mouseenter="(e) => ((e.currentTarget as HTMLButtonElement).style.background = 'rgba(0,0,0,0.75)')"
        @mouseleave="(e) => ((e.currentTarget as HTMLButtonElement).style.background = 'rgba(0,0,0,0.55)')"
      >
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
          <path
            d="M6 3L11 8L6 13"
            stroke="#fff"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>

      <!-- 水平滚动内容区 -->
      <div
        ref="scrollRef"
        class="fyms-row-scroll"
        :style="{
          display: 'flex',
          gap: `${GAP}px`,
          overflowX: 'auto',
          overflowY: 'hidden',
          padding: '4px 0',
        }"
      >
        <MediaCard
          v-for="item in items"
          :key="item.Id"
          :item="item"
          :show-progress="showProgress"
          :width="CARD_WIDTH"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.fyms-row .fyms-row-btn {
  opacity: 0;
  transition:
    opacity 0.2s,
    background 0.15s;
}
.fyms-row:hover .fyms-row-btn {
  opacity: 1;
}
.fyms-row-scroll {
  scrollbar-width: none;
  -ms-overflow-style: none;
}
.fyms-row-scroll::-webkit-scrollbar {
  display: none;
}
</style>
