<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NProgress, NSkeleton, NTag } from 'naive-ui'
import {
  getFavoriteItems,
  getItems,
  getLatestBatch,
  getResumeItems,
  getScanProgress,
  getViews,
} from '../api/client'
import CardSkeleton from '../components/CardSkeleton.vue'
import HeroCarousel from '../components/HeroCarousel.vue'
import LibraryTabs from '../components/LibraryTabs.vue'
import SwiperRow from '../components/SwiperRow.vue'
import { useAuthStore } from '../stores/auth'

interface LibrarySection {
  id: string
  name: string
  items: any[]
}

const router = useRouter()
const auth = useAuthStore()

const heroItems = ref<any[]>([])
const resumeItems = ref<any[]>([])
const favoriteItems = ref<any[]>([])
const libraryViews = ref<any[]>([])
const latestByLibrary = ref<LibrarySection[]>([])
const loading = ref(true)
const scanProgress = ref<any[]>([])
let scanTimer: ReturnType<typeof setInterval> | null = null

const heroReady = computed(() => heroItems.value.length > 0)
const hasContent = computed(() => (
  resumeItems.value.length > 0
  || favoriteItems.value.length > 0
  || latestByLibrary.value.length > 0
))
const activeScans = computed(() => scanProgress.value.filter((item: any) => item.Status === 'scanning'))

function clearScanPolling() {
  if (scanTimer) {
    clearInterval(scanTimer)
    scanTimer = null
  }
}

function ensureScanPolling() {
  if (scanTimer || activeScans.value.length === 0) return
  scanTimer = setInterval(() => {
    void refreshScanProgress()
  }, 5000)
}

function scanLabelSuffix(libraryId: string) {
  const sp = scanProgress.value.find((s: any) => s.LibraryId === libraryId)
  return sp?.Status === 'scanning' ? ` (扫描中 ${sp.Percentage}%)` : ''
}

function libraryNameForScan(libraryId: string) {
  return libraryViews.value.find((lib: any) => lib.Id === libraryId)?.Name || '媒体库'
}

async function refreshScanProgress() {
  try {
    const result = await getScanProgress()
    const items = result.Items || []
    scanProgress.value = items
    if (items.some((item: any) => item.Status === 'scanning')) ensureScanPolling()
    else clearScanPolling()
  } catch {
    clearScanPolling()
  }
}

async function loadHome() {
  try {
    const [resume, favorites, views, heroResult] = await Promise.all([
      getResumeItems(20),
      getFavoriteItems(20),
      getViews(),
      getItems({
        Recursive: 'true',
        IncludeItemTypes: 'Movie,Series',
        SortBy: 'Random',
        Limit: '6',
      }).catch(() => ({ Items: [] })),
    ])

    resumeItems.value = resume.Items || []
    favoriteItems.value = favorites.Items || []

    const candidates = (heroResult.Items || []).filter((item: any) => item.BackdropImageTags?.length > 0)
    heroItems.value = candidates.length > 0 ? candidates : []

    const libraries = views.Items || []
    libraryViews.value = libraries
    const realLibraries = libraries.filter((lib: any) => !lib.PlatformLibrary)
    const libraryIds = realLibraries.map((lib: any) => lib.Id)
    const batchResult = libraryIds.length > 0 ? await getLatestBatch(libraryIds, 20) : {}
    latestByLibrary.value = realLibraries
      .map((lib: any) => ({ id: lib.Id, name: lib.Name, items: batchResult[lib.Id] || [] }))
      .filter((section: LibrarySection) => section.items.length > 0)
  } catch (err) {
    console.error('Failed to load home:', err)
  } finally {
    loading.value = false
  }
}

function goSearch() {
  router.push({ name: 'search' })
}

function goBrowseLibrary() {
  const firstLibrary = libraryViews.value[0]
  if (firstLibrary) {
    router.push({ name: 'library', params: { libraryId: firstLibrary.Id } })
    return
  }
  if (auth.isAdmin) {
    router.push({ name: 'media_libraries' })
  }
}

function goManageLibraries() {
  router.push({ name: 'media_libraries' })
}

onMounted(() => {
  void loadHome()
  void refreshScanProgress()
})

onUnmounted(() => {
  clearScanPolling()
})
</script>

