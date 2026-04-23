import { createRouter, createWebHashHistory } from 'vue-router'

/**
 * Admin 后台路由结构（5 个一级模块 × N 个二级菜单）
 *
 *   概览（单项）       →  /admin/overview
 *   媒体内容（submenu）→  /admin/media/{libraries|metadata}
 *   网关（submenu）    →  /admin/gateway/{emby-sources|path-rules|backends}
 *   观测中心（submenu）→  /admin/observability/gateway/{traffic|redirect|ip-stats}
 *                         /admin/observability/service/{playback|stats|logs|tasks}
 *   系统（submenu）    →  /admin/system/{users|api-keys|branding|webhook|backup|emby-migrate}
 *
 * 菜单由 AdminLayout 根据路由 meta 动态生成：
 *   - section / sectionLabel / sectionOrder / sectionIcon：一级模块归属
 *   - subSection / subSectionLabel / subSectionOrder：section 内部的分组（用于观测中心拆网关/服务两组）
 *   - navLabel / icon / order：二级菜单显示
 *   - sectionSingle：模块下只有一项（如"概览"），菜单不包 submenu
 *   - requiresAdmin：权限过滤
 *
 * 观测中心下有两个父容器：
 *   - GatewayObsLayout：持有 useGatewayObservability，共享 source/tag 过滤器给网关 3 个子页
 *   - ServiceObsLayout：纯 router-view 壳，4 个子页各自管理数据
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
        { path: 'movies', name: 'movies', component: () => import('./pages/LibraryPage.vue'), meta: { title: '电影' } },
        { path: 'tvshows', name: 'tvshows', component: () => import('./pages/LibraryPage.vue'), meta: { title: '剧集' } },
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

        // ── 模块 4：观测中心（两个父容器：网关观测 / 服务观测）
        {
          path: 'observability',
          redirect: { name: 'obs_gw_traffic' },
        },
        {
          path: 'observability/gateway',
          component: () => import('./pages/observability/gateway/GatewayObsLayout.vue'),
          redirect: { name: 'obs_gw_traffic' },
          children: [
            {
              path: 'traffic',
              name: 'obs_gw_traffic',
              component: () => import('./pages/observability/gateway/TrafficTab.vue'),
              meta: {
                title: '流量',
                navLabel: '流量',
                icon: 'traffic',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'gateway',
                subSectionLabel: '网关观测',
                subSectionOrder: 1,
                order: 1,
                requiresAdmin: true,
              },
            },
            {
              path: 'redirect',
              name: 'obs_gw_redirect',
              component: () => import('./pages/observability/gateway/RedirectTab.vue'),
              meta: {
                title: '重定向',
                navLabel: '重定向',
                icon: 'redirect',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'gateway',
                subSectionLabel: '网关观测',
                subSectionOrder: 1,
                order: 2,
                requiresAdmin: true,
              },
            },
            {
              path: 'ip-stats',
              name: 'obs_gw_ip_stats',
              component: () => import('./pages/observability/gateway/IpStatsTab.vue'),
              meta: {
                title: 'IP 统计',
                navLabel: 'IP 统计',
                icon: 'ipStats',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'gateway',
                subSectionLabel: '网关观测',
                subSectionOrder: 1,
                order: 3,
                requiresAdmin: true,
              },
            },
          ],
        },
        {
          path: 'observability/service',
          component: () => import('./pages/observability/service/ServiceObsLayout.vue'),
          redirect: { name: 'obs_svc_playback' },
          children: [
            {
              path: 'playback',
              name: 'obs_svc_playback',
              component: () => import('./pages/observability/service/PlaybackTab.vue'),
              meta: {
                title: '播放',
                navLabel: '播放',
                icon: 'playback',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'service',
                subSectionLabel: '服务观测',
                subSectionOrder: 2,
                order: 1,
                requiresAdmin: true,
              },
            },
            {
              path: 'stats',
              name: 'obs_svc_stats',
              component: () => import('./pages/observability/service/StatsTab.vue'),
              meta: {
                title: '统计',
                navLabel: '统计',
                icon: 'stats',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'service',
                subSectionLabel: '服务观测',
                subSectionOrder: 2,
                order: 2,
                requiresAdmin: true,
              },
            },
            {
              path: 'logs',
              name: 'obs_svc_logs',
              component: () => import('./pages/observability/service/SystemLogsTab.vue'),
              meta: {
                title: '系统日志',
                navLabel: '系统日志',
                icon: 'logs',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'service',
                subSectionLabel: '服务观测',
                subSectionOrder: 2,
                order: 3,
                requiresAdmin: true,
              },
            },
            {
              path: 'tasks',
              name: 'obs_svc_tasks',
              component: () => import('./pages/observability/service/TasksTab.vue'),
              meta: {
                title: '作业调度',
                navLabel: '作业调度',
                icon: 'tasks',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'service',
                subSectionLabel: '服务观测',
                subSectionOrder: 2,
                order: 4,
                requiresAdmin: true,
              },
            },
            {
              path: 'queue',
              name: 'obs_svc_queue',
              component: () => import('./pages/observability/service/QueueTab.vue'),
              meta: {
                title: '队列管道',
                navLabel: '队列管道',
                icon: 'tasks',
                section: 'observability',
                sectionLabel: '观测中心',
                sectionIcon: 'observability',
                sectionOrder: 4,
                subSection: 'service',
                subSectionLabel: '服务观测',
                subSectionOrder: 2,
                order: 5,
                requiresAdmin: true,
              },
            },
          ],
        },
        // 观测中心旧路径 / 旧路由名 → 新路径重定向（保收藏夹 / 外链兼容）
        { path: 'observability/traffic', redirect: { name: 'obs_gw_traffic' } },
        { path: 'observability/redirect', redirect: { name: 'obs_gw_redirect' } },
        { path: 'observability/ip-stats', redirect: { name: 'obs_gw_ip_stats' } },
        { path: 'observability/playback', redirect: { name: 'obs_svc_playback' } },
        { path: 'observability/stats', redirect: { name: 'obs_svc_stats' } },
        { path: 'observability/logs', redirect: { name: 'obs_svc_logs' } },
        { path: 'observability/tasks', redirect: { name: 'obs_svc_tasks' } },

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
          path: 'system/branding',
          name: 'system_branding',
          component: () => import('./pages/tools/BrandingTab.vue'),
          meta: {
            title: '系统品牌',
            navLabel: '系统品牌',
            icon: 'server',
            section: 'system',
            sectionLabel: '系统',
            sectionIcon: 'system',
            sectionOrder: 5,
            order: 3,
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
            order: 4,
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
            order: 5,
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
            order: 6,
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
              branding: 'system_branding',
              webhook: 'system_webhook',
              backup: 'system_backup',
              'emby-migrate': 'system_emby_migrate',
            }
            return { name: map[tab] || 'system_api_keys' }
          },
        },
        { path: 'apikeys', redirect: { name: 'system_api_keys' } },
        { path: 'api-keys', redirect: { name: 'system_api_keys' } },
        { path: 'branding', redirect: { name: 'system_branding' } },
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

// 后端热更后旧 index.html 还指向已失效的 chunk 文件名，动态 import 404 会被静默吞掉，
// 表现为进入页面白屏且 DevTools XHR 面板无任何请求。检测到就触发一次硬刷新拿新 HTML。
const RELOAD_GUARD_KEY = 'fyms_chunk_reload_ts'
function isChunkLoadError(err: unknown): boolean {
  const msg = String((err as { message?: string })?.message ?? err ?? '')
  return (
    /Failed to fetch dynamically imported module/i.test(msg) ||
    /error loading dynamically imported module/i.test(msg) ||
    /Importing a module script failed/i.test(msg) ||
    /Loading chunk \S+ failed/i.test(msg) ||
    /ChunkLoadError/i.test(msg)
  )
}
function reloadOnceForChunkError() {
  const last = Number(sessionStorage.getItem(RELOAD_GUARD_KEY) || 0)
  const now = Date.now()
  if (now - last < 10_000) return
  sessionStorage.setItem(RELOAD_GUARD_KEY, String(now))
  window.location.reload()
}
router.onError((err) => {
  if (isChunkLoadError(err)) reloadOnceForChunkError()
})
window.addEventListener('error', (ev) => {
  if (isChunkLoadError(ev.error ?? ev.message)) reloadOnceForChunkError()
})
window.addEventListener('unhandledrejection', (ev) => {
  if (isChunkLoadError(ev.reason)) reloadOnceForChunkError()
})

export { router }
export default router
