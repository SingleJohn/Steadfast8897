export type ScrapeProviderMeta = {
  label: string
  badge?: string
  desc: string
  accent: string
}

export const scrapeProviderMeta: Record<string, ScrapeProviderMeta> = {
  tmdb: { label: 'TMDB', badge: '基准', desc: '主识别源 · 电影/剧集', accent: '#0ea5e9' },
  tvdb: { label: 'TVDB', desc: '剧集元数据 · 季海报', accent: '#16a34a' },
  bangumi: { label: 'Bangumi', desc: '动画/ACG', accent: '#f472b6' },
  douban: { label: '豆瓣', desc: '中文简介/评分', accent: '#10b981' },
  fanart: { label: 'Fanart.tv', desc: '仅图片补充', accent: '#f97316' },
}

export const defaultScrapeProviders = Object.keys(scrapeProviderMeta)

export const scrapeProviderOptions = defaultScrapeProviders.map((name) => ({
  label: scrapeProviderMeta[name]?.label || name,
  value: name,
}))

export const scrapeFieldLabels: Record<string, string> = {
  overview: '简介',
  title: '标题',
  original_title: '原始标题',
  tagline: '标语',
  premiered: '首映日期',
  year: '年份',
  rating: '评分',
  actors: '演员',
  poster: '海报',
  backdrop: '背景图',
  season_poster: '季海报',
}

export function getScrapeProviderLabel(name: string) {
  return scrapeProviderMeta[name]?.label || name
}

export function getScrapeFieldLabel(name: string) {
  return scrapeFieldLabels[name] || name
}

export function normalizeProviderList(names?: string[] | null, fallback: string[] = defaultScrapeProviders) {
  const basis = Array.isArray(names) && names.length > 0 ? names : fallback
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of basis) {
    const name = String(raw || '').trim().toLowerCase()
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  return out
}

export function buildOrderedProviders(
  priority?: Record<string, number> | null,
  allProviders: string[] = defaultScrapeProviders,
  preferredOrder?: string[] | null,
) {
  const seen = new Set<string>()
  const seed = [...normalizeProviderList(preferredOrder || [], []), ...normalizeProviderList(allProviders)]
  const ordered = seed.filter((name) => {
    if (seen.has(name)) return false
    seen.add(name)
    return true
  })
  if (!priority || Object.keys(priority).length === 0) {
    return ordered
  }
  return [...ordered].sort((a, b) => {
    const pa = typeof priority[a] === 'number' ? priority[a] : Number.MAX_SAFE_INTEGER
    const pb = typeof priority[b] === 'number' ? priority[b] : Number.MAX_SAFE_INTEGER
    if (pa !== pb) return pa - pb
    return ordered.indexOf(a) - ordered.indexOf(b)
  })
}

export function buildProviderPriorityMap(order: string[]) {
  const out: Record<string, number> = {}
  for (const [index, name] of order.entries()) {
    out[name] = index + 1
  }
  return out
}
