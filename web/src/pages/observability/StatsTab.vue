<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { NCard, NSpin, NButton, NIcon } from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import { useToast } from '@/composables/useToast'
import { useUiStore } from '@/stores/ui'
import EmptyState from '@/components/EmptyState.vue'
import {
  getUserActivity,
  getPlayActivity,
  getBreakdownReport,
} from '@/api/client'

import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, PieChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
} from 'echarts/components'

use([CanvasRenderer, BarChart, PieChart, GridComponent, TooltipComponent, LegendComponent])

const ui = useUiStore()
const { showToast } = useToast()

const statsUserActivity = ref<any[]>([])
const statsPlayActivity = ref<any[]>([])
const statsTypeBreakdown = ref<any[]>([])
const statsClientBreakdown = ref<any[]>([])
const statsLoaded = ref(false)
const statsLoading = ref(false)

async function loadStats() {
  statsLoading.value = true
  try {
    const [ua, pa, tb, cb] = await Promise.allSettled([
      getUserActivity(30),
      getPlayActivity(30),
      getBreakdownReport('ItemType', 30),
      getBreakdownReport('ClientName', 30),
    ])
    statsUserActivity.value = ua.status === 'fulfilled' && Array.isArray(ua.value) ? ua.value : []
    statsPlayActivity.value = pa.status === 'fulfilled' && Array.isArray(pa.value) ? pa.value : []
    statsTypeBreakdown.value = tb.status === 'fulfilled' && Array.isArray(tb.value) ? tb.value : []
    statsClientBreakdown.value = cb.status === 'fulfilled' && Array.isArray(cb.value) ? cb.value : []
    statsLoaded.value = true
  } catch {
    showToast('加载统计数据失败', 'error')
  } finally {
    statsLoading.value = false
  }
}

const trendOption = computed(() => {
  const dark = ui.isDark
  const textColor = dark ? '#94a3b8' : '#64748b'
  const borderColor = dark ? 'rgba(148,163,184,0.12)' : 'rgba(148,163,184,0.2)'
  const data = statsPlayActivity.value

  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: dark ? 'rgba(15,23,42,0.9)' : 'rgba(255,255,255,0.95)',
      borderColor,
      textStyle: { color: dark ? '#e2e8f0' : '#1e293b', fontSize: 13 },
      formatter: (params: any) => {
        const p = params[0]
        return `${p.name}<br/>播放 <b>${p.value}</b> 次`
      },
    },
    grid: { left: 48, right: 16, top: 16, bottom: 36 },
    xAxis: {
      type: 'category',
      data: data.map((d: any) => d.date?.slice(5) || ''),
      axisLine: { lineStyle: { color: borderColor } },
      axisLabel: { color: textColor, fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: borderColor } },
      axisLabel: { color: textColor, fontSize: 11 },
    },
    series: [
      {
        type: 'bar',
        data: data.map((d: any) => d.count || 0),
        itemStyle: {
          color: dark
            ? { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(16,185,129,0.85)' }, { offset: 1, color: 'rgba(16,185,129,0.35)' }] }
            : 'rgba(16,185,129,0.75)',
          borderRadius: [4, 4, 0, 0],
        },
        barMaxWidth: 24,
        emphasis: { itemStyle: { color: '#10b981' } },
      },
    ],
  }
})

