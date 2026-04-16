<script setup lang="ts">
import { computed } from 'vue'

/**
 * 纯 SVG 迷你折线图，用于 KPI 卡片内的趋势展示。
 * - 自动归一化数值到 viewBox
 * - 填充区 + 折线双绘制，折线色=color，填充色=同色 18% 透明度
 * - 数据为空或全部相同时渲染虚线 placeholder
 * - 默认 80×24，通过 props 覆盖
 */

const props = withDefaults(
  defineProps<{
    data: number[]
    color?: string
    width?: number
    height?: number
    strokeWidth?: number
  }>(),
  {
    color: 'currentColor',
    width: 80,
    height: 24,
    strokeWidth: 1.5,
  },
)

const viewBox = computed(() => `0 0 ${props.width} ${props.height}`)

const hasData = computed(() => {
  if (!Array.isArray(props.data) || props.data.length < 2) return false
  const max = Math.max(...props.data)
  const min = Math.min(...props.data)
  return max > min || max > 0  // 有变化或至少非全 0
})

const points = computed(() => {
  if (!hasData.value) return [] as { x: number; y: number }[]
  const d = props.data
  const w = props.width
  const h = props.height
  const pad = 2  // 上下各留 2px 避免紧贴边
  const max = Math.max(...d)
  const min = Math.min(...d)
  const range = max - min || 1
  const stepX = d.length > 1 ? w / (d.length - 1) : 0
  return d.map((v, i) => ({
    x: i * stepX,
    y: h - pad - ((v - min) / range) * (h - pad * 2),
  }))
})

const linePath = computed(() => {
  if (points.value.length === 0) return ''
  return points.value.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' ')
})

const areaPath = computed(() => {
  if (points.value.length === 0) return ''
  const line = linePath.value
  const last = points.value[points.value.length - 1]
  const first = points.value[0]
  return `${line} L ${last.x.toFixed(2)} ${props.height} L ${first.x.toFixed(2)} ${props.height} Z`
})
</script>

<template>
  <svg
    :width="width"
    :height="height"
    :viewBox="viewBox"
    preserveAspectRatio="none"
    class="mini-sparkline"
    :style="{ color }"
  >
    <template v-if="hasData">
      <path :d="areaPath" fill="currentColor" fill-opacity="0.18" />
      <path :d="linePath" stroke="currentColor" :stroke-width="strokeWidth" fill="none" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <line
      v-else
      :x1="0"
      :x2="width"
      :y1="height / 2"
      :y2="height / 2"
      stroke="currentColor"
      stroke-opacity="0.25"
      :stroke-width="1"
      stroke-dasharray="2 3"
    />
  </svg>
</template>

<style scoped>
.mini-sparkline {
  display: block;
  overflow: visible;
}
</style>
