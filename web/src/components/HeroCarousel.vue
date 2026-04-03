<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { Swiper, SwiperSlide } from 'swiper/vue'
import { Autoplay, Pagination } from 'swiper/modules'
// @ts-ignore
import 'swiper/css'
// @ts-ignore
import 'swiper/css/pagination'
import { getImageUrl } from '../api/client'

const props = defineProps<{ items: any[] }>()
const router = useRouter()
const activeIndex = ref(0)
const modules = [Autoplay, Pagination]
const isDesktop = useMediaQuery('(min-width: 600px)')

const current = computed(() => props.items[activeIndex.value])

function onSlideChange(swiper: any) {
  activeIndex.value = swiper.realIndex
}

function formatRating(r?: number) {
  return r ? r.toFixed(1) : null
}

function goPlay(item: any) {
  router.push(item.Type === 'Movie' ? `/play/${item.Id}` : `/item/${item.Id}`)
}

function goDetail(item: any) {
  router.push(`/item/${item.Id}`)
}
</script>

<template>
  <div v-if="items.length" class="hero-carousel">
    <Swiper
      :modules="modules"
      :slides-per-view="1"
      :autoplay="{ delay: 8000, disableOnInteraction: false }"
      :pagination="{ clickable: true, el: '.hero-pagination' }"
      loop
      @slide-change="onSlideChange"
      class="hero-swiper"
    >
      <SwiperSlide v-for="item in items" :key="item.Id">
        <div class="slide-backdrop" :class="{ 'sm-and-up': isDesktop }">
          <img
            :src="getImageUrl(item.ParentBackdropItemId || item.Id, 'Backdrop', 1920)"
            :alt="item.Name"
            class="slide-backdrop-img"
          />
        </div>
        <div class="slide-content" :class="{ 'sm-and-up': isDesktop }">
          <div class="slide-container">
            <div class="slide-info">
              <p class="slide-overline">最近添加</p>
              <h1 class="slide-title">{{ item.Name }}</h1>
              <p v-if="item.Overview" class="slide-overview">{{ item.Overview }}</p>
              <div class="slide-meta">
                <span v-if="item.ProductionYear">{{ item.ProductionYear }}</span>
                <span v-if="item.CommunityRating" class="slide-rating">★ {{ formatRating(item.CommunityRating) }}</span>
                <span v-if="item.RunTimeTicks">{{ Math.round(item.RunTimeTicks / 10_000_000 / 60) }}分钟</span>
              </div>
              <div class="slide-buttons">
                <button class="btn-play" @click.prevent="goPlay(item)">
                  <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>
                  播放
                </button>
                <button class="btn-detail" @click.prevent="goDetail(item)">查看详情</button>
              </div>
            </div>
          </div>
        </div>
      </SwiperSlide>
    </Swiper>
    <div class="hero-pagination" />
  </div>
</template>

<style scoped>
.hero-carousel {
  position: relative;
  width: 100%;
  margin: 12px 0 0;
  overflow: hidden;
  border-radius: var(--app-radius);
  box-shadow: var(--app-shadow-card);
}

.hero-swiper {
  width: 100%;
}