const BREAKDOWN_COLORS = ['#10b981', '#3b82f6', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316']

function pieOption(items: any[]) {
  const dark = ui.isDark
  return {
    tooltip: {
      trigger: 'item',
      backgroundColor: dark ? 'rgba(15,23,42,0.9)' : 'rgba(255,255,255,0.95)',
      borderColor: dark ? 'rgba(148,163,184,0.12)' : 'rgba(148,163,184,0.2)',
      textStyle: { color: dark ? '#e2e8f0' : '#1e293b', fontSize: 13 },
      formatter: (p: any) => `${p.name}<br/><b>${p.value}</b> 次 (${p.percent}%)`,
    },
    series: [
      {
        type: 'pie',
        radius: ['45%', '72%'],
        center: ['50%', '50%'],
        avoidLabelOverlap: true,
        itemStyle: { borderRadius: 6, borderColor: dark ? '#0f172a' : '#f8fafc', borderWidth: 2 },
        label: { show: true, color: dark ? '#94a3b8' : '#64748b', fontSize: 12 },
        emphasis: { label: { fontSize: 14, fontWeight: 'bold' } },
        data: items.map((r: any, i: number) => ({
          name: r.label || 'Unknown',
          value: r.count || 0,
          itemStyle: { color: BREAKDOWN_COLORS[i % BREAKDOWN_COLORS.length] },
        })),
      },
    ],
  }
}

const typePieOption = computed(() => pieOption(statsTypeBreakdown.value))
const clientPieOption = computed(() => pieOption(statsClientBreakdown.value))

function durationStr(minutes: number) {
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
  return `${minutes}m`
}

onMounted(() => {
  void loadStats()
})
</script>

<template>
  <div class="stats-tab">
    <!-- Loading / empty -->
    <div v-if="statsLoading && !statsLoaded" class="stats-loading">
      <n-spin size="medium" />
      <span>加载统计数据...</span>
    </div>

    <template v-else-if="statsLoaded">
      <!-- Trend chart -->
      <n-card class="glass-card section-card" :bordered="false">
        <template #header>
          <div class="section-header">
            <span class="section-title">播放趋势</span>
            <span class="section-sub">近 30 天</span>
          </div>
        </template>
        <template #header-extra>
          <n-button text size="small" :loading="statsLoading" @click="loadStats">
            <template #icon><n-icon :component="RefreshOutline" /></template>
          </n-button>
        </template>
        <v-chart
          v-if="statsPlayActivity.length > 0"
          :option="trendOption"
          autoresize
          style="height: 220px"
        />
        <empty-state v-else description="暂无播放数据" />
      </n-card>

      <!-- Breakdown row -->
      <div class="breakdown-grid">
        <n-card class="glass-card section-card" title="按类型" :bordered="false">
          <v-chart
            v-if="statsTypeBreakdown.length > 0"
            :option="typePieOption"
            autoresize
            style="height: 220px"
          />
          <empty-state v-else description="无数据" />
        </n-card>

        <n-card class="glass-card section-card" title="按客户端" :bordered="false">
          <v-chart
            v-if="statsClientBreakdown.length > 0"
            :option="clientPieOption"
            autoresize
            style="height: 220px"
          />
          <empty-state v-else description="无数据" />
        </n-card>
      </div>

      <!-- User activity table -->
      <n-card class="glass-card section-card" title="用户活动排行" :bordered="false">
        <div v-if="statsUserActivity.length > 0" class="user-table-wrap">
          <table class="user-table">
            <thead>
              <tr>
                <th class="user-table__th">#</th>
                <th class="user-table__th">用户</th>
                <th class="user-table__th">最后在线</th>
                <th class="user-table__th user-table__th--right">播放次数</th>
                <th class="user-table__th user-table__th--right">总时长</th>
                <th class="user-table__th">客户端</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(u, i) in statsUserActivity" :key="i" class="user-table__row">
                <td class="user-table__td user-table__td--rank">
                  <span class="rank-badge" :class="{ 'rank-badge--top': i < 3 }">{{ i + 1 }}</span>
                </td>
                <td class="user-table__td user-table__td--name">{{ u.user_name }}</td>
                <td class="user-table__td user-table__td--muted">
                  {{ u.last_seen ? new Date(u.last_seen).toLocaleString() : '-' }}
                </td>
                <td class="user-table__td user-table__td--right">{{ u.total_plays }}</td>
                <td class="user-table__td user-table__td--right">
                  {{ durationStr(Math.round(u.total_play_time / 60)) }}
                </td>
                <td class="user-table__td user-table__td--muted">{{ u.client_name || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <empty-state v-else description="暂无用户活动数据" />
      </n-card>
    </template>

    <template v-else>
      <div class="stats-empty-action">
        <n-button type="primary" :loading="statsLoading" @click="loadStats">
          加载统计数据（近 30 天）
        </n-button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.stats-tab {
  display: flex;
  flex-direction: column;
  gap: var(--app-section-gap);
}

.section-card {
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
}

.section-sub {
  font-size: 13px;
  color: var(--app-text-muted);
  font-weight: 400;
}

.stats-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 60px 20px;
  color: var(--app-text-muted);
  font-size: 14px;
}

.stats-empty-action {
  display: flex;
  justify-content: center;
  padding: 60px 20px;
}

/* Breakdown */
.breakdown-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--app-section-gap);
}

/* User table */
.user-table-wrap {
  overflow-x: auto;
}

.user-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.user-table__th {
  text-align: left;
  padding: 10px 8px;
  font-weight: 500;
  color: var(--app-text-muted);
  border-bottom: 1px solid var(--app-border);
  white-space: nowrap;
}

.user-table__th--right {
  text-align: right;
}

.user-table__row {
  transition: background 0.15s ease;
}

.user-table__row:hover {
  background: rgba(148, 163, 184, 0.04);
}

.user-table__td {
  padding: 10px 8px;
  border-bottom: 1px solid var(--app-border);
  color: var(--app-text);
}

.user-table__td--name {
  font-weight: 500;
}

.user-table__td--muted {
  color: var(--app-text-muted);
}

.user-table__td--right {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.user-table__td--rank {
  width: 40px;
}

.rank-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text-muted);
  background: rgba(148, 163, 184, 0.08);
}

.rank-badge--top {
  color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.12);
}

@media (max-width: 768px) {
  .breakdown-grid {
    grid-template-columns: 1fr;
  }
}
</style>
