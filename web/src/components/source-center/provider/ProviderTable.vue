<script setup lang="ts">
import { h } from 'vue'
import { NButton, NDataTable, NIcon, NPopconfirm, NPopover, NSpace, NTag, NTooltip, useMessage } from 'naive-ui'
import type { Component } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import {
  CloudDownloadOutline,
  ConstructOutline,
  GridOutline,
  HomeOutline,
  PowerOutline,
  PulseOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import type { SourceProvider, SourceProviderHealthSummary } from '@/api/source'
import { copyText } from '@/utils/externalPlayers'
import {
  CAPABILITY_GLOSSARY,
  HEALTH_FACETS,
  HEALTH_STATUS,
  RUNTIME_KINDS,
  healthStatusTitle,
  healthStatusType,
  runtimeKindLabel,
  type TagType,
} from '../sourceGlossary'

const props = defineProps<{
  providers: SourceProvider[]
  selectedIds: number[]
  action: string
}>()

const emit = defineEmits<{
  'update:selectedIds': [value: number[]]
  toggle: [id: number, enabled: boolean]
  health: [id: number]
  diagnose: [id: number]
  homeProfile: [id: number]
  categories: [id: number]
  fetchCatalog: [id: number]
  deleteOne: [id: number]
}>()

const message = useMessage()

const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}

function hTag(label: string, type?: TagType) {
  return h(NTag, { size: 'small', type: type === 'default' ? undefined : type }, { default: () => label })
}

// 带 hover 说明的标签
function hTagTip(label: string, type: TagType, tip: string) {
  const tag = hTag(label, type)
  if (!tip) return tag
  return h(NTooltip, null, { trigger: () => tag, default: () => tip })
}

// 带 hover 说明的纯文本（虚线下划线提示可悬浮）
function hTextTip(text: string, tip: string) {
  if (!tip) return text
  return h(NTooltip, null, {
    trigger: () => h('span', { class: 'cell-hint' }, text),
    default: () => tip,
  })
}

// 列头：标题 + 角标（hover 出说明 / 图例）
function hHeaderTip(title: string, content: any, badge = '?') {
  return h('span', { class: 'col-head' }, [
    title,
    h(NPopover, { trigger: 'hover', placement: 'bottom-start', style: 'max-width:300px' }, {
      trigger: () => h('span', { class: 'col-info', 'aria-label': `${title}说明` }, badge),
      default: () => content,
    }),
  ])
}

function legendList(rows: Array<{ head: string; desc: string }>) {
  return h('div', { class: 'legend' }, rows.map((row) => h('div', { class: 'legend-row' }, [
    h('strong', row.head),
    h('span', row.desc),
  ])))
}

function runtimeLegend() {
  return legendList(Object.values(RUNTIME_KINDS).map((entry) => ({ head: entry.label, desc: entry.desc })))
}

function statusLegend() {
  return h('div', { class: 'legend' }, [
    h('div', { class: 'legend-title' }, '分项（探活逐项标记）'),
    ...HEALTH_FACETS.map((facet) => h('div', { class: 'legend-row' }, [
      h('strong', `${facet.code} · ${facet.title}`),
      h('span', facet.desc),
    ])),
    h('div', { class: 'legend-title' }, '状态值'),
    ...Object.values(HEALTH_STATUS).map((status) => h('div', { class: 'legend-row' }, [
      h('strong', status.title),
      h('span', status.desc),
    ])),
  ])
}

function capabilityLegend() {
  return legendList(Object.values(CAPABILITY_GLOSSARY).map((entry) => ({ head: entry.label, desc: entry.desc })))
}

function hHealthTags(health?: SourceProviderHealthSummary) {
  const tags = HEALTH_FACETS
    .map((facet) => [facet, (health as Record<string, string> | undefined)?.[facet.field]] as const)
    .filter((item): item is [typeof HEALTH_FACETS[number], string] => !!item[1])
  if (tags.length === 0) {
    return h('span', { class: 'health-empty' }, '未分项')
  }
  return h(NSpace, { size: 3 }, {
    default: () => tags.map(([facet, status]) => {
      const st = HEALTH_STATUS[status]
      const tip = `${facet.title}（${facet.field}）· ${st ? st.title : status}：${facet.desc}`
      return hTagTip(`${facet.code}:${status}`, healthStatusType(status), tip)
    }),
  })
}

