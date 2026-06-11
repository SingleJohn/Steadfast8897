<script setup lang="ts">
import { NDropdown, NIcon, NInput, useMessage } from 'naive-ui'
import { computed, h, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowBackOutline,
  ColorPaletteOutline,
  HomeOutline,
  LogOutOutline,
  SearchOutline,
  SettingsOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'
import { useBranding } from '@/composables/useBranding'
import { useUiStore } from '@/stores/ui'
import ThemeDrawer from '@/components/ThemeDrawer.vue'
import { getViews, logout as apiLogout } from '@/api/client'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const branding = useBranding()
const ui = useUiStore()
const message = useMessage()

function qFromRoute(): string {
  const q = route.query.q
  if (typeof q === 'string') return q
  if (Array.isArray(q)) return q[0] || ''
  return ''
}

const searchTerm = ref(qFromRoute())
const scrolled = ref(false)
const backdropUrl = ref('')
const themeOpen = ref(false)

const hasMovies = ref(false)
const hasTvshows = ref(false)

function setBackdrop(url: string) {
  backdropUrl.value = url
}

provide('setBackdrop', setBackdrop)

watch(() => route.fullPath, () => {
  backdropUrl.value = ''
  window.scrollTo({ top: 0, behavior: 'auto' })
})

watch(() => route.query.q, () => {
  searchTerm.value = qFromRoute()
})

const userMenuOptions = computed(() => {
  const options: any[] = [
    {
      label: '外观设置',
      key: 'theme',
      icon: () => h(NIcon, { size: 18 }, { default: () => h(ColorPaletteOutline) }),
    },
    {
      label: '返回首页',
      key: 'home',
      icon: () => h(NIcon, { size: 18 }, { default: () => h(HomeOutline) }),
    },
  ]

  if (auth.isAdmin) {
    options.push({
      label: '管理后台',
      key: 'admin',
      icon: () => h(NIcon, { size: 18 }, { default: () => h(SettingsOutline) }),
    })
  }

  options.push(
    { type: 'divider', key: 'divider-1' },
    {
      label: '退出登录',
      key: 'logout',
      icon: () => h(NIcon, { size: 18 }, { default: () => h(LogOutOutline) }),
    },
  )

  return options
})

const transparentShell = computed(() => route.name === 'home' && !scrolled.value)
const showBackdrop = computed(() => !!backdropUrl.value && route.name !== 'home')

const topLevelRoutes = new Set(['home', 'movies', 'tvshows', 'search'])
const showBackButton = computed(() => !topLevelRoutes.has(String(route.name)))

const moviesRoute = computed(() => (hasMovies.value ? { name: 'movies' } : null))
const tvshowsRoute = computed(() => (hasTvshows.value ? { name: 'tvshows' } : null))

const activeNav = computed<'home' | 'movies' | 'tvshows' | ''>(() => {
  if (route.name === 'home') return 'home'
  if (route.name === 'movies') return 'movies'
  if (route.name === 'tvshows') return 'tvshows'
  return ''
})

function handleScroll() {
  scrolled.value = window.scrollY > 10
}

function handleSearch(event?: Event) {
  event?.preventDefault()
  const q = searchTerm.value.trim()
  if (!q) {
    if (route.name !== 'search') router.push({ name: 'search' })
    return
  }
  if (route.name === 'search' && route.query.q === q) return
  router.push({ name: 'search', query: { q } })
}

function goSearch() {
  handleSearch()
}

async function handleLogout() {
  try {
    await apiLogout()
  } catch {
    // ignore
  }
  auth.logout()
  message.success('已退出登录')
  router.push('/login')
}

function handleUserMenuSelect(key: string) {
  if (key === 'theme') themeOpen.value = true
  if (key === 'home') router.push({ name: 'home' })
  if (key === 'admin') router.push({ name: 'admin_overview' })
  if (key === 'logout') void handleLogout()
}

function goBack() {
  if (window.history.length > 1) router.back()
  else router.push({ name: 'home' })
}

async function loadLibraryNav() {
  try {
    const res = await getViews()
    const items = (res.Items || []) as any[]
    hasMovies.value = items.some((i) => (i.CollectionType === 'movies' || i.CollectionType === 'mixed') && !i.PlatformLibrary)
    hasTvshows.value = items.some((i) => (i.CollectionType === 'tvshows' || i.CollectionType === 'mixed') && !i.PlatformLibrary)
  } catch {
    // 未登录或网络异常时静默,nav 只显示"首页"
  }
}

onMounted(() => {
  ui.forceDark = true
  window.addEventListener('scroll', handleScroll, { passive: true })
  handleScroll()
  void branding.loadBranding()
  void loadLibraryNav()
})

onUnmounted(() => {
  ui.forceDark = false
  window.removeEventListener('scroll', handleScroll)
})
</script>

<template>
  <div class="media-shell cinematic" :class="{ 'media-shell-transparent': transparentShell }">
    <transition name="backdrop-fade">
      <div v-if="showBackdrop" class="page-backdrop" :style="{ backgroundImage: `url(${backdropUrl})` }" />
    </transition>

    <div class="media-main">
      <header class="media-topbar" :class="{ 'media-topbar-transparent': transparentShell }">
        <div class="topbar-left">
          <button
            v-if="showBackButton"
            class="icon-button back-button"
            @click="goBack"
            aria-label="返回"
          >
            <n-icon :size="18"><ArrowBackOutline /></n-icon>
          </button>
          <router-link class="brand-mark" :to="{ name: 'home' }" :aria-label="`${branding.serverName.value || 'FYMS'} 首页`">
            <img v-if="branding.iconUrl.value" :src="branding.iconUrl.value" class="brand-mark__icon" alt="" />
            <span class="brand-mark__text">{{ branding.serverName.value || 'FYMS' }}</span>
          </router-link>
          <nav class="topbar-nav" aria-label="主导航">
            <router-link
              :to="{ name: 'home' }"
              class="nav-link"
              :class="{ active: activeNav === 'home' }"
            >首页</router-link>
            <router-link
              v-if="moviesRoute"
              :to="moviesRoute"
              class="nav-link"
              :class="{ active: activeNav === 'movies' }"
            >电影</router-link>
            <router-link
              v-if="tvshowsRoute"
              :to="tvshowsRoute"
              class="nav-link"
              :class="{ active: activeNav === 'tvshows' }"
            >剧集</router-link>
          </nav>
        </div>

        <div class="topbar-right">
          <form class="search-form" @submit.prevent="handleSearch">
            <n-input
              v-model:value="searchTerm"
              clearable
              size="small"
              class="topbar-search"
              placeholder="搜索媒体..."
              @keyup.enter="handleSearch"
            >
              <template #prefix>
                <n-icon :size="16"><SearchOutline /></n-icon>
              </template>
            </n-input>
          </form>
          <button class="icon-button search-icon-only" @click="goSearch" aria-label="打开搜索">
            <n-icon :size="18"><SearchOutline /></n-icon>
          </button>
          <n-dropdown :options="userMenuOptions" @select="handleUserMenuSelect" placement="bottom-end">
            <button class="user-chip" aria-label="用户菜单">
              <span class="user-avatar">{{ auth.userName?.[0]?.toUpperCase() || 'A' }}</span>
              <span class="user-name">{{ auth.userName || 'Admin' }}</span>
            </button>
          </n-dropdown>
        </div>
      </header>

      <main class="media-content">
        <div class="media-page-inner">
          <router-view v-slot="{ Component }">
            <transition name="fade-slide" mode="out-in">
              <suspense>
                <component :is="Component" />
              </suspense>
            </transition>
          </router-view>
        </div>
      </main>
    </div>
    <theme-drawer v-model:show="themeOpen" :hide-color-mode="true" />
  </div>
</template>

<style scoped>
.media-shell {
  min-height: 100vh;
  display: flex;
  background: var(--app-bg);
  color: var(--app-text);
}

.media-shell-transparent {
  background: var(--app-bg);
}

/* 前台强制 cinematic 深色,背景由 .cinematic token 驱动 */
.media-shell.cinematic {
  background: var(--app-bg);
}

.media-main {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  position: relative;
}

.page-backdrop {
  position: fixed;
  inset: 0;
  z-index: 0;
  background-size: cover;
  background-position: center top;
  background-repeat: no-repeat;
  opacity: 0.18;
  filter: blur(36px) saturate(1.05);
  transform: scale(1.08);
  pointer-events: none;
}

/* ============== Topbar ============== */
.media-topbar {
  position: sticky;
  top: 0;
  z-index: 20;
  height: 68px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 0 32px;
  background: rgba(14, 14, 14, 0.92);
  backdrop-filter: blur(24px) saturate(1.4);
  -webkit-backdrop-filter: blur(24px) saturate(1.4);
  border-bottom: 0;
  transition: background 0.3s ease, backdrop-filter 0.3s ease;
}

.media-topbar-transparent {
  background: rgba(14, 14, 14, 0.4);
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 28px;
  min-width: 0;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* ---- Brand mark ---- */
.brand-mark {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-weight: 900;
  font-size: 22px;
  line-height: 1;
  letter-spacing: -0.03em;
  color: var(--app-accent-red, #e50914);
  text-decoration: none;
  text-transform: uppercase;
}
.brand-mark__icon {
  width: 24px;
  height: 24px;
  display: block;
  flex-shrink: 0;
}
.brand-mark__text {
  display: block;
}
.brand-mark:hover {
  filter: brightness(1.15);
}

/* ---- Nav links ---- */
.topbar-nav {
  display: flex;
  align-items: center;
  gap: 28px;
}

.nav-link {
  position: relative;
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-weight: 700;
  font-size: 14px;
  letter-spacing: -0.005em;
  color: rgba(255, 255, 255, 0.55);
  text-decoration: none;
  padding: 10px 2px;
  transition: color 0.2s ease;
}
.nav-link:hover {
  color: rgba(255, 255, 255, 0.9);
}
.nav-link.active {
  color: #fff;
}
.nav-link.active::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 2px;
  height: 2px;
  border-radius: 2px;
  background: var(--app-accent-red, #e50914);
}

/* ---- Search form ---- */
.search-form {
  display: flex;
}

.topbar-search {
  width: 260px;
}

.topbar-search :deep(.n-input) {
  background: rgba(255, 255, 255, 0.06);
  border: 0;
  border-radius: 999px;
}
.topbar-search :deep(.n-input:hover),
.topbar-search :deep(.n-input--focus) {
  background: rgba(255, 255, 255, 0.12);
}
.topbar-search :deep(.n-input__border),
.topbar-search :deep(.n-input__state-border) {
  border: 0 !important;
}

.search-icon-only {
  display: none;
}

/* ---- Icon button & user chip ---- */
.icon-button {
  width: 36px;
  height: 36px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 999px;
  color: rgba(255, 255, 255, 0.82);
  background: rgba(255, 255, 255, 0.06);
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease;
}
.icon-button:hover {
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
}

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 14px 4px 4px;
  border: 0;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.92);
  cursor: pointer;
  transition: background 0.2s ease;
}
.user-chip:hover {
  background: rgba(255, 255, 255, 0.14);
}

.user-avatar {
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--app-primary);
  color: #fff;
  font-size: 13px;
  font-weight: 700;
}

