import type { DataTableColumns, MessageApi } from 'naive-ui'
import { NTag } from 'naive-ui'
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { ApiError } from '@/api/client'
import { getConfig } from '@/api/config'
import { getErrorLogTail, readErrorLog } from '@/api/console'
import { getIPStatsSummary } from '@/api/ipstats'
import { listLogs } from '@/api/logs'
import {
  getRedirectSummary,
  getRedirectTraceAgg,
  listRedirectLogs,
  type RedirectTraceAggBackendRow,
  type RedirectTraceAggMetricRow,
} from '@/api/redirects'
import { getDailyStats } from '@/api/stats'
import type { Config, DailyStat, EmbySourceConfig, IPInfoLite, IPStatsSummary, RedirectSummary, RequestLog } from '@/types'

export type ObservabilityTab = 'traffic' | 'redirect302' | 'ipstats' | 'console'

const OBS_ACTIVE_TAB_KEY = 'observability.activeTab'
const ALLOWED_TABS: ObservabilityTab[] = ['traffic', 'redirect302', 'ipstats', 'console']
const DEFAULT_REDIRECT_WINDOW_MS = 24 * 60 * 60 * 1000

function unixSeconds(ms: number | null) {
  if (!ms) return ''
  return String(Math.floor(ms / 1000))
}

function getDefaultRedirectWindow(): [number, number] {
  const now = Date.now()
  return [now - DEFAULT_REDIRECT_WINDOW_MS, now]
}

function formatIPLocation(info: IPInfoLite | null) {
  if (!info) return '未知'
  const parts = [info.country, info.prov, info.city, info.area].filter((v) => v && v !== '未知')
  return parts.length > 0 ? parts.join(' - ') : '未知'
}

function ipInfoFor(ip: string, m: Record<string, IPInfoLite> | undefined) {
  if (!ip) return null
  return m && m[ip] ? m[ip] : null
}

function formatWindowLabel(sinceIso: string, untilIso: string) {
  const since = String(sinceIso || '').replace('T', ' ').slice(5, 16)
  const until = String(untilIso || '').replace('T', ' ').slice(5, 16)
  return `${since} ~ ${until}`
}

