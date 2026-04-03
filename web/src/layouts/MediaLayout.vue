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
import ThemeDrawer from '@/components/ThemeDrawer.vue'
import { logout as apiLogout } from '@/api/client'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const message = useMessage()

const searchTerm = ref('')
const scrolled = ref(false)
const backdropUrl = ref('')
const themeOpen = ref(false)

function setBackdrop(url: string) {
  backdropUrl.value = url
}

provide('setBackdrop', setBackdrop)

watch(() => route.fullPath, () => {
  backdropUrl.value = ''
  window.scrollTo({ top: 0, behavior: 'auto' })
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

function handleScroll() {
  scrolled.value = window.scrollY > 10
}

function handleSearch(event: Event) {
  event.preventDefault()
  const q = searchTerm.value.trim()
  if (!q) return
  router.push({ name: 'search', query: { q } })
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

onMounted(() => {
  window.addEventListener('scroll', handleScroll, { passive: true })
  handleScroll()
})

onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll)
})
</script>

<template>
  <div class="media-shell" :class="{ 'media-shell-transparent': transparentShell }">
    <transition name="backdrop-fade">
      <div v-if="showBackdrop" class="page-backdrop" :style="{ backgroundImage: `url(${backdropUrl})` }" />
    </transition>

    <div class="media-main">
      <header class="media-topbar" :class="{ 'media-topbar-transparent': transparentShell }">
        <div class="topbar-left">
          <button class="icon-button" @click="goBack" aria-label="返回">
            <n-icon :size="20"><ArrowBackOutline /></n-icon>
          </button>
        </div>

        <div class="topbar-center">
          <form class="search-form" @submit="handleSearch">
            <n-input
              v-model:value="searchTerm"
              clearable
              size="small"
              class="topbar-search"
              placeholder="搜索媒体..."
            >
              <template #prefix>
                <n-icon :size="16"><SearchOutline /></n-icon>
              </template>
            </n-input>
          </form>
        </div>

        <div class="topbar-right">
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
              <component :is="Component" />
            </transition>
          </router-view>
        </div>
      </main>
    </div>
    <theme-drawer v-model:show="themeOpen" />
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

.media-topbar {
  position: sticky;
  top: 0;
  z-index: 20;
  height: 56px;
  display: grid;
  grid-template-columns: auto minmax(260px, 520px) 1fr;
  align-items: center;
  gap: 16px;
  padding: 0 24px;
  background: var(--app-surface-1);
  backdrop-filter: blur(var(--app-glass-blur));
  -webkit-backdrop-filter: blur(var(--app-glass-blur));
  border-bottom: 1px solid var(--app-border);
}

.media-topbar-transparent {
  background: transparent;
  border-bottom-color: transparent;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}

.topbar-left,
.topbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.topbar-right {
  justify-content: flex-end;
}

.topbar-center {
  min-width: 0;
}

.search-form {
  display: flex;
}

.topbar-search {
  width: 100%;
}

.topbar-search :deep(.n-input) {
  background: var(--app-surface-2);
  border-radius: var(--app-radius);
}

.icon-button {
  width: 36px;
  height: 36px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: var(--app-radius);
  color: var(--app-text);
  background: var(--app-surface-2);
  cursor: pointer;
}

.icon-button:hover {
  background: rgba(var(--app-primary-rgb), 0.12);
}

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px 4px 4px;
  border: 0;
  border-radius: 999px;
  background: var(--app-surface-2);
  color: var(--app-text);
  cursor: pointer;
  transition: background 0.15s;
}

.user-chip:hover {
  background: rgba(var(--app-primary-rgb), 0.12);
}

.user-avatar {
  width: 28px;
  height: 28px;
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
}

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

:global(.app-dark) .media-shell {
  background:
    radial-gradient(circle at top left, rgba(100, 181, 246, 0.12), transparent 24%),
    linear-gradient(180deg, rgba(16, 24, 40, 0.98), rgba(12, 18, 32, 0.98));
}

:global(.app-dark) .media-topbar {
  background: rgba(8, 15, 28, 0.92);
  border-bottom-color: rgba(255, 255, 255, 0.06);
}

:global(.app-dark) .icon-button,
:global(.app-dark) .user-chip,
:global(.app-dark) .topbar-search :deep(.n-input) {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.9);
}

@media (max-width: 959px) {
  .media-topbar {
    grid-template-columns: auto 1fr auto;
    padding: 0 16px;
  }

  .media-page-inner {
    padding: 0 16px 24px;
  }
}

@media (max-width: 599px) {
  .media-topbar {
    grid-template-columns: auto 1fr auto;
    gap: 10px;
  }

  .user-name {
    display: none;
  }
}
</style>
