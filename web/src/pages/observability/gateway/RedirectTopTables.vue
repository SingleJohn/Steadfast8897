<script setup lang="ts">
import { NDataTable, NGrid, NGridItem } from 'naive-ui'

import PageSectionCard from '@/components/PageSectionCard.vue'
import type { RedirectSummary } from '@/types'

const props = defineProps<{
  summary: RedirectSummary | null
  loading: boolean
}>()

function formatIPLocation(row: any) {
  const info = props.summary?.ip_infos?.[row.client_ip]
  if (!info) return '未知'
  const parts = [info.country, info.prov, info.city, info.area].filter((v: string) => v && v !== '未知')
  return parts.length > 0 ? parts.join(' - ') : '未知'
}
</script>

<template>
  <n-grid cols="1 m:2" x-gap="16" y-gap="16" responsive="screen">
    <n-grid-item>
      <page-section-card title="Top Users">
        <n-data-table
          :columns="[
            { title: 'User', key: 'emby_user_id', ellipsis: { tooltip: true }, render: (row: any) => row.emby_user_name ? `${row.emby_user_name} (${row.emby_user_id})` : row.emby_user_id },
            { title: 'Count', key: 'count', width: 120, align: 'right' },
          ]"
          :data="summary?.top_users || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="260"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="Top User × Backend">
        <n-data-table
          :columns="[
            { title: 'User', key: 'emby_user_id', ellipsis: { tooltip: true }, render: (row: any) => row.emby_user_name ? `${row.emby_user_name} (${row.emby_user_id})` : row.emby_user_id },
            { title: 'Backend', key: 'backend', width: 140, ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 120, align: 'right' },
          ]"
          :data="summary?.top_user_backend || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="260"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="Top IPs">
        <n-data-table
          :columns="[
            { title: 'IP', key: 'client_ip', width: 150, ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 120, align: 'right' },
            { title: '归属地', key: 'geo_location', ellipsis: { tooltip: true }, render: (row: any) => formatIPLocation(row) },
          ]"
          :data="summary?.top_ips || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="260"
        />
      </page-section-card>
    </n-grid-item>
    <n-grid-item>
      <page-section-card title="Top UAs">
        <n-data-table
          :columns="[
            { title: 'UA', key: 'user_agent', ellipsis: { tooltip: true } },
            { title: 'Count', key: 'count', width: 120, align: 'right' },
          ]"
          :data="summary?.top_uas || []"
          :loading="loading"
          :bordered="false"
          size="small"
          :max-height="260"
        />
      </page-section-card>
    </n-grid-item>
  </n-grid>
</template>