export function useObservability(message: MessageApi) {
  const shownWarnings = new Set<string>()

  const activeTab = ref<ObservabilityTab>('traffic')
  {
    const saved = String(localStorage.getItem(OBS_ACTIVE_TAB_KEY) || '') as ObservabilityTab
    if (ALLOWED_TABS.includes(saved)) activeTab.value = saved
  }

  const tag = ref<'proxy' | 'admin'>('proxy')
  const sourceId = ref<string>('')
  const sourceOptions = ref<{ label: string; value: string }[]>([{ label: '全部 Emby', value: '' }])
  const sourceById = ref<Record<string, EmbySourceConfig>>({})
  const backendNameById = ref<Record<string, string>>({})

  const statsDays = ref(7)
  const statsLoading = ref(false)
  const statsError = ref<string | null>(null)
  const statsItems = ref<DailyStat[]>([])

  const logsLoading = ref(false)
  const logsError = ref<string | null>(null)
  const logsItems = ref<RequestLog[]>([])
  const logsOffset = ref(0)
  const logsNextOffset = ref(0)
  const logsIPInfos = ref<Record<string, IPInfoLite>>({})

  const showDetail = ref(false)
  const selectedLog = ref<RequestLog | null>(null)

  const isLive = ref(false)
  let liveTimer: number | null = null

  const status = ref<number | null>(null)
  const ip = ref('')
  const pathPrefix = ref('')
  const keyword = ref('')
  const limit = ref(50)
  const range = ref<[number, number] | null>(null)

  const redirectSummaryLoading = ref(false)
  const redirectSummaryError = ref<string | null>(null)
  const redirectSummary = ref<RedirectSummary | null>(null)

  const redirectLogsLoading = ref(false)
  const redirectLogsError = ref<string | null>(null)
  const redirectLogsItems = ref<RequestLog[]>([])
  const redirectLogsOffset = ref(0)
  const redirectLogsNextOffset = ref(0)
  const redirectLogsIPInfos = ref<Record<string, IPInfoLite>>({})
  const redirectTraceAggLoading = ref(false)
  const redirectTraceAggError = ref<string | null>(null)
  const redirectTraceRequestStages = ref<RedirectTraceAggMetricRow[]>([])
  const redirectTraceAttemptStages = ref<RedirectTraceAggMetricRow[]>([])
  const redirectTraceByBackend = ref<RedirectTraceAggBackendRow[]>([])
  const redirectTraceAggMeta = ref<{ sampled: number; parsed: number; skipped: number; sample_limit: number }>({
    sampled: 0,
    parsed: 0,
    skipped: 0,
    sample_limit: 0,
  })

  const redirectIsLive = ref(false)
  let redirectLiveTimer: number | null = null

  const redirectBackend = ref<string>('')
  const redirectUserID = ref('')
  const redirectUserName = ref('')
  const redirectIP = ref('')
  const redirectUAContains = ref('')
  const redirectPathPrefix = ref('')
  const redirectLimit = ref(50)
  const redirectRange = ref<[number, number] | null>(null)

  const ipStatsLoading = ref(false)
  const ipStatsError = ref<string | null>(null)
  const ipStatsSummary = ref<IPStatsSummary | null>(null)
  const ipStatsMode = ref<'all' | 'redirect302'>('all')
  const ipStatsRange = ref<[number, number] | null>(null)

  const consoleLoading = ref(false)
  const consoleError = ref<string | null>(null)
  const consoleText = ref('')
  const consoleOffset = ref(0)
  const consoleSize = ref(0)
  const consoleIsLive = ref(false)
  const consoleSearch = ref('')
  let consoleLiveTimer: number | null = null

  const selectedIPInfo = computed(() => {
    const clientIP = String(selectedLog.value?.client_ip || '').trim()
    if (!clientIP) return null
    return (
      ipInfoFor(clientIP, logsIPInfos.value) ||
      ipInfoFor(clientIP, redirectLogsIPInfos.value) ||
      (redirectSummary.value?.ip_infos?.[clientIP] || null)
    )
  })

  const selectedIPLocation = computed(() => formatIPLocation(selectedIPInfo.value))

  const selectedSourceLabel = computed(() => {
    const id = String(selectedLog.value?.source_id || '').trim()
    if (!id) return '-'
    const src = sourceById.value[id]
    if (!src) return id
    return src.name ? `${src.name} (${src.id})` : src.id
  })

  const selectedSourceUpstream = computed(() => {
    const id = String(selectedLog.value?.source_id || '').trim()
    if (!id) return '-'
    const src = sourceById.value[id]
    if (!src?.upstream?.host) return '-'
    const base = String(src.upstream.base_path || '').trim()
    return base ? `${src.upstream.host}${base}` : src.upstream.host
  })

  const canNextPage = computed(() => {
    if (logsLoading.value || logsItems.value.length === 0 || isLive.value) return false
    return logsNextOffset.value > logsOffset.value
  })

  const canNextRedirectPage = computed(() => {
    if (redirectLogsLoading.value || redirectLogsItems.value.length === 0 || redirectIsLive.value) return false
    return redirectLogsNextOffset.value > redirectLogsOffset.value
  })

  const statsSummary = computed(() => {
    const items = statsItems.value
    const totalRequests = items.reduce((acc, it) => acc + (it.requests || 0), 0)
    const total302 = items.reduce((acc, it) => acc + (it.redirects302 || 0), 0)
    const total4xx = items.reduce((acc, it) => acc + (it.status4xx || 0), 0)
    const total5xx = items.reduce((acc, it) => acc + (it.status5xx || 0), 0)
    return { totalRequests, total302, total4xx, total5xx }
  })

  const redirectBackendCounts = computed(() => {
    const items = redirectSummary.value?.by_backend || []
    const map: Record<string, number> = {}
    for (const it of items) map[it.backend] = it.count
    return map
  })

  const topRedirectBackends = computed(() => {
    const items = redirectSummary.value?.by_backend || []
    return Array.isArray(items) ? items.slice(0, 2) : []
  })

  const redirectBackendOptions = computed(() => {
    const map = redirectBackendCounts.value
    return Object.keys(map).map((k) => {
      const name = backendNameById.value[k]
      return { label: name ? `${name} (${k})` : k, value: k }
    })
  })

  const redirectWindowLabel = computed(() => {
    if (redirectSummary.value?.since && redirectSummary.value?.until) {
      return formatWindowLabel(redirectSummary.value.since, redirectSummary.value.until)
    }
    return '近24小时'
  })

  const ipStatsUseCumulative = computed(() => !ipStatsRange.value && ipStatsMode.value === 'all' && tag.value === 'proxy')

  const ipStatsScopeLabel = computed(() => {
    if (ipStatsSummary.value?.scope === 'cumulative') return '所有请求数据'
    return '动态计算数据'
  })

  const ipStatsRangeLabel = computed(() => {
    if (ipStatsSummary.value?.scope === 'cumulative') return '累计所有数据'
    if (ipStatsSummary.value?.since) {
      return `${ipStatsSummary.value.since.replace('T', ' ').slice(0, 19)} ~ ${ipStatsSummary.value.until.replace('T', ' ').slice(0, 19)}`
    }
    return '近24小时'
  })

  const consoleFilteredText = computed(() => {
    const q = consoleSearch.value.trim().toLowerCase()
    const lines = consoleText.value.split('\n')
    const filtered = q ? lines.filter((line) => line.toLowerCase().includes(q)) : lines
    return filtered.reverse().join('\n')
  })

  const logsColumns: DataTableColumns<RequestLog> = [
    {
      title: 'ID',
      key: 'id',
      width: 80,
      render: (row) => h('span', { class: 'tabular-nums', style: { opacity: 0.5 } }, row.id),
    },
    {
      title: 'Time',
      key: 'created_at',
      width: 160,
      render: (row) => h('span', { class: 'tabular-nums' }, String(row.created_at).replace('T', ' ').slice(5, 19)),
    },
    {
      title: 'Status',
      key: 'status',
      width: 90,
      render: (row) => {
        const s = row.status
        let type: 'success' | 'warning' | 'error' | 'info' | 'default' = 'default'
        if (s >= 200 && s < 300) type = 'success'
        else if (s >= 300 && s < 400) type = 'info'
        else if (s >= 400 && s < 500) type = 'warning'
        else if (s >= 500) type = 'error'
        return h(NTag, { size: 'small', type, bordered: false, round: true }, { default: () => s })
      },
    },
    { title: 'Method', key: 'method', width: 80 },
    {
      title: 'Path',
      key: 'path',
      ellipsis: { tooltip: true },
      render: (row) => h('span', { class: 'path-cell' }, row.path),
    },
    {
      title: 'Latency',
      key: 'latency_ms',
      width: 100,
      align: 'right',
      render: (row) => {
        const ms = row.latency_ms
        const color = ms > 1000 ? 'var(--app-error)' : ms > 500 ? 'var(--app-warning)' : 'var(--app-text-muted)'
        return h('span', { style: { color, fontWeight: 500 } }, `${ms}ms`)
      },
    },
    { title: 'Client IP', key: 'client_ip', width: 130, align: 'right', ellipsis: { tooltip: true } },
    {
      title: '归属地',
      key: 'geo_location',
      ellipsis: { tooltip: true },
      render: (row) => h('span', {}, formatIPLocation(ipInfoFor(row.client_ip, logsIPInfos.value))),
    },
  ]

  const redirectLogsColumns: DataTableColumns<RequestLog> = [
    {
      title: 'ID',
      key: 'id',
      width: 80,
      render: (row) => h('span', { class: 'tabular-nums', style: { opacity: 0.5 } }, row.id),
    },
    {
      title: 'Time',
      key: 'created_at',
      width: 160,
      render: (row) => h('span', { class: 'tabular-nums' }, String(row.created_at).replace('T', ' ').slice(5, 19)),
    },
    {
      title: 'Backend',
      key: 'redirect_backend',
      width: 160,
      render: (row) => {
        const v = String(row.redirect_backend || '').toLowerCase()
        const isCdn = v.includes('cdn')
        return h(
          'div',
          { class: 'backend-tag-cell' },
          h(NTag, { size: 'small', bordered: false, round: true, type: isCdn ? 'info' : 'success' }, { default: () => row.redirect_backend || '-' }),
        )
      },
    },
    {
      title: 'User',
      key: 'emby_user_name',
      width: 180,
      ellipsis: { tooltip: true },
      render: (row) => {
        const label = row.emby_user_name?.trim() ? `${row.emby_user_name} (${row.emby_user_id || '0'})` : (row.emby_user_id || '0')
        return h('span', { class: 'tabular-nums' }, label)
      },
    },
    {
      title: 'Path',
      key: 'path',
      ellipsis: { tooltip: true },
      render: (row) => h('span', { class: 'path-cell' }, row.path),
    },
    {
      title: 'Location',
      key: 'redirect_location',
      ellipsis: { tooltip: true },
      render: (row) => h('span', { class: 'path-cell' }, row.redirect_location || '-'),
    },
    { title: 'Client IP', key: 'client_ip', width: 130, align: 'right', ellipsis: { tooltip: true } },
    {
      title: '归属地',
      key: 'geo_location',
      ellipsis: { tooltip: true },
      render: (row) => h('span', {}, formatIPLocation(ipInfoFor(row.client_ip, redirectLogsIPInfos.value))),
    },
  ]

  const rowProps = (row: RequestLog) => ({
    style: 'cursor: pointer;',
    onClick: () => {
      selectedLog.value = row
      showDetail.value = true
    },
  })

  async function refreshRedirectSummary() {
    redirectSummaryLoading.value = true
    redirectSummaryError.value = null
    try {
      let since: string | undefined
      let until: string | undefined
      if (redirectRange.value) {
        since = unixSeconds(redirectRange.value[0])
        until = unixSeconds(redirectRange.value[1])
      } else {
        const [defaultSince, defaultUntil] = getDefaultRedirectWindow()
        since = unixSeconds(defaultSince)
        until = unixSeconds(defaultUntil)
      }
      redirectSummary.value = await getRedirectSummary({ source_id: sourceId.value || undefined, since, until, limit: 20, with_ip_info: true })
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      redirectSummaryError.value = err.message
      if (!redirectIsLive.value) message.error(err.message)
    } finally {
      redirectSummaryLoading.value = false
    }
  }

  async function refreshRedirectLogs(resetOffset = true) {
    if (redirectLogsLoading.value && !redirectIsLive.value) return
    redirectLogsLoading.value = true
    redirectLogsError.value = null
    try {
      const nextOffset = resetOffset ? 0 : (redirectIsLive.value ? 0 : redirectLogsNextOffset.value)

      let since: string | undefined
      let until: string | undefined
      if (redirectRange.value) {
        since = unixSeconds(redirectRange.value[0])
        until = unixSeconds(redirectRange.value[1])
      } else {
        const [defaultSince, defaultUntil] = getDefaultRedirectWindow()
        since = unixSeconds(defaultSince)
        until = unixSeconds(defaultUntil)
      }

      const resp = await listRedirectLogs({
        source_id: sourceId.value || undefined,
        user_id: redirectUserID.value.trim() || undefined,
        user_name: redirectUserName.value.trim() || undefined,
        backend: redirectBackend.value || undefined,
        ip: redirectIP.value.trim() || undefined,
        ua_contains: redirectUAContains.value.trim() || undefined,
        path_prefix: redirectPathPrefix.value.trim() || undefined,
        since,
        until,
        limit: Math.max(1, Math.min(200, Number(redirectLimit.value) || 50)),
        offset: nextOffset,
        with_ip_info: true,
      })
      const items = Array.isArray(resp.items) ? resp.items : []
      const ipInfos = resp.ip_infos || {}

      if (redirectIsLive.value && !resetOffset && items.length > 0) {
        const currentMaxId = redirectLogsItems.value.length > 0 ? redirectLogsItems.value[0].id : 0
        const newItems = items.filter((it) => it.id > currentMaxId)
        if (newItems.length > 0) {
          redirectLogsItems.value = [...newItems, ...redirectLogsItems.value].slice(0, 500)
          redirectLogsIPInfos.value = { ...redirectLogsIPInfos.value, ...ipInfos }
        }
      } else {
        redirectLogsOffset.value = nextOffset
        redirectLogsNextOffset.value = typeof resp.next_offset === 'number' ? resp.next_offset : nextOffset + items.length
        redirectLogsItems.value = items
        redirectLogsIPInfos.value = ipInfos
      }
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      redirectLogsError.value = err.message
      if (!redirectIsLive.value) message.error(err.message)
    } finally {
      redirectLogsLoading.value = false
    }
  }

  async function refreshRedirectTraceAgg() {
    if (redirectTraceAggLoading.value) return
    redirectTraceAggLoading.value = true
    redirectTraceAggError.value = null
    try {
      let since: string | undefined
      let until: string | undefined
      if (redirectRange.value) {
        since = unixSeconds(redirectRange.value[0])
        until = unixSeconds(redirectRange.value[1])
      } else {
        const [defaultSince, defaultUntil] = getDefaultRedirectWindow()
        since = unixSeconds(defaultSince)
        until = unixSeconds(defaultUntil)
      }
      const resp = await getRedirectTraceAgg({
        source_id: sourceId.value || undefined,
        backend: redirectBackend.value || undefined,
        since,
        until,
        limit: redirectIsLive.value ? 1000 : 5000,
      })
      redirectTraceRequestStages.value = Array.isArray(resp.request_stages) ? resp.request_stages : []
      redirectTraceAttemptStages.value = Array.isArray(resp.attempt_stages) ? resp.attempt_stages : []
      redirectTraceByBackend.value = Array.isArray(resp.by_backend) ? resp.by_backend : []
      redirectTraceAggMeta.value = {
        sampled: Number(resp.sampled || 0),
        parsed: Number(resp.parsed || 0),
        skipped: Number(resp.skipped || 0),
        sample_limit: Number(resp.sample_limit || 0),
      }
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      redirectTraceAggError.value = err.message
      if (!redirectIsLive.value) message.error(err.message)
    } finally {
      redirectTraceAggLoading.value = false
    }
  }

  function appendConsoleText(chunk: string) {
    if (!chunk) return
    const next = consoleText.value ? `${consoleText.value}\n${chunk}` : chunk
    if (next.length > 1024 * 1024) {
      consoleText.value = next.slice(next.length - 1024 * 1024)
      return
    }
    consoleText.value = next
  }

  async function refreshIPStats(reset = true) {
    if (ipStatsLoading.value) return
    ipStatsLoading.value = true
    ipStatsError.value = null
    try {
      if (reset) ipStatsSummary.value = null

      let since: string | undefined
      let until: string | undefined
      if (!ipStatsUseCumulative.value) {
        if (ipStatsRange.value) {
          since = unixSeconds(ipStatsRange.value[0])
          until = unixSeconds(ipStatsRange.value[1])
        } else {
          const [defaultSince, defaultUntil] = getDefaultRedirectWindow()
          since = unixSeconds(defaultSince)
          until = unixSeconds(defaultUntil)
        }
      }

      ipStatsSummary.value = await getIPStatsSummary({
        tag: tag.value,
        mode: ipStatsMode.value,
        source_id: sourceId.value || undefined,
        since,
        until,
        limit: 20,
        scope: ipStatsUseCumulative.value ? 'cumulative' : undefined,
      })
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      ipStatsError.value = err.message
      message.error(err.message)
    } finally {
      ipStatsLoading.value = false
    }
  }

  async function loadConsoleTail() {
    consoleLoading.value = true
    consoleError.value = null
    try {
      const resp = await getErrorLogTail(300)
      consoleText.value = resp.text || ''
      consoleOffset.value = resp.next_offset || 0
      consoleSize.value = resp.size || 0
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      consoleError.value = err.message
      if (!consoleIsLive.value) message.error(err.message)
    } finally {
      consoleLoading.value = false
    }
  }

  async function loadConsoleIncrement() {
    if (consoleLoading.value) return
    consoleLoading.value = true
    consoleError.value = null
    try {
      const resp = await readErrorLog(consoleOffset.value, 128 * 1024)
      const currentSize = resp.size || 0
      if (currentSize < consoleOffset.value) {
        await loadConsoleTail()
        return
      }
      if (resp.text) appendConsoleText(resp.text)
      consoleOffset.value = resp.next_offset || consoleOffset.value
      consoleSize.value = currentSize
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      consoleError.value = err.message
      if (!consoleIsLive.value) message.error(err.message)
    } finally {
      consoleLoading.value = false
    }
  }

  async function refreshStats() {
    statsLoading.value = true
    statsError.value = null
    try {
      const days = Math.max(1, Math.min(3650, Number(statsDays.value) || 7))
      const resp = await getDailyStats(tag.value, days, tag.value === 'proxy' ? (sourceId.value || undefined) : undefined)
      statsItems.value = Array.isArray(resp.items) ? resp.items : []
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      statsError.value = err.message
      message.error(err.message)
    } finally {
      statsLoading.value = false
    }
  }

  async function refreshLogs(resetOffset = true) {
    if (logsLoading.value && !isLive.value) return
    logsLoading.value = true
    logsError.value = null
    try {
      const nextOffset = resetOffset ? 0 : (isLive.value ? 0 : logsNextOffset.value)
      let since: string | undefined
      let until: string | undefined
      if (range.value) {
        since = unixSeconds(range.value[0])
        until = unixSeconds(range.value[1])
      }
      const resp = await listLogs({
        tag: tag.value,
        source_id: tag.value === 'proxy' ? (sourceId.value || undefined) : undefined,
        status: status.value ?? undefined,
        ip: ip.value.trim() || undefined,
        path_prefix: pathPrefix.value.trim() || undefined,
        q: keyword.value.trim() || undefined,
        since,
        until,
        limit: Math.max(1, Math.min(200, Number(limit.value) || 50)),
        offset: nextOffset,
        with_ip_info: true,
      })
      if (Array.isArray(resp.warnings)) {
        for (const w of resp.warnings) {
          if (!w || shownWarnings.has(w)) continue
          shownWarnings.add(w)
          message.warning(w)
        }
      }
      const items = Array.isArray(resp.items) ? resp.items : []
      const ipInfos = resp.ip_infos || {}

      if (isLive.value && !resetOffset && items.length > 0) {
        const currentMaxId = logsItems.value.length > 0 ? logsItems.value[0].id : 0
        const newItems = items.filter((it) => it.id > currentMaxId)
        if (newItems.length > 0) {
          logsItems.value = [...newItems, ...logsItems.value].slice(0, 500)
          logsIPInfos.value = { ...logsIPInfos.value, ...ipInfos }
        }
      } else {
        logsOffset.value = nextOffset
        logsNextOffset.value = typeof resp.next_offset === 'number' ? resp.next_offset : nextOffset + items.length
        logsItems.value = items
        logsIPInfos.value = ipInfos
      }
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError((e as Error).message || '加载失败', 0)
      logsError.value = err.message
      if (!isLive.value) message.error(err.message)
    } finally {
      logsLoading.value = false
    }
  }

  function startLive() {
    stopLive()
    liveTimer = window.setInterval(() => {
      void refreshLogs(false)
    }, 3000)
  }

  function stopLive() {
    if (!liveTimer) return
    window.clearInterval(liveTimer)
    liveTimer = null
  }

  function startRedirectLive() {
    stopRedirectLive()
    redirectLiveTimer = window.setInterval(() => {
      void refreshRedirectSummary()
      void refreshRedirectLogs(false)
      void refreshRedirectTraceAgg()
    }, 3000)
  }

  function stopRedirectLive() {
    if (!redirectLiveTimer) return
    window.clearInterval(redirectLiveTimer)
    redirectLiveTimer = null
  }

  function startConsoleLive() {
    stopConsoleLive()
    consoleLiveTimer = window.setInterval(() => {
      void loadConsoleIncrement()
    }, 1500)
  }

  function stopConsoleLive() {
    if (!consoleLiveTimer) return
    window.clearInterval(consoleLiveTimer)
    consoleLiveTimer = null
  }

  async function nextPage() {
    await refreshLogs(false)
  }

  async function nextRedirectPage() {
    await refreshRedirectLogs(false)
  }

  function resetFilters() {
    status.value = null
    ip.value = ''
    pathPrefix.value = ''
    keyword.value = ''
    range.value = null
    limit.value = 50
    if (!isLive.value) void refreshLogs(true)
  }

  function resetRedirectFilters() {
    redirectBackend.value = ''
    redirectUserID.value = ''
    redirectUserName.value = ''
    redirectIP.value = ''
    redirectUAContains.value = ''
    redirectPathPrefix.value = ''
    redirectRange.value = null
    redirectLimit.value = 50
    if (!redirectIsLive.value) {
      void refreshRedirectSummary()
      void refreshRedirectLogs(true)
      void refreshRedirectTraceAgg()
    }
  }

  async function onTagChange(v: 'proxy' | 'admin') {
    tag.value = v
    if (v === 'admin') sourceId.value = ''
    await refreshStats()
    await refreshLogs(true)
    await refreshIPStats(true)
  }

  watch(isLive, (val) => {
    if (val) {
      resetFilters()
      startLive()
    } else {
      stopLive()
    }
  })

  watch(redirectIsLive, (val) => {
    if (val) {
      resetRedirectFilters()
      startRedirectLive()
    } else {
      stopRedirectLive()
    }
  })

  watch(consoleIsLive, async (val) => {
    if (val) {
      if (!consoleText.value) await loadConsoleTail()
      startConsoleLive()
    } else {
      stopConsoleLive()
    }
  })

  watch(activeTab, async (tab) => {
    if (tab === 'console' && !consoleText.value) {
      await loadConsoleTail()
    }
  })

  watch(sourceId, async () => {
    stopLive()
    stopRedirectLive()
    await refreshStats()
    await refreshLogs(true)
    await refreshRedirectSummary()
    await refreshRedirectLogs(true)
    await refreshRedirectTraceAgg()
    await refreshIPStats(true)
  })

  watch(activeTab, (v) => {
    if (ALLOWED_TABS.includes(v)) localStorage.setItem(OBS_ACTIVE_TAB_KEY, v)
  })

  onMounted(() => {
    getConfig()
      .then((cfg: Config) => {
        const opts = [{ label: '全部 Emby', value: '' }]
        const byID: Record<string, EmbySourceConfig> = {}
        for (const s of Array.isArray(cfg.sources) ? cfg.sources : []) {
          if (!s.enabled) continue
          const label = s.name ? `${s.name} (${s.id})` : s.id
          opts.push({ label, value: s.id })
          if (s.id) byID[s.id] = s
        }
        sourceOptions.value = opts
        sourceById.value = byID

        const backendMap: Record<string, string> = {}
        for (const b of Array.isArray(cfg.backends) ? cfg.backends : []) {
          if (!b.enabled || !b.id) continue
          if (b.name) backendMap[b.id] = b.name
        }
        backendNameById.value = backendMap
      })
      .catch(() => {
        sourceOptions.value = [{ label: '全部 Emby', value: '' }]
        sourceById.value = {}
        backendNameById.value = {}
      })

    void refreshStats()
    void refreshLogs(true)
    void refreshRedirectSummary()
    void refreshRedirectLogs(true)
    void refreshRedirectTraceAgg()
    void refreshIPStats(true)
  })

  onBeforeUnmount(() => {
    stopLive()
    stopRedirectLive()
    stopConsoleLive()
  })

  return {
    activeTab,
    tag,
    sourceId,
    sourceOptions,
    logsColumns,
    logsItems,
    logsLoading,
    logsOffset,
    canNextPage,
    rowProps,
    isLive,
    status,
    ip,
    pathPrefix,
    keyword,
    range,
    statsSummary,
    redirectIsLive,
    redirectBackend,
    redirectUserID,
    redirectUserName,
    redirectIP,
    redirectUAContains,
    redirectPathPrefix,
    redirectLimit,
    redirectRange,
    redirectSummary,
    redirectSummaryError,
    redirectLogsError,
    redirectSummaryLoading,
    redirectLogsLoading,
    redirectLogsItems,
    redirectLogsColumns,
    redirectLogsOffset,
    canNextRedirectPage,
    redirectBackendOptions,
    topRedirectBackends,
    redirectWindowLabel,
    redirectTraceAggLoading,
    redirectTraceAggError,
    redirectTraceRequestStages,
    redirectTraceAttemptStages,
    redirectTraceByBackend,
    redirectTraceAggMeta,
    ipStatsMode,
    ipStatsRange,
    ipStatsError,
    ipStatsLoading,
    ipStatsSummary,
    ipStatsScopeLabel,
    ipStatsRangeLabel,
    ipStatsUseCumulative,
    consoleSearch,
    consoleIsLive,
    consoleError,
    consoleLoading,
    consoleOffset,
    consoleSize,
    consoleFilteredText,
    showDetail,
    selectedLog,
    selectedIPLocation,
    selectedSourceLabel,
    selectedSourceUpstream,
    onTagChange,
    refreshLogs,
    resetFilters,
    nextPage,
    refreshRedirectSummary,
    refreshRedirectLogs,
    refreshRedirectTraceAgg,
    resetRedirectFilters,
    nextRedirectPage,
    refreshIPStats,
    loadConsoleTail,
    loadConsoleIncrement,
  }
}