<template>
  <div v-if="loading" class="home-loading">
    <n-skeleton class="home-hero-skeleton" height="clamp(480px, 72vh, 720px)" style="border-radius: 0" />
    <div class="home-loading-sections">
      <div v-for="i in 3" :key="`row-${i}`" class="home-loading-row">
        <n-skeleton text style="width: 140px; margin-bottom: 16px" />
        <CardSkeleton :count="6" />
      </div>
    </div>
  </div>

  <div v-else-if="!hasContent" class="home-empty">
    <div class="empty-icon-wrap">
      <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="empty-icon">
        <rect x="2" y="2" width="20" height="20" rx="2" />
        <path d="M7 2v20" />
        <path d="M17 2v20" />
        <path d="M2 12h20" />
        <path d="M2 7h5" />
        <path d="M2 17h5" />
        <path d="M17 7h5" />
        <path d="M17 17h5" />
      </svg>
    </div>
    <h2 class="empty-title">欢迎使用 FYMS</h2>
    <p class="empty-desc">
      {{ auth.isAdmin ? '当前还没有可浏览的媒体内容。先去添加媒体库并触发扫描，首页会自动展示最新内容。' : '当前还没有可浏览的媒体内容，请联系管理员完成媒体库接入与扫描。' }}
    </p>
    <div v-if="auth.isAdmin" class="empty-actions">
      <n-button type="primary" @click="goManageLibraries">管理媒体库</n-button>
      <n-button secondary @click="goSearch">打开搜索</n-button>
    </div>
  </div>

  <div v-else class="home-page">
    <div v-if="heroReady" class="home-hero-wrap">
      <HeroCarousel :items="heroItems" />
    </div>

    <div class="home-sections">
      <section v-if="activeScans.length > 0" class="scan-banner">
        <div class="scan-banner-header">
          <div>
            <p class="scan-banner-eyebrow">后台任务</p>
            <h2 class="scan-banner-title">媒体库扫描进行中</h2>
          </div>
          <n-tag type="warning" round>{{ activeScans.length }} 个任务</n-tag>
        </div>

        <div class="scan-progress-list">
          <article v-for="sp in activeScans" :key="sp.LibraryId" class="scan-progress-card">
            <div class="scan-progress-top">
              <strong>{{ libraryNameForScan(sp.LibraryId) }}</strong>
              <span>{{ sp.Percentage }}%</span>
            </div>
            <n-progress
              type="line"
              :percentage="sp.Percentage"
              :show-indicator="false"
              :height="8"
              border-radius="999px"
            />
            <div class="scan-progress-meta">
              <span>{{ sp.ProcessedItems }}/{{ sp.TotalItems }} 已处理</span>
              <span>{{ sp.Status }}</span>
            </div>
          </article>
        </div>
      </section>

      <div class="home-rows">
        <LibraryTabs
          v-if="libraryViews.length > 0"
          :items="libraryViews"
          title="媒体库"
          shape="thumb"
        />
        <SwiperRow
          v-if="resumeItems.length > 0"
          title="继续观看"
          :items="resumeItems"
          shape="thumb"
          density="compact"
          :show-progress="true"
        />
        <SwiperRow
          v-if="favoriteItems.length > 0"
          title="我的收藏"
          :items="favoriteItems"
          density="compact"
        />
        <SwiperRow
          v-for="{ id, name, items } in latestByLibrary"
          :key="id"
          :title="`最新 ${name}${scanLabelSuffix(id)}`"
          :items="items"
          :link-to="`/library/${id}`"
          density="compact"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.home-loading {
  min-height: 100vh;
}

.home-hero-skeleton {
  margin: 0;
  border-radius: var(--app-radius-xl, 24px) !important;
}

.home-loading-sections {
  position: relative;
  z-index: 4;
  max-width: 1480px;
  margin: 0 auto;
  padding: 20px 8px 0;
}

.home-loading-row {
  margin-bottom: 36px;
}

.home-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
  text-align: center;
  padding: 40px 20px;
}

.empty-icon-wrap {
  width: 88px;
  height: 88px;
  border-radius: var(--app-radius-card, 20px);
  background: var(--app-surface-solid, #1c1b1b);
  border: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 28px;
  box-shadow: var(--app-shadow-1);
}

.empty-icon {
  color: var(--app-text-muted);
}

.empty-title {
  color: var(--app-text);
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 28px;
  font-weight: 800;
  letter-spacing: -0.02em;
  margin: 0 0 10px;
}

.empty-desc {
  color: var(--app-text-muted);
  font-size: 15px;
  max-width: 420px;
  line-height: 1.7;
  margin: 0;
}

.empty-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: center;
  margin-top: 24px;
}

.home-page {
  padding-bottom: 24px;
}

/*
 * Hero 全幅出血:抵消 media-page-inner 24px 水平 padding,让背景顶到 viewport 左右两侧。
 * 内容文字区域的 1480 限宽在 HeroCarousel 内部 .hero-content 处理。
 */
.home-hero-wrap {
  margin: 0 -24px 36px;
  padding: 0;
}

.home-sections {
  position: relative;
  z-index: 4;
  max-width: 1480px;
  min-width: 0;
  margin: 0 auto;
  padding: 0 8px 8px;
}

.home-rows {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 18px;
}

.scan-banner {
  margin: 0 8px 34px;
  padding: 24px 28px;
  border: 0;
  border-radius: var(--app-radius-card, 20px);
  background: var(--app-surface-solid, #1c1b1b);
  box-shadow: var(--app-shadow-1);
}

.scan-banner-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}

.scan-banner-title {
  margin: 0;
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 24px;
  font-weight: 800;
  letter-spacing: -0.01em;
  color: var(--app-text);
}

.scan-banner-eyebrow {
  margin: 0 0 6px;
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--app-accent-red, #e50914);
}

.scan-progress-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
}

.scan-progress-card {
  padding: 16px 18px;
  border-radius: var(--app-radius, 16px);
  background: var(--app-surface-solid-2, #2a2a2a);
  border: 0;
}

.scan-progress-top,
.scan-progress-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.scan-progress-top {
  margin-bottom: 12px;
  color: var(--app-text);
  font-size: 14px;
}

.scan-progress-meta {
  color: var(--app-text-muted);
  margin-top: 10px;
  font-size: 12px;
}

@media (max-width: 959px) {
  .home-hero-wrap {
    margin: 0 -16px 26px;
  }

  .home-loading-sections,
  .home-sections {
    padding-left: 0;
    padding-right: 0;
  }
}

@media (max-width: 599px) {
  .home-hero-wrap {
    margin: 0 -16px 20px;
  }

  .scan-banner {
    margin-left: 0;
    margin-right: 0;
    border-radius: var(--app-radius, 16px);
  }

  .empty-actions {
    flex-direction: column;
  }
}
</style>
