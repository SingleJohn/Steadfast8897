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
import {
  MenuOutline,
  LogOutOutline,
  ColorPaletteOutline,
  HomeOutline,
  ChevronForwardOutline,
} from '@vicons/ionicons5'

import SystemMetricsPill from '@/components/SystemMetricsPill.vue'
import ThemeDrawer from '@/components/ThemeDrawer.vue'
import { AppIcons, type IconComponent } from '@/icons/appIcons'
import { useBranding } from '@/composables/useBranding'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { logout as apiLogout } from '@/api/client'

const router = useRouter()
const route = useRoute()
const ui = useUiStore()
const auth = useAuthStore()
const branding = useBranding()
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
  sectionIcon?: AppIconKey
  sectionOrder?: number
  sectionSingle?: boolean
  subSection?: string
  subSectionLabel?: string
  subSectionOrder?: number
  order?: number
  requiresAdmin?: boolean
}

type NavItem = {
  key: string
  label: string
  icon?: AppIconKey
  order: number
  subSection?: string
  subSectionLabel?: string
  subSectionOrder?: number
}

type NavSection = {
  key: string
  label: string
  icon?: AppIconKey
  single: boolean
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

function renderIcon(icon?: IconComponent): MenuOption['icon'] | undefined {
  if (!icon) return undefined
  return () => h(NIcon, { size: 18 }, { default: () => h(icon) })
}

function getNavMeta(record: RouteRecordNormalized): NavMeta {
  return (record.meta || {}) as NavMeta
}

/**
 * 生成带二级菜单的 menuOptions：
 *   - sectionSingle=true 的模块直接作为一级菜单项（如"概览"）
 *   - 其它 section 打包成 submenu，children 为该 section 的菜单项
 */
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
        icon: meta.sectionIcon,
        single: !!meta.sectionSingle,
        order: Number(meta.sectionOrder ?? 99),
        items: [],
      })
    }

    sectionMap.get(meta.section)!.items.push({
      key: record.name,
      label: meta.navLabel,
      icon: meta.icon,
      order: Number(meta.order ?? 99),
      subSection: meta.subSection,
      subSectionLabel: meta.subSectionLabel,
      subSectionOrder: meta.subSectionOrder,
    })
  }

  const sorted = Array.from(sectionMap.values()).sort((a, b) => a.order - b.order)
  const options: MenuOption[] = []

  for (const section of sorted) {
    const items = section.items.sort((a, b) => a.order - b.order)

    if (section.single && items.length === 1) {
      // 单项模块直接挂为一级菜单项
      const only = items[0]
      options.push({
        key: only.key,
        label: only.label,
        icon: renderIcon(section.icon ? AppIcons[section.icon] : only.icon ? AppIcons[only.icon] : undefined),
      })
      continue
    }

    const hasSubGroups = items.some((it) => !!it.subSection)
    let children: MenuOption[]
    if (hasSubGroups) {
      // 把 items 按 subSection 分桶，保留桶内 order 排序；输出 naive-ui 的 group 菜单项
      const buckets = new Map<string, { label: string; order: number; items: NavItem[] }>()
      for (const it of items) {
        const key = it.subSection || '__ungrouped'
        if (!buckets.has(key)) {
          buckets.set(key, {
            label: it.subSectionLabel || '',
            order: Number(it.subSectionOrder ?? 99),
            items: [],
          })
        }
        buckets.get(key)!.items.push(it)
      }
      const groups = Array.from(buckets.entries()).sort((a, b) => a[1].order - b[1].order)
      children = groups.map(([key, bucket]) => ({
        type: 'group',
        key: `group:${section.key}:${key}`,
        label: bucket.label,
        children: bucket.items.map((item) => ({
          key: item.key,
          label: item.label,
          icon: renderIcon(item.icon ? AppIcons[item.icon] : undefined),
        })),
      }))
    } else {
      children = items.map((item) => ({
        key: item.key,
        label: item.label,
        icon: renderIcon(item.icon ? AppIcons[item.icon] : undefined),
      }))
    }

    options.push({
      key: `section:${section.key}`,
      label: section.label,
      icon: renderIcon(section.icon ? AppIcons[section.icon] : undefined),
      children,
    })
  }

  return options
})

