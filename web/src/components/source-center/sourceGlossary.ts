// 在线媒体 / SourceCenter 术语词典：运行态、健康分项、能力标签、维度、状态色的中文释义与配色。
// 所有 tooltip / 图例 / 列头说明共用此处，避免散落硬编码。

export type TagType = 'success' | 'error' | 'warning' | 'info' | 'default'

export interface GlossaryEntry {
  /** 简短标签（角标 / tag 文案） */
  label: string
  /** 完整中文名 */
  title: string
  /** 一句话说明 */
  desc: string
}

/** 运行态：Provider 的执行方式 */
export const RUNTIME_KINDS: Record<string, GlossaryEntry> = {
  native_cms: {
    label: 'JSON CMS',
    title: '标准 CMS JSON 直连',
    desc: '苹果 CMS 风格 JSON 接口，服务端直接请求解析，无需 JS / JAR 运行时，最稳定。',
  },
  js_node_drpy: {
    label: 'DRPY JS',
    title: 'DRPY JavaScript 运行时',
    desc: '依赖 Node JS 运行时执行 drpy 脚本取数，能力强但更易受脚本/网络影响。',
  },
  csp_dex: {
    label: 'CSP JAR',
    title: 'CSP / JAR 运行时',
    desc: '依赖 CSP（dex/jar）运行时加载爬虫类源，兼容 FongMi 生态，开销最高。',
  },
}

export function runtimeKindLabel(value: string): string {
  return RUNTIME_KINDS[value]?.label || value
}

/** 健康分项：探活会逐项标记可用性，对应表格状态列的 R / H / C / S / P 徽章 */
export interface HealthFacet extends GlossaryEntry {
  /** 单字母代号 */
  code: string
  /** 对应 SourceProviderHealthSummary 字段名 */
  field: string
}

export const HEALTH_FACETS: HealthFacet[] = [
  { code: 'R', field: 'runtime_status', label: 'R', title: '运行时', desc: 'Provider 运行时能否正常加载与初始化（脚本/JAR 是否就绪）。' },
  { code: 'H', field: 'home_status', label: 'H', title: '首页', desc: '首页内容（homeContent / homeVideoContent）能否取到数据。' },
  { code: 'C', field: 'category_status', label: 'C', title: '分类', desc: '分类列表（categoryContent）能否取到数据。' },
  { code: 'S', field: 'search_status', label: 'S', title: '搜索', desc: '搜索接口能否返回结果。' },
  { code: 'P', field: 'play_ready_status', label: 'P', title: '可播放', desc: '详情 / 播放地址是否就绪（能否解析出可播链接）。' },
]

/** 健康/探活状态值的中文释义与配色 */
export const HEALTH_STATUS: Record<string, GlossaryEntry & { type: TagType }> = {
  ok: { label: '正常', title: '正常 ok', desc: '该项探活通过，可正常使用。', type: 'success' },
  partial: { label: '部分可用', title: '部分可用 partial', desc: '部分子项可用（如首页空但搜索正常），不一定代表源坏。', type: 'warning' },
  error: { label: '失败', title: '失败 error', desc: '该项探活失败，通常不可用。', type: 'error' },
  unhealthy: { label: '不健康', title: '不健康 unhealthy', desc: '连续失败被标记为不健康，建议停用或重新探活。', type: 'error' },
  skipped: { label: '已跳过', title: '已跳过 skipped', desc: '本次未探测该项（如运行时未就绪而跳过下游检查）。', type: 'default' },
  unknown: { label: '未探活', title: '未探活 unknown', desc: '尚未探活或无数据，点击“探活”获取状态。', type: 'default' },
}

export function healthStatusType(status?: string): TagType {
  return HEALTH_STATUS[status || 'unknown']?.type || 'info'
}

export function healthStatusTitle(status?: string): string {
  const entry = HEALTH_STATUS[status || 'unknown']
  return entry ? `${entry.title}：${entry.desc}` : status || 'unknown'
}

/** 能力标签：站点声明 / 探测到的功能特性 */
export const CAPABILITY_GLOSSARY: Record<string, GlossaryEntry> = {
  quick_search: { label: '快搜', title: '快速搜索 quickSearch', desc: '支持快速搜索接口，聚合搜索时响应更快。' },
  quick_search_off: { label: '禁快搜', title: '关闭快速搜索', desc: '该站点显式关闭了快搜，聚合搜索会走普通搜索。' },
  filter: { label: '筛选', title: '分类筛选 filter', desc: '提供分类下的筛选条件（地区 / 年份 / 类型等）。' },
  hidden: { label: '隐藏', title: '隐藏站点 hide=1', desc: 'TVBox 配置标记为隐藏，默认不在列表展示，可勾选“显示隐藏站点”查看。' },
  header: { label: 'Header', title: '自定义请求头', desc: '请求需携带指定请求头（如 User-Agent / Referer）才能正常取数。' },
  play_url: { label: 'playUrl', title: '直链播放字段', desc: '配置包含 playUrl 直链字段，部分内容可直接播放。' },
  click: { label: 'click', title: '点击嗅探规则', desc: '配置包含 click 嗅探规则，依赖运行时模拟点击取流。' },
  categories: { label: '白名单', title: '分类白名单', desc: '限定了可用分类，仅收录白名单内的分类内容。' },
}

/** 在线虚拟库维度释义 */
export const DIMENSION_GLOSSARY: Record<string, GlossaryEntry> = {
  normalized_kind: { label: '内容类型', title: '内容类型 normalized_kind', desc: '按规整后的内容类型聚合，如 movie / tv / anime。' },
  region: { label: '地区', title: '地区 region', desc: '按内容地区聚合，如 CN / JP / US / KR。' },
  kind_region: { label: '类型/地区', title: '类型+地区 kind_region', desc: '类型与地区组合聚合，写法 movie/CN、tv/JP。' },
  provider: { label: 'Provider', title: '站点 provider', desc: '按来源站点聚合，主匹配值填 Provider 的数字 ID。' },
  custom: { label: '自定义', title: '自定义 custom', desc: '自定义维度，按配置中的自定义键聚合。' },
}
