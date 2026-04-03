<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import {
  NButton,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  useMessage,
  type MenuOption,
} from 'naive-ui'
import { computed, h, ref, watch } from 'vue'
import { useRoute, useRouter, type RouteRecordNormalized } from 'vue-router'
import { MenuOutline, LogOutOutline, ArrowBackOutline, ColorPaletteOutline, HomeOutline } from '@vicons/ionicons5'

import ThemeDrawer from '@/components/ThemeDrawer.vue'
import { AppIcons, type IconComponent } from '@/icons/appIcons'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { logout as apiLogout } from '@/api/client'

const router = useRouter()
const route = useRoute()
const ui = useUiStore()
const auth = useAuthStore()
const message = useMessage()

const isMobile = useMediaQuery('(max-width: 768px)')
const mobileMenuOpen = ref(false)
const themeOpen = ref(false)

type AppIconKey = keyof typeof AppIcons

type NavMeta = {
  title?: string
  navLabel?: string
  icon?: AppIconKey
  section?: string
  sectionLabel?: string
  sectionOrder?: number
  order?: number
  requiresAdmin?: boolean
}

type NavItem = {
  key: string
  label: string
  icon?: AppIconKey
  order: number
}

type NavSection = {
  key: string
  label: string
  order: number
  items: NavItem[]
}

async function handleLogout() {
  try { await apiLogout() } catch { /* ignore */ }
  auth.logout()
  message.success('已退出登录')
  router.push('/login')
}

const userMenuOptions = computed(() => [
  { label: '外观设置', key: 'theme', icon: () => h(NIcon, { size: 18 }, { default: () => h(ColorPaletteOutline) }) },
  { label: '返回首页', key: 'home', icon: () => h(NIcon, { size: 18 }, { default: () => h(HomeOutline) }) },
  { type: 'divider', key: 'd1' },
  { label: '退出登录', key: 'logout', icon: () => h(NIcon, { size: 18 }, { default: () => h(LogOutOutline) }) },
])

function handleUserMenuSelect(key: string) {
  if (key === 'theme') themeOpen.value = true
  if (key === 'home') router.push({ name: 'home' })
  if (key === 'logout') void handleLogout()
}

function menuIcon(icon?: IconComponent): MenuOption['icon'] | undefined {
  if (!icon) return undefined
  return () => h(NIcon, { size: 20 }, { default: () => h(icon) })
}

function getNavMeta(record: RouteRecordNormalized): NavMeta {
  return (record.meta || {}) as NavMeta
}

const menuOptions = computed<MenuOption[]>(() => {
  const sectionMap = new Map<string, NavSection>()

  for (const record of router.getRoutes()) {
    if (typeof record.name !== 'string') continue
    const meta = getNavMeta(record)
    if (!meta.section || !meta.navLabel) continue
    if (meta.requiresAdmin && !auth.isAdmin) continue

    if (!sectionMap.has(meta.section)) {
      sectionMap.set(meta.section, {
        key: meta.section,
        label: meta.sectionLabel || meta.section,
        order: Number(meta.sectionOrder ?? 99),
        items: [],
      })
    }

    sectionMap.get(meta.section)!.items.push({
      key: record.name,
      label: meta.navLabel,
      icon: meta.icon,
      order: Number(meta.order ?? 99),
    })
  }

  const options: MenuOption[] = []
  const sorted = Array.from(sectionMap.values()).sort((a, b) => a.order - b.order)

  for (const section of sorted) {
    const children: MenuOption[] = section.items
      .sort((a, b) => a.order - b.order)
      .map((item) => ({
        key: item.key,
        label: item.label,
        icon: menuIcon(item.icon ? AppIcons[item.icon] : undefined),
      }))

    if (section.key === 'admin') {
      const toolIndex = children.findIndex((item) => item.key === 'tools')
      if (toolIndex > 0) {
        children.splice(toolIndex, 0, { type: 'divider', key: 'divider:admin:tools' })
      }
    }

    options.push({
      key: `section:${section.key}`,
      label: section.label,
      type: 'group',
      children,
    })
  }

  return options
})

const selectedKey = computed(() => {
  return typeof route.name === 'string' ? route.name : 'admin_overview'
})

const currentTitle = computed(() => {
  const title = route.meta?.title
  return typeof title === 'string' && title ? title : '管理后台'
})

async function onMenuSelect(key: string) {
  if (key.startsWith('section:')) return
  mobileMenuOpen.value = false
  await router.push({ name: key })
}