.user-name {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.005em;
}

/* ============== Main content ============== */
.media-content {
  position: relative;
  z-index: 1;
  flex: 1;
}

.media-page-inner {
  padding: 0 24px 32px;
}

.backdrop-fade-enter-active,
.backdrop-fade-leave-active {
  transition: opacity 0.25s ease;
}

.backdrop-fade-enter-from,
.backdrop-fade-leave-to {
  opacity: 0;
}

.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

/* ============== Responsive ============== */
@media (max-width: 959px) {
  .media-topbar {
    padding: 0 20px;
    gap: 16px;
  }
  .topbar-left {
    gap: 18px;
  }
  .topbar-nav {
    gap: 18px;
  }
  .topbar-search {
    width: 200px;
  }
  .media-page-inner {
    padding: 0 16px 24px;
  }
}

@media (max-width: 719px) {
  .topbar-nav {
    display: none;
  }
}

@media (max-width: 599px) {
  .media-topbar {
    padding: 0 14px;
    gap: 10px;
    height: 60px;
  }
  .brand-mark {
    font-size: 19px;
  }
  .brand-mark__icon {
    width: 20px;
    height: 20px;
  }
  .search-form {
    display: none;
  }
  .search-icon-only {
    display: inline-flex;
  }
  .user-name {
    display: none;
  }
}
</style>
