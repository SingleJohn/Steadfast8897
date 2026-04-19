import { createRouter, createWebHashHistory } from 'vue-router'

/**
 * Admin 后台路由结构（5 个一级模块 × N 个二级菜单）
 *
 *   概览（单项）       →  /admin/overview
 *   媒体内容（submenu）→  /admin/media/{libraries|metadata}
 *   网关（submenu）    →  /admin/gateway/{emby-sources|path-rules|backends}
 *   观测中心（submenu）→  /admin/observability/{traffic|redirect|ip-stats|playback|stats|logs}
 *   系统（submenu）    →  /admin/system/{users|api-keys|webhook|backup|emby-migrate}
 *
 * 菜单由 AdminLayout 根据路由 meta 动态生成：
 *   - section / sectionLabel / sectionOrder / sectionIcon：一级模块归属
 *   - navLabel / icon / order：二级菜单显示
 *   - sectionSingle：模块下只有一项（如"概览"），菜单不包 submenu
 *   - requiresAdmin：权限过滤
 *
 * 观测中心父路由带组件：ObservabilityPage 作为容器持有 useObservability，
 * 通过 provide/inject 把状态共享给所有子路由（source/tag 过滤器不重置、数据不重复拉取）。
 */

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

    // Media browsing layout (用户端，未做改动)
    {
      path: '/',
      component: () => import('./layouts/MediaLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        { path: '', name: 'home', component: () => import('./pages/HomePage.vue'), meta: { title: '首页' } },
        { path: 'search', name: 'search', component: () => import('./pages/SearchPage.vue'), meta: { title: '搜索' } },
        { path: 'library/:libraryId', name: 'library', component: () => import('./pages/LibraryPage.vue'), meta: { title: '媒体库' } },
        { path: 'item/:itemId', name: 'item_detail', component: () => import('./pages/ItemDetailPage.vue'), meta: { title: '详情' } },
      ],
    },

    // Admin layout
    {
      path: '/admin',
      component: () => import('./layouts/AdminLayout.vue'),
      meta: { requiresAuth: true },
      redirect: { name: 'admin_overview' },
      children: [
        // ── 模块 1：概览（单项）
        {
          path: 'overview',
          name: 'admin_overview',
          component: () => import('./pages/OverviewPage.vue'),
          meta: {
            title: '总览',
            navLabel: '总览',
            icon: 'overview',
            section: 'overview',
            sectionLabel: '概览',
            sectionIcon: 'overview',
            sectionOrder: 1,
            order: 1,
            sectionSingle: true,
            requiresAdmin: true,
          },
        },

        // ── 模块 2：媒体内容
        {
          path: 'media/libraries',
          name: 'media_libraries',
          component: () => import('./pages/LibrariesPage.vue'),
          meta: {
            title: '媒体库',
            navLabel: '媒体库',
            icon: 'library',
            section: 'media',
            sectionLabel: '媒体内容',
            sectionIcon: 'media',
            sectionOrder: 2,
            order: 1,
            requiresAdmin: true,
          },
        },
        {
          path: 'media/metadata',
          name: 'media_metadata',
          component: () => import('./pages/MetadataPage.vue'),
          meta: {
            title: '元数据',
            navLabel: '元数据',
            icon: 'metadata',
            section: 'media',
            sectionLabel: '媒体内容',
            sectionIcon: 'media',
            sectionOrder: 2,
            order: 2,
            requiresAdmin: true,
          },
        },
        {
          path: 'media/unmatched',
          name: 'media_unmatched',
          component: () => import('./pages/UnmatchedPage.vue'),
          meta: {
            title: '未匹配',
            navLabel: '未匹配',
            icon: 'metadata',
            section: 'media',
            sectionLabel: '媒体内容',
            sectionIcon: 'media',
            sectionOrder: 2,
            order: 3,
            requiresAdmin: true,
          },
        },

        // ── 模块 3：网关
        {
          path: 'gateway/emby-sources',
          name: 'gateway_emby_sources',
          component: () => import('./pages/EmbySourcesPage.vue'),
          meta: {
            title: 'Emby 源',
            navLabel: 'Emby 源',
            icon: 'emby',
            section: 'gateway',
            sectionLabel: '网关',
            sectionIcon: 'gateway',
            sectionOrder: 3,
            order: 1,
            requiresAdmin: true,
          },
        },
        {
          path: 'gateway/path-rules',
          name: 'gateway_path_rules',
          component: () => import('./pages/PathRuleSetsPage.vue'),
          meta: {
            title: '路径映射',
            navLabel: '路径映射',
            icon: 'pathMapping',
            section: 'gateway',
            sectionLabel: '网关',
            sectionIcon: 'gateway',
            sectionOrder: 3,
            order: 2,
            requiresAdmin: true,
          },
        },
        {
          path: 'gateway/backends',
          name: 'gateway_backends',
          component: () => import('./pages/BackendsPage.vue'),
          meta: {
            title: '资源池与后端',
            navLabel: '资源池与后端',
            icon: 'backends',
            section: 'gateway',
            sectionLabel: '网关',
            sectionIcon: 'gateway',
            sectionOrder: 3,
            order: 3,
            requiresAdmin: true,
          },
        },

        // ── 模块 4：观测中心（父容器 + 6 个子路由）
        {
          path: 'observability',
          component: () => import('./pages/ObservabilityPage.vue'),
          redirect: { name: 'observability_traffic' },
          children: [
            {
              path: 'traffic',
              name: 'observability_traffic',
              component: () => import('./pages/observability/TrafficTab.vue'),
              meta: {
                title: '流量',
                navLabel: '流量',
                icon: 'traffic',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 1,
                requiresAdmin: true,
              },
            },
            {
              path: 'redirect',
              name: 'observability_redirect',
              component: () => import('./pages/observability/RedirectTab.vue'),
              meta: {
                title: '重定向',
                navLabel: '重定向',
                icon: 'redirect',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 2,
                requiresAdmin: true,
              },
            },
            {
              path: 'ip-stats',
              name: 'observability_ip_stats',
              component: () => import('./pages/observability/IpStatsTab.vue'),
              meta: {
                title: 'IP 统计',
                navLabel: 'IP 统计',
                icon: 'ipStats',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 3,
                requiresAdmin: true,
              },
            },
            {
              path: 'playback',
              name: 'observability_playback',
              component: () => import('./pages/observability/PlaybackTab.vue'),
              meta: {
                title: '播放',
                navLabel: '播放',
                icon: 'playback',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 4,
                requiresAdmin: true,
              },
            },
            {
              path: 'stats',
              name: 'observability_stats',
              component: () => import('./pages/observability/StatsTab.vue'),
              meta: {
                title: '统计',
                navLabel: '统计',
                icon: 'stats',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 5,
                requiresAdmin: true,
              },
            },
            {
              path: 'logs',
              name: 'observability_logs',
              component: () => import('./pages/observability/SystemLogsTab.vue'),
              meta: {
                title: '系统日志',
                navLabel: '系统日志',
                icon: 'logs',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                order: 6,
                requiresAdmin: true,
              },
            },
          ],
        },

        // ── 模块 5：系统
        {
          path: 'system/users',
          name: 'system_users',
          component: () => import('./pages/UserManagementPage.vue'),
          meta: {
            title: '用户管理',
            navLabel: '用户管理',
            icon: 'users',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 1,
            requiresAdmin: true,
          },
        },
        {
          path: 'system/api-keys',
          name: 'system_api_keys',
          component: () => import('./pages/tools/ApiKeysTab.vue'),
          meta: {
            title: 'API 密钥',
            navLabel: 'API 密钥',
            icon: 'apikeys',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 2,
            requiresAdmin: true,
          },
        },
        {
          path: 'system/webhook',
          name: 'system_webhook',
          component: () => import('./pages/tools/WebhookTab.vue'),
          meta: {
            title: 'Webhook',
            navLabel: 'Webhook',
            icon: 'webhook',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 3,
            requiresAdmin: true,
          },
        },
        {
          path: 'system/backup',
          name: 'system_backup',
          component: () => import('./pages/tools/BackupTab.vue'),
          meta: {
            title: '备份',
            navLabel: '备份',
            icon: 'backup',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 4,
            requiresAdmin: true,
          },
        },
        {
          path: 'system/emby-migrate',
          name: 'system_emby_migrate',
          component: () => import('./pages/tools/EmbyMigrateTab.vue'),
          meta: {
            title: 'Emby 迁移',
            navLabel: 'Emby 迁移',
            icon: 'migrate',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 5,
            requiresAdmin: true,
          },
        },

        // ── 旧路径兼容：重定向到新路径
        { path: 'emby-sources', redirect: { name: 'gateway_emby_sources' } },
        { path: 'path-rules', redirect: { name: 'gateway_path_rules' } },
        { path: 'backends', redirect: { name: 'gateway_backends' } },
        { path: 'users', redirect: { name: 'system_users' } },
        { path: 'libraries', redirect: { name: 'media_libraries' } },
        { path: 'library-edit/:libraryId', redirect: { name: 'media_libraries' } },
        { path: 'metadata', redirect: { name: 'media_metadata' } },
        // Tools 旧的 query-based 链接重定向
        {
          path: 'tools',
          redirect: (to) => {
            const tab = (to.query.tab as string) || ''
            const map: Record<string, string> = {
              'api-keys': 'system_api_keys',
              webhook: 'system_webhook',
              backup: 'system_backup',
              'emby-migrate': 'system_emby_migrate',
            }
            return { name: map[tab] || 'system_api_keys' }
          },
        },
        { path: 'apikeys', redirect: { name: 'system_api_keys' } },
        { path: 'api-keys', redirect: { name: 'system_api_keys' } },
        { path: 'webhook', redirect: { name: 'system_webhook' } },
        { path: 'backup', redirect: { name: 'system_backup' } },
        { path: 'emby-migrate', redirect: { name: 'system_emby_migrate' } },
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
