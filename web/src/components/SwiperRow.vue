<script setup lang="ts">
import { computed, useId } from 'vue'
import { Swiper, SwiperSlide } from 'swiper/vue'
import { Navigation, FreeMode } from 'swiper/modules'
import { useMediaQuery } from '@vueuse/core'
// @ts-ignore
import 'swiper/css'
// @ts-ignore
import 'swiper/css/free-mode'
import MediaCard from './MediaCard.vue'

const props = withDefaults(defineProps<{
  title: string
  items: any[]
  showProgress?: boolean
  linkTo?: string
  shape?: 'portrait' | 'thumb'
}>(), {
  showProgress: false,
  shape: 'portrait',
})

const uid = useId()
const isMobile = useMediaQuery('(max-width: 599px)')
const modules = [Navigation, FreeMode]

const navPrev = computed(() => `.swiper-row-${uid} .sr-prev`)
const navNext = computed(() => `.swiper-row-${uid} .sr-next`)

const thumbBreakpoints: Record<number, any> = {
  0: { slidesPerView: 1.5, slidesPerGroup: 1, spaceBetween: 12 },
  600: { slidesPerView: 2, slidesPerGroup: 2, spaceBetween: 16 },
  960: { slidesPerView: 3, slidesPerGroup: 3, spaceBetween: 16 },
  1904: { slidesPerView: 4, slidesPerGroup: 4, spaceBetween: 16 },
}

const portraitBreakpoints: Record<number, any> = {
  0: { slidesPerView: 2.5, slidesPerGroup: 2, spaceBetween: 12 },
  600: { slidesPerView: 4, slidesPerGroup: 3, spaceBetween: 16 },
  960: { slidesPerView: 6, slidesPerGroup: 5, spaceBetween: 16 },
  1904: { slidesPerView: 8, slidesPerGroup: 6, spaceBetween: 16 },
}

const breakpoints = computed(() => props.shape === 'thumb' ? thumbBreakpoints : portraitBreakpoints)
</script>

<template>
  <section v-if="items.length" :class="`swiper-section swiper-row-${uid}`">
    <div class="sr-header">
      <h2 class="sr-title"><span>{{ title }}</span></h2>
      <div class="sr-nav">
        <router-link v-if="linkTo" :to="linkTo" class="sr-viewall">查看全部</router-link>
        <button class="sr-prev sr-arrow" aria-label="上一页">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M10 3L5 8L10 13" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </button>
        <button class="sr-next sr-arrow" aria-label="下一页">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M6 3L11 8L6 13" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </button>
      </div>
    </div>

    <Swiper
      :modules="modules"
      :free-mode="isMobile"
      :navigation="{ prevEl: navPrev, nextEl: navNext, disabledClass: 'sr-arrow-disabled' }"
      :breakpoints="breakpoints"
      class="sr-swiper"
    >
      <SwiperSlide v-for="item in items" :key="item.Id">
        <MediaCard :item="item" :show-progress="showProgress" :shape="shape" />
      </SwiperSlide>
    </Swiper>
  </section>
</template>

<style scoped>
.swiper-section {
  width: 100%;
  min-width: 0;
  margin-bottom: 0;
}

.sr-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
  margin-bottom: 12px;
  padding: 0 8px;
}

.sr-title {
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

.sr-title::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 1.1em;
  border-radius: 2px;
  background: var(--app-primary, #e50914);
  flex-shrink: 0;
}

.sr-nav {
  display: flex;
  align-items: center;
  gap: 10px;
}

.sr-viewall {
  font-size: 13px;
  color: var(--app-primary);
  text-decoration: none;
  font-weight: 500;
  margin-right: 6px;
  letter-spacing: 0.02em;
}
.sr-viewall:hover { opacity: 0.85; }

.sr-arrow {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 0;
  background: rgba(255, 255, 255, 0.06);
  color: var(--app-text);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  padding: 0;
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
  transition: background 0.2s ease, transform 0.2s ease;
}
.sr-arrow:hover {
  background: rgba(255, 255, 255, 0.14);
  transform: translateY(-1px);
}
.sr-arrow-disabled {
  opacity: 0.25;
  pointer-events: none;
}

.sr-swiper {
  width: 100%;
  min-width: 0;
  overflow: hidden;
  /* 下边给 hover scale 和环境阴影留一点空间,避免被 swiper 的 overflow clip 切掉 */
  padding: 4px 8px 20px;
}
.sr-swiper :deep(.swiper-slide) { height: auto; }
</style>