const selectedKey = computed(() => {
  return typeof route.name === 'string' ? route.name : 'admin_overview'
})

const currentSection = computed<string | undefined>(() => (route.meta as NavMeta)?.section)
const currentSectionLabel = computed<string | undefined>(() => (route.meta as NavMeta)?.sectionLabel)
const currentSubSectionLabel = computed<string | undefined>(() => (route.meta as NavMeta)?.subSectionLabel)
const currentTitle = computed(() => {
  const title = route.meta?.title
  return typeof title === 'string' && title ? title : '管理后台'
})
// 面包屑中的"模块名"——当 section 是单项（如概览）或没有 sectionLabel 时，不展示二级
const breadcrumbShowSection = computed(() => {
  const meta = route.meta as NavMeta
  if (!meta.sectionLabel) return false
  if (meta.sectionSingle) return false
  return meta.sectionLabel !== meta.title
})
const breadcrumbShowSubSection = computed(() => {
  const meta = route.meta as NavMeta
  if (!meta.subSectionLabel) return false
  return meta.subSectionLabel !== meta.title
})

// 展开状态：默认展开当前 section；用户手动展开/收起后保留偏好
const expandedKeys = ref<string[]>([])

function syncExpandedFromRoute() {
  const section = currentSection.value
  if (!section) return
  const key = `section:${section}`
  if (!expandedKeys.value.includes(key)) {
    expandedKeys.value = [...expandedKeys.value, key]
  }
}
syncExpandedFromRoute()
watch(currentSection, syncExpandedFromRoute)

async function onMenuSelect(key: string) {
  if (key.startsWith('section:')) return
  mobileMenuOpen.value = false
  await router.push({ name: key })
}

watch(() => route.path, () => {
  mobileMenuOpen.value = false
  void branding.loadBranding()
}, { immediate: true })
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
          <img v-if="branding.iconUrl.value" :src="branding.iconUrl.value" class="brand-logo-image" alt="" />
          <n-icon v-else :size="20"><component :is="AppIcons.overview" /></n-icon>
        </div>
        <transition name="fade">
          <span v-show="!ui.siderCollapsed" class="brand-text">{{ branding.serverName.value || 'FYMS' }} 管理</span>
        </transition>
      </div>

      <n-menu
        :value="selectedKey"
        :expanded-keys="expandedKeys"
        :options="menuOptions"
        :collapsed="ui.siderCollapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :icon-size="18"
        :indent="18"
        :root-indent="16"
        @update:value="onMenuSelect"
        @update:expanded-keys="expandedKeys = $event"
      />
    </n-layout-sider>

    <n-drawer v-else v-model:show="mobileMenuOpen" placement="left" :width="260">
      <n-drawer-content title="管理菜单" body-content-style="padding: 0">
        <n-menu
          :value="selectedKey"
          :expanded-keys="expandedKeys"
          :options="menuOptions"
          :icon-size="18"
          :indent="18"
          :root-indent="16"
          @update:value="onMenuSelect"
          @update:expanded-keys="expandedKeys = $event"
        />
      </n-drawer-content>
    </n-drawer>

    <n-layout class="main-layout">
      <n-layout-header class="topbar">
        <n-space align="center" justify="space-between" style="height: 100%">
          <n-space align="center" :size="10">
            <n-button v-if="isMobile" text style="font-size: 24px" @click="mobileMenuOpen = true">
              <n-icon><MenuOutline /></n-icon>
            </n-button>
            <div class="breadcrumb">
              <span v-if="breadcrumbShowSection" class="breadcrumb-section">{{ currentSectionLabel }}</span>
              <span v-if="breadcrumbShowSection" class="breadcrumb-sep">
                <n-icon :size="14"><ChevronForwardOutline /></n-icon>
              </span>
              <span v-if="breadcrumbShowSubSection" class="breadcrumb-section">{{ currentSubSectionLabel }}</span>
              <span v-if="breadcrumbShowSubSection" class="breadcrumb-sep">
                <n-icon :size="14"><ChevronForwardOutline /></n-icon>
              </span>
              <span class="breadcrumb-current">{{ currentTitle }}</span>
            </div>
          </n-space>

          <n-space align="center" :size="10">
            <system-metrics-pill />
            <n-dropdown :options="userMenuOptions" @select="handleUserMenuSelect" placement="bottom-end">
              <button class="user-chip" aria-label="用户菜单">
                <span class="user-avatar">{{ auth.userName?.[0]?.toUpperCase() || 'A' }}</span>
                <span class="user-name">{{ auth.userName || 'Admin' }}</span>
              </button>
            </n-dropdown>
          </n-space>
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
  /* 弱化的单层径向渐变，取代原来的四层叠加 + 圆点网格 */
  background-image:
    radial-gradient(1200px circle at 10% -10%, rgba(var(--app-primary-rgb), 0.08), transparent 55%),
    radial-gradient(900px circle at 90% 100%, rgba(59, 130, 246, 0.06), transparent 55%);
  background-attachment: fixed;
}