.hero-swiper :deep(.swiper-slide) {
  position: relative;
  overflow: hidden;
  background: linear-gradient(180deg, #09101c, #0f172a);
  min-height: 420px;
}

.slide-backdrop {
  position: relative;
  width: 100%;
  padding-bottom: 56.25%;
  z-index: 1;
  mask-image: linear-gradient(180deg, rgba(37,18,18,0.75) 0%, rgba(0,0,0,0) 100%);
}

.slide-backdrop.sm-and-up {
  width: 80%;
  margin-left: auto;
  margin-right: 0;
  padding-bottom: 45%;
  mask-image:
    linear-gradient(
      to right,
      hsla(0,0%,0%,0) 0%,
      hsla(0,0%,0%,0.182) 5.6%,
      hsla(0,0%,0%,0.34) 9.9%,
      hsla(0,0%,0%,0.476) 13.1%,
      hsla(0,0%,0%,0.593) 15.7%,
      hsla(0,0%,0%,0.69) 17.9%,
      hsla(0,0%,0%,0.771) 20.2%,
      hsla(0,0%,0%,0.836) 22.9%,
      hsla(0,0%,0%,0.888) 26.3%,
      hsla(0,0%,0%,0.927) 30.8%,
      hsla(0,0%,0%,0.956) 36.7%,
      hsla(0,0%,0%,0.976) 44.4%,
      hsla(0,0%,0%,0.989) 54.3%,
      hsla(0,0%,0%,0.996) 66.6%,
      hsla(0,0%,0%,0.999) 81.7%,
      hsl(0,0%,0%) 100%
    ),
    linear-gradient(
      to top,
      hsla(0,0%,0%,0) 0%,
      hsla(0,0%,0%,0.182) 5.6%,
      hsla(0,0%,0%,0.34) 9.9%,
      hsla(0,0%,0%,0.476) 13.1%,
      hsla(0,0%,0%,0.593) 15.7%,
      hsla(0,0%,0%,0.69) 17.9%,
      hsla(0,0%,0%,0.771) 20.2%,
      hsla(0,0%,0%,0.836) 22.9%,
      hsla(0,0%,0%,0.888) 26.3%,
      hsla(0,0%,0%,0.927) 30.8%,
      hsla(0,0%,0%,0.956) 36.7%,
      hsla(0,0%,0%,0.976) 44.4%,
      hsla(0,0%,0%,0.989) 54.3%,
      hsla(0,0%,0%,0.996) 66.6%,
      hsla(0,0%,0%,0.999) 81.7%,
      hsl(0,0%,0%) 100%
    );
  mask-composite: intersect;
  -webkit-mask-composite: source-in;
}

.slide-backdrop-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.slide-content {
  position: absolute;
  inset: 0;
  z-index: 2;
  display: flex;
  align-items: end;
}

.slide-content.sm-and-up {
  top: 56px;
  align-items: center;
}

.slide-container {
  width: 100%;
  max-width: 1480px;
  margin: 0 auto;
  padding: 0 32px 40px;
}

.slide-content.sm-and-up .slide-container {
  padding: 0 32px;
}

.slide-info {
  max-width: min(44rem, 48%);
  padding: 32px 0;
}

@media (max-width: 599px) {
  .slide-info { max-width: 100%; }
  .slide-container { padding: 0 20px 24px; }
}

.slide-overline {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 1px;
  color: rgba(255,255,255,0.6);
  margin: 0 0 8px;
}

.slide-title {
  font-size: clamp(2rem, 4vw, 3.4rem);
  font-weight: 700;
  color: #fff;
  margin: 0 0 12px;
  line-height: 1.15;
  text-shadow: 0 2px 12px rgba(0,0,0,0.5);
}

.slide-overview {
  margin: 0 0 18px;
  max-width: 52ch;
  color: rgba(255, 255, 255, 0.72);
  font-size: 14px;
  line-height: 1.7;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.slide-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
  font-size: 14px;
  color: rgba(255,255,255,0.7);
}

.slide-rating { color: #ffd700; }

.slide-buttons {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.btn-play {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 24px; border-radius: var(--app-radius);
  background: rgba(255,255,255,0.92); color: #0f172a;
  font-size: 15px; font-weight: 600;
  border: none; cursor: pointer; transition: filter 0.2s;
}
.btn-play:hover { filter: brightness(1.15); }

.btn-detail {
  display: inline-flex; align-items: center;
  padding: 10px 24px; border-radius: var(--app-radius);
  background: rgba(255,255,255,0.08); color: #fff;
  font-size: 15px; font-weight: 600;
  border: 1px solid rgba(255,255,255,0.18);
  cursor: pointer; transition: background 0.2s;
  min-width: 11em; justify-content: center;
}
.btn-detail:hover { background: rgba(255,255,255,0.16); }

.hero-pagination {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 12px;
  z-index: 10;
  display: flex;
  justify-content: center;
}

.hero-carousel :deep(.swiper-pagination-bullet) {
  width: 8px; height: 8px;
  background: rgba(255,255,255,0.4);
  opacity: 1; transition: all 0.3s;
}
.hero-carousel :deep(.swiper-pagination-bullet-active) {
  background: #fff; width: 24px; border-radius: var(--app-radius);
}

@media (max-width: 599px) {
  .hero-carousel {
    margin: 8px 0 0;
    width: 100%;
    border-radius: var(--app-radius);
  }
  .slide-title { font-size: 24px; }
  .slide-overview { -webkit-line-clamp: 2; }
}
</style>