watch(() => route.path, () => { mobileMenuOpen.value = false })
</script>

<template>
  <n-layout class="app-shell" has-sider>
    <n-layout-sider
      v-if="!isMobile"
      bordered
      collapse-mode="width"
      :collapsed="ui.siderCollapsed"
      :collapsed-width="64"
      :width="240"
      show-trigger="bar"
      @update:collapsed="ui.siderCollapsed = $event"
      class="app-sider"
    >
      <div class="brand" @click="router.push({ name: 'admin_overview' })">
        <div class="brand-logo">
          <n-icon :size="20"><component :is="AppIcons.overview" /></n-icon>
        </div>
        <transition name="fade">
          <span v-show="!ui.siderCollapsed" class="brand-text">FYMS 管理</span>
        </transition>
      </div>

      <n-menu
        :value="selectedKey"
        :options="menuOptions"
        :collapsed="ui.siderCollapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :icon-size="20"
        @update:value="onMenuSelect"
      />
    </n-layout-sider>

    <n-drawer v-else v-model:show="mobileMenuOpen" placement="left" :width="260">
      <n-drawer-content title="管理菜单" body-content-style="padding: 0">
        <n-menu :value="selectedKey" :options="menuOptions" @update:value="onMenuSelect" />
      </n-drawer-content>
    </n-drawer>

    <n-layout class="main-layout">
      <n-layout-header class="topbar">
        <n-space align="center" justify="space-between" style="height: 100%">
          <n-space align="center" :size="12">
            <n-button v-if="isMobile" text style="font-size: 24px" @click="mobileMenuOpen = true">
              <n-icon><MenuOutline /></n-icon>
            </n-button>
            <div class="page-title">{{ currentTitle }}</div>
          </n-space>

          <n-dropdown :options="userMenuOptions" @select="handleUserMenuSelect" placement="bottom-end">
            <button class="user-chip" aria-label="用户菜单">
              <span class="user-avatar">{{ auth.userName?.[0]?.toUpperCase() || 'A' }}</span>
              <span class="user-name">{{ auth.userName || 'Admin' }}</span>
            </button>
          </n-dropdown>
        </n-space>
      </n-layout-header>

      <n-layout-content class="content" :native-scrollbar="false">
        <div class="content-inner">
          <router-view v-slot="{ Component }">
            <transition name="fade-slide" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </div>
      </n-layout-content>
    </n-layout>

    <theme-drawer v-model:show="themeOpen" />
  </n-layout>
</template>

<style scoped>
.app-shell {
  height: 100vh;
  background-color: var(--app-bg);
  background-image:
    radial-gradient(circle at center, var(--c-slate-300) 1px, transparent 1px),
    radial-gradient(1200px circle at 15% -10%, rgba(var(--app-primary-rgb), 0.15), transparent 55%),
    radial-gradient(800px circle at 85% 100%, rgba(59, 130, 246, 0.12), transparent 55%),
    radial-gradient(1000px circle at 50% 50%, rgba(245, 158, 11, 0.06), transparent 60%);
  background-size: 24px 24px, 100% 100%, 100% 100%, 100% 100%;
  background-position: 0 0, 0 0, 0 0, 0 0;
  background-attachment: fixed;
}

:global(.app-dark) .app-shell {
  background-image:
    radial-gradient(circle at center, var(--c-slate-700) 1px, transparent 1px),
    radial-gradient(1200px circle at 15% -10%, rgba(var(--app-primary-rgb), 0.12), transparent 55%),
    radial-gradient(800px circle at 85% 100%, rgba(59, 130, 246, 0.10), transparent 55%),
    radial-gradient(1000px circle at 50% 50%, rgba(245, 158, 11, 0.05), transparent 60%);
}

.app-sider {
  background: var(--app-surface-1);
  backdrop-filter: blur(var(--app-glass-blur)) saturate(1.25);
  -webkit-backdrop-filter: blur(var(--app-glass-blur)) saturate(1.25);
  border-right: 1px solid var(--app-border);
  z-index: 50;
}

.brand {
  height: 64px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  gap: 12px;
  overflow: hidden;
  cursor: pointer;
  transition: background-color 0.2s ease;
  user-select: none;
}
.brand:hover { background-color: rgba(0, 0, 0, 0.03); }
:global(.app-dark) .brand:hover { background-color: rgba(255, 255, 255, 0.05); }
.brand:active { transform: scale(0.98); }