:global(.app-dark) .app-shell {
  background-image:
    radial-gradient(1200px circle at 10% -10%, rgba(var(--app-primary-rgb), 0.10), transparent 55%),
    radial-gradient(900px circle at 90% 100%, rgba(59, 130, 246, 0.05), transparent 55%);
}

.app-sider {
  background: var(--app-surface-1);
  backdrop-filter: blur(var(--app-glass-blur)) saturate(1.2);
  -webkit-backdrop-filter: blur(var(--app-glass-blur)) saturate(1.2);
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

.brand-logo-image {
  width: 20px;
  height: 20px;
  display: block;
}

.brand-text {
  font-weight: 700;
  font-size: 16px;
  letter-spacing: -0.02em;
  white-space: nowrap;
}

/* ---------- Menu Customization ---------- */
.app-shell :deep(.n-menu) {
  --n-item-height: 38px;
  padding: 8px 0;
}

/* 所有菜单项（含 submenu 标题栏）通用外观 */
.app-shell :deep(.n-menu .n-menu-item-content) {
  padding-left: 14px !important;
  margin: 2px 8px;
  border-radius: 8px;
  min-height: var(--n-item-height);
  box-shadow: none !important;
  background-color: transparent;
  transition: background-color 0.18s ease, color 0.18s ease;
}
.app-shell :deep(.n-menu .n-menu-item-content::before) { display: none !important; }

.app-shell :deep(.n-menu .n-menu-item-content:hover):not(.n-menu-item-content--selected) {
  background-color: var(--app-primary-soft) !important;
  color: var(--app-text) !important;
}
.app-shell :deep(.n-menu .n-menu-item-content:hover .n-menu-item-content-header),
.app-shell :deep(.n-menu .n-menu-item-content:hover .n-icon) {
  color: var(--app-text) !important;
}

/* 叶子节点选中（仅选中的真·子菜单项才高亮为主色） */
.app-shell :deep(.n-menu .n-menu-item-content--selected) {
  background-color: var(--app-primary) !important;
  box-shadow: 0 4px 12px -2px rgba(var(--app-primary-rgb), 0.35) !important;
}
.app-shell :deep(.n-menu .n-menu-item-content--selected .n-menu-item-content-header),
.app-shell :deep(.n-menu .n-menu-item-content--selected .n-icon) {
  color: #ffffff !important;
}

/* submenu 父节点"子项被选中"状态：浅色底色 + 左侧色条，不抢主色 */
.app-shell :deep(.n-submenu.n-submenu--child-active > .n-menu-item) {
  background-color: transparent !important;
}
.app-shell :deep(.n-submenu.n-submenu--child-active > .n-menu-item .n-menu-item-content) {
  background-color: transparent !important;
  color: var(--app-text) !important;
  font-weight: 600;
}
.app-shell :deep(.n-submenu.n-submenu--child-active > .n-menu-item .n-menu-item-content .n-icon) {
  color: var(--app-primary) !important;
}

/* 图标与文字间距 */
.app-shell :deep(.n-menu .n-menu-item-content__icon) {
  margin-right: 10px !important;
}
.app-shell :deep(.n-menu .n-menu-item-content-header) { font-size: 13px; }

/* 展开箭头：更轻 */
.app-shell :deep(.n-menu .n-submenu .n-menu-item-content__arrow) {
  opacity: 0.55;
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.app-shell :deep(.n-menu .n-submenu .n-menu-item-content:hover .n-menu-item-content__arrow) {
  opacity: 1;
}

/* 二级菜单项（submenu 内子项）活动态：左侧色条 + 浅底 */
.app-shell :deep(.n-submenu-children .n-menu-item-content) {
  position: relative;
  padding-left: 16px !important;
  margin: 1px 8px;
}
.app-shell :deep(.n-submenu-children .n-menu-item-content--selected) {
  background-color: var(--app-primary-soft) !important;
  box-shadow: none !important;
}
.app-shell :deep(.n-submenu-children .n-menu-item-content--selected .n-menu-item-content-header),
.app-shell :deep(.n-submenu-children .n-menu-item-content--selected .n-icon) {
  color: var(--app-primary) !important;
  font-weight: 600;
}
.app-shell :deep(.n-submenu-children .n-menu-item-content--selected::after) {
  content: '';
  position: absolute;
  left: 4px;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 2px;
  background: var(--app-primary);
}

/* ---------- Collapsed State ---------- */
.app-shell :deep(.n-menu--collapsed .n-menu-item-content) {
  margin: 2px auto;
  padding: 0 !important;
  width: var(--n-item-height);
  min-width: var(--n-item-height);
  min-height: var(--n-item-height);
  display: flex; align-items: center; justify-content: center;
}
.app-shell :deep(.n-menu--collapsed .n-menu-item-content__icon) {
  margin-right: 0 !important;
  width: 100%;
  display: flex; align-items: center; justify-content: center;
}
.app-shell :deep(.n-menu--collapsed .n-menu-item-content-header),
.app-shell :deep(.n-menu--collapsed .n-menu-item-content__arrow) {
  width: 0 !important; min-width: 0 !important;
  margin: 0 !important; padding: 0 !important;
  overflow: hidden !important; opacity: 0 !important;
}

/* ---------- Main Layout ---------- */
.main-layout { background: transparent; display: flex; flex-direction: column; }
.app-shell :deep(.n-layout) { background: transparent !important; }
.app-shell :deep(.n-layout-scroll-container) { background: transparent !important; }

.topbar {
  height: 64px;
  padding: 0 24px;
  background: var(--app-surface-1);
  backdrop-filter: blur(var(--app-glass-blur)) saturate(1.15);
  -webkit-backdrop-filter: blur(var(--app-glass-blur)) saturate(1.15);
  border-bottom: 1px solid var(--app-border);
  position: sticky;
  top: 0;
  z-index: 40;
}

.breadcrumb {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--app-text-muted);
}
.breadcrumb-section { font-weight: 500; }
.breadcrumb-sep { display: inline-flex; opacity: 0.55; }
.breadcrumb-current {
  font-size: 16px;
  font-weight: 600;
  color: var(--app-text);
}

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
  background: var(--app-primary-soft);
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
.content-inner { max-width: 1280px; margin: 0 auto; padding: 24px; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.fade-slide-enter-active, .fade-slide-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.fade-slide-enter-from { opacity: 0; transform: translateY(8px); }
.fade-slide-leave-to { opacity: 0; transform: translateY(-8px); }
</style>
