<script setup lang="ts">
import { NButton, NIcon } from 'naive-ui'
import { ArrowForwardOutline } from '@vicons/ionicons5'
import MiniSparkline from '@/components/MiniSparkline.vue'
import type { OverviewKpiItem } from '../types'

defineProps<{
  title: string
  icon: any
  detailRoute?: string
  items: OverviewKpiItem[]
}>()

const emit = defineEmits<{
  navigate: [routeName: string]
}>()
</script>

<template>
  <section class="kpi-group">
    <header class="kpi-group-head">
      <span class="kpi-group-title">
        <n-icon :component="icon" :size="15" />
        {{ title }}
      </span>
      <n-button
        v-if="detailRoute"
        text
        size="tiny"
        class="kpi-group-more"
        @click="emit('navigate', detailRoute)"
      >
        查看详情
        <template #icon><n-icon :component="ArrowForwardOutline" /></template>
      </n-button>
    </header>
    <div class="kpi-row">
      <div
        v-for="item in items"
        :key="item.key"
        class="kpi-cell"
        :class="{ 'kpi-clickable': item.routeName }"
        :data-type="item.type"
        @click="item.routeName && emit('navigate', item.routeName)"
      >
        <div class="kpi-head">
          <span class="kpi-title">
            {{ item.title }}
            <span v-if="item.live" class="live-pill">LIVE</span>
          </span>
          <span class="kpi-icon-box"><n-icon :component="item.icon" :size="16" /></span>
        </div>
        <div class="kpi-value">
          {{ item.value }}<span v-if="item.valueSub" class="kpi-value-sub">{{ item.valueSub }}</span>
        </div>
        <div class="kpi-foot">
          <mini-sparkline
            v-if="item.sparkline"
            :data="item.sparkline"
            :color="item.sparklineColor || 'var(--app-primary)'"
            :width="76"
            :height="22"
          />
          <span v-if="item.hint" class="kpi-hint">{{ item.hint }}</span>
        </div>
        <span class="kpi-bg-icon"><n-icon :component="item.icon" /></span>
      </div>
    </div>
  </section>
</template>