.brand-logo {
  width: 32px;
  height: 32px;
  background: linear-gradient(135deg, var(--app-primary), #0ea5e9);
  color: white;
  border-radius: 8px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  box-shadow: 0 4px 6px -1px rgba(var(--app-primary-rgb), 0.3);
}

.brand-text {
  font-weight: 700;
  font-size: 16px;
  letter-spacing: -0.02em;
  white-space: nowrap;
}

/* Menu Customization */
.app-shell :deep(.n-menu) { --n-item-height: 36px; }
.app-shell :deep(.n-menu .n-menu-item-content) {
  padding-left: 10px !important;
  margin: 2px 10px;
  border-radius: 7px;
  min-height: var(--n-item-height);
  box-shadow: none !important;
  background-color: transparent;
  transition: background-color 0.2s ease, box-shadow 0.2s ease, color 0.2s ease;
}
.app-shell :deep(.n-menu .n-menu-item-content::before) { display: none !important; }

.app-shell :deep(.n-menu .n-menu-item-content:hover):not(.n-menu-item-content--selected) {
  background-color: var(--c-slate-200) !important;
  box-shadow: none !important;
}
.app-dark :deep(.n-menu .n-menu-item-content:hover):not(.n-menu-item-content--selected) {
  background-color: var(--c-slate-800) !important;
  box-shadow: none !important;
}

.app-shell :deep(.n-menu .n-menu-item-content--selected) {
  background-color: var(--app-primary) !important;
  box-shadow: 0 4px 12px -2px rgba(var(--app-primary-rgb), 0.4) !important;
}
.app-shell :deep(.n-menu .n-menu-item-content--selected .n-menu-item-content-header),
.app-shell :deep(.n-menu .n-menu-item-content--selected .n-icon) {
  color: #ffffff !important;
}
.app-shell :deep(.n-menu .n-menu-item-content__icon) { margin-right: 10px !important; }

.app-shell :deep(.n-menu .n-menu-item-group-title) {
  margin: 0; padding: 0; height: 1px; line-height: 0; font-size: 0; color: transparent;
  background: linear-gradient(to right, transparent 0%, var(--app-border) 10%, var(--app-border) 90%, transparent 100%);
  opacity: 0.75; pointer-events: none; user-select: none;
}
.app-shell :deep(.n-menu .n-menu-item-content-header) { font-size: 13px; }

/* Collapsed State */
.app-shell :deep(.n-menu--collapsed .n-menu-item-content) {
  margin: 2px auto; padding: 0 !important;
  width: var(--n-item-height); min-width: var(--n-item-height); min-height: var(--n-item-height);
  display: flex; align-items: center; justify-content: center;
}
.app-shell :deep(.n-menu--collapsed .n-menu-item-content__icon) { margin-right: 0 !important; width: 100%; display: flex; align-items: center; justify-content: center; }
.app-shell :deep(.n-menu--collapsed .n-menu-item-content-header) { width: 0 !important; min-width: 0 !important; margin: 0 !important; padding: 0 !important; overflow: hidden !important; opacity: 0 !important; }
.app-shell :deep(.n-menu--collapsed .n-menu-item-content__arrow) { width: 0 !important; min-width: 0 !important; margin: 0 !important; overflow: hidden !important; opacity: 0 !important; }
.app-shell :deep(.n-menu--collapsed .n-menu-item-group-title) { display: none !important; }

/* Main Layout */
.main-layout { background: transparent; display: flex; flex-direction: column; }
.app-shell :deep(.n-layout) { background: transparent !important; }
.app-shell :deep(.n-layout-scroll-container) { background: transparent !important; }

.topbar {
  height: 64px;
  padding: 0 24px;
  background: var(--app-surface-1);
  backdrop-filter: blur(var(--app-glass-blur)) saturate(1.25);
  -webkit-backdrop-filter: blur(var(--app-glass-blur)) saturate(1.25);
  border-bottom: 1px solid var(--app-border);
  position: sticky;
  top: 0;
  z-index: 40;
}

.page-title { font-size: 16px; font-weight: 600; color: var(--app-text); }

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px 4px 4px;
  border: 0;
  border-radius: 999px;
  background: var(--app-surface-2, rgba(128,128,128,0.08));
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

.content { flex: 1; }
.content-inner { max-width: 1200px; margin: 0 auto; padding: 24px; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.fade-slide-enter-active, .fade-slide-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.fade-slide-enter-from { opacity: 0; transform: translateY(8px); }
.fade-slide-leave-to { opacity: 0; transform: translateY(-8px); }
</style>
