<script setup lang="ts">
import { NDataTable, NGrid, NGridItem } from 'naive-ui'

import PageSectionCard from '@/components/PageSectionCard.vue'
import type { IPStatsSummary } from '@/types'

defineProps<{
  summary: IPStatsSummary | null
  loading: boolean
}>()
</script>

<template>
  <n-grid cols="1 m:2" x-gap="16" y-gap="16" responsive="screen">
    <n-grid-item>
      <page-section-card title="Top IPs">
        <n-data-table
          :columns="[
            { title: 'IP', key: 'client_ip', width: 150, ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: '归属地', key: 'geo_location', ellipsis: { tooltip: true }, render: (row: any) => [row.country, row.prov, row.city, row.area].filter((v: string) => v && v !== '未知').join(' - ') || '未知' },
          ]"
          :data="summary?.top_ips || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="地区分布占比">
        <n-data-table
          :columns="[
            { title: '大区', key: 'big_area', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_big_area || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="国家/地区占比">
        <n-data-table
          :columns="[
            { title: '国家/地区', key: 'country', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_country || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="省份占比">
        <n-data-table
          :columns="[
            { title: '国家', key: 'country', width: 110, ellipsis: { tooltip: true } },
            { title: '省', key: 'prov', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_prov || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="城市占比">
        <n-data-table
          :columns="[
            { title: '国家', key: 'country', width: 110, ellipsis: { tooltip: true } },
            { title: '省', key: 'prov', width: 110, ellipsis: { tooltip: true } },
            { title: '市', key: 'city', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_city || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="区县占比">
        <n-data-table
          :columns="[
            { title: '国家', key: 'country', width: 110, ellipsis: { tooltip: true } },
            { title: '省', key: 'prov', width: 110, ellipsis: { tooltip: true } },
            { title: '市', key: 'city', width: 110, ellipsis: { tooltip: true } },
            { title: '区县', key: 'area', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_area || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="ISP 占比">
        <n-data-table
          :columns="[
            { title: 'ISP', key: 'isp', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_isp || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="IP 类型占比">
        <n-data-table
          :columns="[
            { title: 'Type', key: 'ip_type', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 110, align: 'right' },
            { title: 'Percent', key: 'percent', width: 110, align: 'right', render: (row: any) => `${Number(row.percent || 0).toFixed(2)}%` },
          ]"
          :data="summary?.by_ip_type || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="360"
        />
      </page-section-card>
    </n-grid-item>
  </n-grid>
</template>
