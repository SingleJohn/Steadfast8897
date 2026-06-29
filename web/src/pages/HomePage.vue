<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NSkeleton } from 'naive-ui'
import {
  getFavoriteItems,
  getItems,
  getLatestBatch,
  getResumeItems,
  getViews,
} from '../api/client'
import CardSkeleton from '../components/CardSkeleton.vue'
import LibraryTabs from '../components/LibraryTabs.vue'
import { useBranding } from '@/composables/useBranding'
import { useAuthStore } from '../stores/auth'

interface LibrarySection {
  id: string
  name: string
  items: any[]
}

const router = useRouter()
const auth = useAuthStore()
const branding = useBranding()
const HeroCarousel = defineAsyncComponent(() => import('../components/HeroCarousel.vue'))
const SwiperRow = defineAsyncComponent(() => import('../components/SwiperRow.vue'))

const heroItems = ref<any[]>([])
const resumeItems = ref<any[]>([])
const favoriteItems = ref<any[]>([])
const libraryViews = ref<any[]>([])
const latestByLibrary = ref<LibrarySection[]>([])
const loading = ref(true)
const latestLoading = ref(false)
const latestLoaded = ref(false)

const heroReady = computed(() => heroItems.value.length > 0)
const hasContent = computed(() => (
  resumeItems.value.length > 0
  || favoriteItems.value.length > 0
  || latestByLibrary.value.length > 0
))
const hasLibraryViews = computed(() => libraryViews.value.length > 0)
const showEmpty = computed(() => !loading.value && latestLoaded.value && !hasContent.value && !hasLibraryViews.value)
const showHomeShell = computed(() => heroReady.value || hasContent.value || latestLoading.value || libraryViews.value.length > 0)

async function loadLatestSections(libraries: any[]) {
  latestLoading.value = true
  latestLoaded.value = false
  latestByLibrary.value = []
  try {
    const libraryIds = libraries.map((lib: any) => lib.Id)
    const batchResult = libraryIds.length > 0 ? await getLatestBatch(libraryIds, 20) : {}
    latestByLibrary.value = libraries
      .map((lib: any) => ({ id: lib.Id, name: lib.Name, items: batchResult[lib.Id] || [] }))
      .filter((section: LibrarySection) => section.items.length > 0)
  } catch {
    latestByLibrary.value = []
    latestLoaded.value = false
    return
  } finally {
    latestLoading.value = false
  }
  latestLoaded.value = true
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
    loading.value = false
    void loadLatestSections(libraries)
  } catch (err) {
    console.error('Failed to load home:', err)
    loading.value = false
    latestLoaded.value = true
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

  <div v-else-if="showEmpty" class="home-empty">
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
    <h2 class="empty-title">欢迎使用 {{ branding.serverName.value || 'FYMS' }}</h2>
    <p class="empty-desc">
      {{ auth.isAdmin ? '当前还没有可浏览的媒体内容。先去添加媒体库并触发扫描，首页会自动展示最新内容。' : '当前还没有可浏览的媒体内容，请联系管理员完成媒体库接入与扫描。' }}
    </p>
    <div v-if="auth.isAdmin" class="empty-actions">
      <n-button type="primary" @click="goManageLibraries">管理媒体库</n-button>
      <n-button secondary @click="goSearch">打开搜索</n-button>
    </div>
  </div>

  <div v-else-if="showHomeShell" class="home-page">
    <div v-if="heroReady" class="home-hero-wrap">
      <Suspense>
        <HeroCarousel :items="heroItems" />
        <template #fallback>
          <n-skeleton class="home-hero-skeleton" height="clamp(480px, 72vh, 720px)" style="border-radius: 0" />
        </template>
      </Suspense>
    </div>

    <div class="home-sections">
      <div class="home-rows">
        <LibraryTabs
          v-if="libraryViews.length > 0"
          :items="libraryViews"
          title="媒体库"
          shape="thumb"
        />
        <Suspense v-if="resumeItems.length > 0">
          <SwiperRow
            title="继续观看"
            :items="resumeItems"
            shape="thumb"
            density="compact"
            :show-progress="true"
          />
          <template #fallback>
            <div class="home-loading-row">
              <n-skeleton text style="width: 140px; margin-bottom: 16px" />
              <CardSkeleton :count="6" density="compact" />
            </div>
          </template>
        </Suspense>
        <Suspense v-if="favoriteItems.length > 0">
          <SwiperRow
            title="我的收藏"
            :items="favoriteItems"
            shape="mixed"
            density="compact"
          />
          <template #fallback>
            <div class="home-loading-row">
              <n-skeleton text style="width: 140px; margin-bottom: 16px" />
              <CardSkeleton :count="6" density="compact" />
            </div>
          </template>
        </Suspense>
        <Suspense
          v-for="{ id, name, items } in latestByLibrary"
          :key="id"
        >
          <SwiperRow
            :title="`最新 ${name}`"
            :items="items"
            :link-to="`/library/${id}`"
            shape="mixed"
            density="compact"
          />
          <template #fallback>
            <div class="home-loading-row">
              <n-skeleton text style="width: 140px; margin-bottom: 16px" />
              <CardSkeleton :count="6" density="compact" />
            </div>
          </template>
        </Suspense>
        <div v-if="latestLoading" class="home-loading-row home-latest-loading">
          <n-skeleton text style="width: 140px; margin-bottom: 16px" />
          <CardSkeleton :count="6" density="compact" />
        </div>
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
 * 内容文字区域的 1480 限宽在 HeroCarousel 内部 .hero-carousel-content 处理。
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

  .empty-actions {
    flex-direction: column;
  }
}
</style>