function capTip(key: string) {
  const entry = CAPABILITY_GLOSSARY[key]
  return entry ? `${entry.title}：${entry.desc}` : ''
}

function hCapabilityTags(row: SourceProvider) {
  const caps = row.Capabilities
  if (!caps) return '-'
  const tags: Array<[string, TagType, string]> = []
  if (caps.quick_search === true) tags.push(['快搜', 'success', capTip('quick_search')])
  else if (caps.quick_search === false) tags.push(['禁快搜', 'warning', capTip('quick_search_off')])
  if (caps.filter) tags.push(['筛选', 'info', capTip('filter')])
  if (!row.Visible) tags.push(['隐藏', 'warning', capTip('hidden')])
  if (caps.header_present) tags.push([`Header:${(caps.header_keys || []).join(',') || 'yes'}`, 'info', capTip('header')])
  if (caps.play_url_present) tags.push(['playUrl', 'default', capTip('play_url')])
  if (caps.click_present) tags.push(['click', 'default', capTip('click')])
  if (Array.isArray(caps.categories) && caps.categories.length > 0) tags.push([`白名单:${caps.categories.length}`, 'default', capTip('categories')])
  if (tags.length === 0) return '-'
  return h(NSpace, { size: 4 }, {
    default: () => tags.map(([label, type, tip]) => hTagTip(label, type, tip)),
  })
}

// 图标动作按钮：圆形 + hover tooltip，紧凑且统一。
function iconAction(icon: Component, label: string, opts: { type?: 'primary' | 'error'; loading?: boolean; onClick: () => void }) {
  return h(NTooltip, null, {
    trigger: () => h(NButton, {
      size: 'small',
      circle: true,
      quaternary: true,
      type: opts.type,
      loading: opts.loading,
      onClick: opts.onClick,
    }, { icon: () => h(NIcon, null, { default: () => h(icon) }) }),
    default: () => label,
  })
}

function hEnabledToggle(row: SourceProvider) {
  return h(NPopconfirm, {
    positiveText: row.Enabled ? '停用' : '启用',
    negativeText: '取消',
    onPositiveClick: () => emit('toggle', row.ID, !row.Enabled),
  }, {
    trigger: () => h(NTooltip, null, {
      trigger: () => h(NButton, {
        size: 'small',
        circle: true,
        quaternary: true,
        type: row.Enabled ? 'success' : undefined,
        onClick: () => {},
      }, { icon: () => h(NIcon, null, { default: () => h(PowerOutline) }) }),
      default: () => row.Enabled ? '已启用（点击停用）' : '已停用（点击启用）',
    }),
    default: () => `${row.Enabled ? '停用' : '启用'}站点“${row.Name}”？在线虚拟库命中范围会随之变化。`,
  })
}

function hActions(row: SourceProvider) {
  return h(NSpace, { size: 2, wrap: false, align: 'center' }, {
    default: () => [
      iconAction(PulseOutline, '探活', { loading: props.action === `health:${row.ID}`, onClick: () => emit('health', row.ID) }),
      iconAction(ConstructOutline, '兼容诊断', { loading: props.action === `diagnose:${row.ID}`, onClick: () => emit('diagnose', row.ID) }),
      iconAction(HomeOutline, '首页画像', { loading: props.action === `home-profile:${row.ID}`, onClick: () => emit('homeProfile', row.ID) }),
      iconAction(GridOutline, '分类', { loading: props.action === `categories:${row.ID}`, onClick: () => emit('categories', row.ID) }),
      iconAction(CloudDownloadOutline, '抓取入库', { type: 'primary', loading: props.action === `catalog:${row.ID}`, onClick: () => emit('fetchCatalog', row.ID) }),
      h(NPopconfirm, {
        positiveText: '删除',
        negativeText: '取消',
        onPositiveClick: () => emit('deleteOne', row.ID),
      }, {
        trigger: () => h(NTooltip, null, {
          trigger: () => h(NButton, { size: 'small', circle: true, quaternary: true, type: 'error', loading: props.action === 'batch-delete' }, { icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) }),
          default: () => '删除',
        }),
        default: () => `删除 Provider “${row.Name}”？在线缓存条目会级联删除，运行时审计保留脱敏记录。`,
      }),
    ],
  })
}

