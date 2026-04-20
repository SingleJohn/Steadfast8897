<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Swiper, SwiperSlide } from 'swiper/vue'
import { Autoplay, Pagination } from 'swiper/modules'
// @ts-ignore
import 'swiper/css'
// @ts-ignore
import 'swiper/css/pagination'
import { getImageUrl, toggleFavorite } from '../api/client'

defineProps<{ items: any[] }>()
const router = useRouter()
const modules = [Autoplay, Pagination]

function formatRating(r?: number) {
  return r ? r.toFixed(1) : null
}

function topGenres(item: any): string[] {
  return (item?.Genres || []).slice(0, 2)
}

function metaExtra(item: any): string {
  if (item.Type === 'Series' && item.ChildCount) return `${item.ChildCount} 集`
  if (item.RunTimeTicks) {
    const min = Math.round(item.RunTimeTicks / 10_000_000 / 60)
    if (min < 60) return `${min}分钟`
    const h = Math.floor(min / 60)
    const m = min % 60
    return m > 0 ? `${h}小时 ${m}分钟` : `${h}小时`
  }
  return ''
}

function isFavorite(item: any): boolean {
  return !!item?.UserData?.IsFavorite
}

async function toggleFav(item: any) {
  const next = !isFavorite(item)
  try {
    await toggleFavorite(item.Id, next)
    if (!item.UserData) item.UserData = {}
    item.UserData.IsFavorite = next
  } catch {
    // 静默失败,不打扰用户(Hero 是展示区)
  }
}

function goPlay(item: any) {
  router.push(item.Type === 'Movie' ? `/play/${item.Id}` : `/item/${item.Id}`)
}

function goDetail(item: any) {
  router.push(`/item/${item.Id}`)
}

function backdropId(item: any): string {
  return item.ParentBackdropItemId || item.Id
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
      class="hero-swiper"
    >
      <SwiperSlide v-for="item in items" :key="item.Id">
        <article class="hero-slide">
          <img
            :src="getImageUrl(backdropId(item), 'Backdrop', 1920)"
            :alt="item.Name"
            class="hero-backdrop"
          />
          <div class="hero-shade" />

          <div class="hero-inner">
            <div class="hero-content">
              <div class="hero-meta-row">
                <span
                  v-for="(g, i) in topGenres(item)"
                  :key="g"
                  class="pill"
                  :class="i === 0 ? 'pill-accent' : 'pill-glass'"
                >{{ g }}</span>
                <span v-if="item.CommunityRating" class="hero-rating">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="#f5c518" aria-hidden="true">
                    <path d="M12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"/>
                  </svg>
                  <strong>{{ formatRating(item.CommunityRating) }}</strong>
                  <span class="hero-rating-label">IMDb</span>
                </span>
                <template v-if="item.ProductionYear">
                  <span class="hero-dot">•</span>
                  <span>{{ item.ProductionYear }}</span>
                </template>
                <template v-if="metaExtra(item)">
                  <span class="hero-dot">•</span>
                  <span>{{ metaExtra(item) }}</span>
                </template>
              </div>

              <h1 class="hero-title">{{ item.Name }}</h1>

              <p v-if="item.Overview" class="hero-overview">{{ item.Overview }}</p>

              <div class="hero-actions">
                <button class="hero-btn hero-btn-primary" @click.prevent="goPlay(item)">
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                    <path d="M8 5v14l11-7z"/>
                  </svg>
                  <span>播放</span>
                </button>
                <button class="hero-btn hero-btn-glass" @click.prevent="goDetail(item)">
                  <span>查看详情</span>
                </button>
                <button
                  class="hero-btn hero-btn-square"
                  :aria-pressed="isFavorite(item)"
                  :title="isFavorite(item) ? '取消收藏' : '加入收藏'"
                  @click.prevent="toggleFav(item)"
                >
                  <svg v-if="isFavorite(item)" width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                    <path d="M12 21s-7-4.35-7-10a5 5 0 0 1 9-3 5 5 0 0 1 9 3c0 5.65-7 10-7 10z"/>
                  </svg>
                  <svg v-else width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M12 5v14M5 12h14"/>
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </article>
      </SwiperSlide>
    </Swiper>
    <div class="hero-pagination" />
  </div>
</template>

<style scoped>
.hero-carousel {
  position: relative;
  width: 100%;
  margin: 0;
  overflow: hidden;
  border-radius: var(--app-radius-xl, 24px);
  box-shadow: var(--app-shadow-ambient, 0 24px 48px rgba(0, 0, 0, 0.55));
}

.hero-swiper {
  width: 100%;
}

.hero-swiper :deep(.swiper-slide) {
  position: relative;
  overflow: hidden;
  background: #0e0e0e;
  height: clamp(480px, 72vh, 720px);
}

.hero-slide {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
}

.hero-backdrop {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 20%;
  z-index: 1;
}

.hero-shade {
  position: absolute;
  inset: 0;
  z-index: 2;
  pointer-events: none;
  background:
    linear-gradient(
      to bottom,
      rgba(14, 14, 14, 0) 0%,
      rgba(14, 14, 14, 0) 35%,
      rgba(14, 14, 14, 0.5) 65%,
      rgba(14, 14, 14, 0.92) 90%,
      #0e0e0e 100%
    ),
    linear-gradient(
      to right,
      rgba(14, 14, 14, 0.55) 0%,
      rgba(14, 14, 14, 0.28) 35%,
      rgba(14, 14, 14, 0) 65%
    );
}

.hero-inner {
  position: absolute;
  inset: 0;
  z-index: 3;
  display: flex;
  align-items: flex-end;
  padding: 0 48px 72px;
}

.hero-content {
  width: 100%;
  max-width: 1480px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
  color: #fff;
}

/* Meta row:类型 pill + IMDb 评分 + 年份 + 时长 */
.hero-meta-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 14px;
  font-size: 14px;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.88);
}

