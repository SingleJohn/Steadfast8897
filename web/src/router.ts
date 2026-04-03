import { createRouter, createWebHashHistory } from 'vue-router'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('./pages/LoginPage.vue'),
      meta: { title: '登录' },
    },
    {
      path: '/play/:itemId',
      name: 'player',
      component: () => import('./pages/PlayerPage.vue'),
      meta: { requiresAuth: true, title: '播放' },
    },

    // Media browsing layout
    {
      path: '/',
      component: () => import('./layouts/MediaLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          name: 'home',
          component: () => import('./pages/HomePage.vue'),
          meta: { title: '首页' },
        },
        {
          path: 'search',
          name: 'search',
          component: () => import('./pages/SearchPage.vue'),
          meta: { title: '搜索' },
        },
        {
          path: 'library/:libraryId',
          name: 'library',
          component: () => import('./pages/LibraryPage.vue'),
          meta: { title: '媒体库' },
        },
        {
          path: 'item/:itemId',
          name: 'item_detail',
          component: () => import('./pages/ItemDetailPage.vue'),
          meta: { title: '详情' },
        },
      ],
    },

    // Admin layout
    {
      path: '/admin',
      component: () => import('./layouts/AdminLayout.vue'),
      meta: { requiresAuth: true },
      redirect: { name: 'admin_overview' },
      children: [
        {
          path: 'overview',
          name: 'admin_overview',
          component: () => import('./pages/OverviewPage.vue'),
          meta: {
            title: '总览',
            navLabel: '总览',
            icon: 'overview',
            section: 'core',
            sectionLabel: '核心',
            sectionOrder: 1,
            order: 1,
          },
        },

        // -- 网关 --
        {
          path: 'emby-sources',
          name: 'emby_sources',
          component: () => import('./pages/EmbySourcesPage.vue'),
          meta: {
            title: 'Emby 源',
            navLabel: 'Emby 源',
            icon: 'emby',
            section: 'gateway',
            sectionLabel: '网关',
            sectionOrder: 2,
            order: 1,
            requiresAdmin: true,
          },
        },
        {
          path: 'path-rules',
          name: 'path_rule_sets',
          component: () => import('./pages/PathRuleSetsPage.vue'),
          meta: {
            title: '路径映射',
            navLabel: '路径映射',
            icon: 'pathMapping',
            section: 'gateway',
            sectionLabel: '网关',
            sectionOrder: 2,
            order: 2,
            requiresAdmin: true,
          },
        },
        {
          path: 'backends',
          name: 'backends',
          component: () => import('./pages/BackendsPage.vue'),
          meta: {
            title: '资源池与后端',
            navLabel: '资源池与后端',
            icon: 'backends',
            section: 'gateway',
            sectionLabel: '网关',
            sectionOrder: 2,
            order: 3,
            requiresAdmin: true,
          },
        },

        // -- 监控 --
        {
          path: 'observability',
          name: 'observability',
          component: () => import('./pages/ObservabilityPage.vue'),
          meta: {
            title: '观测中心',
            navLabel: '观测中心',
            icon: 'observability',
            section: 'monitor',
            sectionLabel: '监控',
            sectionOrder: 3,
            order: 1,
            requiresAdmin: true,
          },
        },

        // -- 管理 --
        {
          path: 'users',
          name: 'users',
          component: () => import('./pages/UserManagementPage.vue'),
          meta: {
            title: '用户管理',
            navLabel: '用户管理',
            icon: 'users',
            section: 'admin',
            sectionLabel: '管理',
            sectionOrder: 4,
            order: 1,
            requiresAdmin: true,
          },
        },
        {
          path: 'libraries',
          name: 'libraries',
          component: () => import('./pages/LibrariesPage.vue'),
          meta: {
            title: '媒体库',
            navLabel: '媒体库',
            icon: 'library',
            section: 'admin',
            sectionLabel: '管理',
            sectionOrder: 4,
            order: 2,
            requiresAdmin: true,
          },
        },
        {
          path: 'metadata',
          name: 'metadata',
          component: () => import('./pages/MetadataPage.vue'),
          meta: {
            title: '元数据',
            navLabel: '元数据',
            icon: 'metadata',
            section: 'admin',
            sectionLabel: '管理',
            sectionOrder: 4,
            order: 3,
            requiresAdmin: true,
          },
        },
        {
          path: 'tools',
          name: 'tools',
          component: () => import('./pages/ToolsPage.vue'),
          meta: {
            title: '工具',
            navLabel: '工具',
            icon: 'tools',
            section: 'admin',
            sectionLabel: '管理',
            sectionOrder: 4,
            order: 4,
            requiresAdmin: true,
          },
        },
        { path: 'apikeys', redirect: '/admin/tools?tab=api-keys' },
        { path: 'library-edit/:libraryId', redirect: '/admin/libraries' },
        { path: 'webhook', redirect: '/admin/tools?tab=webhook' },
        { path: 'api-keys', redirect: '/admin/tools?tab=api-keys' },
        { path: 'backup', redirect: '/admin/tools?tab=backup' },
        { path: 'emby-migrate', redirect: '/admin/tools?tab=emby-migrate' },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach((to) => {
  const token = localStorage.getItem('accessToken')
  if (to.meta.requiresAuth && !token) return '/login'
  if (to.meta.requiresAdmin && localStorage.getItem('isAdmin') !== 'true') return '/'
})

export { router }
export default router