async function copyProviderError(row: SourceProvider) {
  const text = [
    `Provider: ${row.Name}`,
    `SourceKey: ${row.SourceKey}`,
    `HealthStatus: ${row.HealthStatus || 'unknown'}`,
    `Error: ${row.LastError || '-'}`,
  ].join('\n')
  const ok = await copyText(text)
  if (ok) message.success('Provider 错误已复制')
  else message.error('复制失败，请手动选中')
}

const columns: DataTableColumns<SourceProvider> = [
  { type: 'selection', width: 42 },
  {
    title: '站点',
    key: 'Name',
    minWidth: 220,
    render(row) {
      return h('div', { class: 'provider-name-cell' }, [
        h('strong', row.Name),
        h('span', row.SourceKey),
      ])
    },
  },
  {
    title: () => hHeaderTip('运行态', runtimeLegend()),
    key: 'RuntimeKind',
    width: 130,
    render(row) {
      const entry = RUNTIME_KINDS[row.RuntimeKind]
      return hTextTip(runtimeKindLabel(row.RuntimeKind), entry ? `${entry.title}：${entry.desc}` : '')
    },
  },
  {
    title: () => hHeaderTip('状态', statusLegend(), '图例'),
    key: 'HealthStatus',
    width: 180,
    render(row) {
      return h(NSpace, { size: 4, vertical: true }, {
        default: () => [
          hTagTip(row.HealthStatus || 'unknown', healthStatusType(row.HealthStatus), healthStatusTitle(row.HealthStatus)),
          hHealthTags(row.Health),
        ],
      })
    },
  },
  {
    title: () => hHeaderTip('能力', capabilityLegend()),
    key: 'Capabilities',
    minWidth: 180,
    render(row) {
      return hCapabilityTags(row)
    },
  },
  {
    title: '最近错误',
    key: 'LastError',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render(row) {
      if (!row.LastError) return '-'
      return h('div', { class: 'error-cell' }, [
        h('span', row.LastError),
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          onClick: () => copyProviderError(row),
        }, { default: () => '复制' }),
      ])
    },
  },
  {
    title: '启用',
    key: 'Enabled',
    width: 64,
    align: 'center',
    render(row) {
      return hEnabledToggle(row)
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 196,
    render(row) {
      return hActions(row)
    },
  },
]
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="providers"
    :checked-row-keys="selectedIds"
    :pagination="tablePagination"
    :row-key="(row: SourceProvider) => row.ID"
    :scroll-x="1180"
    size="small"
    :bordered="false"
    @update:checked-row-keys="emit('update:selectedIds', $event as number[])"
  />
</template>

<style scoped>
.provider-name-cell {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.provider-name-cell strong,
.provider-name-cell span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-name-cell span {
  color: var(--app-text-muted);
  font-size: 12px;
}
.health-empty {
  color: var(--app-text-muted);
  font-size: 12px;
}
.col-head {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.col-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 16px;
  height: 16px;
  padding: 0 5px;
  border-radius: 8px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  cursor: help;
}
.col-info:hover {
  color: var(--app-text);
}
.cell-hint {
  border-bottom: 1px dashed var(--app-border);
  cursor: help;
}
.error-cell {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px;
  align-items: center;
}
.error-cell span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

<style>
/* 列头 Popover 图例（teleport 到 body，需非 scoped） */
.legend {
  display: grid;
  gap: 6px;
  max-width: 280px;
  font-size: 12px;
}
.legend-title {
  margin-top: 2px;
  color: var(--app-text-muted);
  font-size: 11px;
  font-weight: 700;
}
.legend-title:first-child {
  margin-top: 0;
}
.legend-row {
  display: grid;
  gap: 1px;
}
.legend-row strong {
  font-size: 12px;
}
.legend-row span {
  color: var(--app-text-muted);
  line-height: 1.4;
}
</style>