.hero-rating {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.hero-rating strong {
  font-weight: 700;
  color: #fff;
}

.hero-rating-label {
  color: rgba(255, 255, 255, 0.6);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.hero-dot {
  color: rgba(255, 255, 255, 0.4);
}

/* Title:Manrope 巨幅 editorial */
.hero-title {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: clamp(2.25rem, 6.5vw, 5.5rem);
  font-weight: 900;
  line-height: 1.02;
  letter-spacing: -0.035em;
  text-transform: none;
  margin: 0;
  max-width: 20ch;
  color: #fff;
  text-shadow: 0 4px 24px rgba(0, 0, 0, 0.55);
}

.hero-overview {
  max-width: 56ch;
  margin: 0;
  font-size: 15px;
  line-height: 1.7;
  color: rgba(255, 255, 255, 0.78);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* Actions */
.hero-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}

.hero-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border: 0;
  cursor: pointer;
  font-family: inherit;
  font-size: 15px;
  font-weight: 700;
  line-height: 1;
  padding: 14px 26px;
  border-radius: 14px;
  transition: transform 0.2s ease, background 0.2s ease, filter 0.2s ease;
}
.hero-btn:active {
  transform: translateY(1px);
}

.hero-btn-primary {
  background: var(--app-primary, #e50914);
  color: #fff;
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.4);
}
.hero-btn-primary:hover {
  filter: brightness(1.1);
}

.hero-btn-glass {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
  min-width: 140px;
}
.hero-btn-glass:hover {
  background: rgba(255, 255, 255, 0.18);
}

.hero-btn-square {
  width: 52px;
  height: 52px;
  padding: 0;
  color: #fff;
  background: rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
}
.hero-btn-square:hover {
  background: rgba(255, 255, 255, 0.18);
}
.hero-btn-square[aria-pressed='true'] {
  color: var(--app-accent-red-soft, #ffb4aa);
}

/* Pagination:右下胶囊 */
.hero-pagination {
  position: absolute;
  right: 32px;
  bottom: 28px;
  z-index: 10;
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}

.hero-carousel :deep(.swiper-pagination-bullet) {
  width: 8px;
  height: 8px;
  background: rgba(255, 255, 255, 0.32);
  opacity: 1;
  border-radius: 999px;
  transition: width 0.3s ease, background 0.3s ease;
}
.hero-carousel :deep(.swiper-pagination-bullet-active) {
  background: #fff;
  width: 28px;
}

@media (max-width: 959px) {
  .hero-swiper :deep(.swiper-slide) {
    height: clamp(440px, 66vh, 620px);
  }
  .hero-inner {
    padding: 0 28px 56px;
  }
  .hero-pagination {
    right: 20px;
    bottom: 20px;
  }
}

@media (max-width: 599px) {
  .hero-carousel {
    border-radius: var(--app-radius, 16px);
  }
  .hero-swiper :deep(.swiper-slide) {
    height: clamp(400px, 62vh, 520px);
  }
  .hero-inner {
    padding: 0 20px 40px;
  }
  .hero-content {
    gap: 14px;
  }
  .hero-title {
    font-size: clamp(1.9rem, 8vw, 2.6rem);
    max-width: 100%;
  }
  .hero-overview {
    -webkit-line-clamp: 2;
    font-size: 13px;
  }
  .hero-btn {
    padding: 12px 20px;
    font-size: 14px;
  }
  .hero-btn-glass {
    min-width: 0;
  }
  .hero-btn-square {
    width: 46px;
    height: 46px;
  }
  .hero-pagination {
    right: 16px;
    bottom: 14px;
  }
}
</style>
