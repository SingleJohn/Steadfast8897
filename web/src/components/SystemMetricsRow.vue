<script setup lang="ts">
import { computed } from 'vue'

import MiniSparkline from '@/components/MiniSparkline.vue'
import { useSystemMetrics } from '@/composables/useSystemMetrics'

const { current, history } = useSystemMetrics()

const cpuPct = computed(() => current.value?.cpuPercent ?? 0)
const memPct = computed(() => current.value?.memPercent ?? 0)
const memUsed = computed(() => humanBytes(current.value?.memUsed ?? 0))
const memTotal = computed(() => humanBytes(current.value?.memTotal ?? 0))
const env = computed(() => current.value?.env ?? '-')
const cores = computed(() => current.value?.cpuCores ?? 0)
const activeSessions = computed(() => current.value?.activeSessions ?? 0)

const cpuSeries = computed(() => history.map((s) => s.cpuPercent))
const memSeries = computed(() => history.map((s) => s.memPercent))
const directSeries = computed(() => history.map((s) => s.directTxBps))
const redirectSeries = computed(() => history.map((s) => s.redirectBpsEst))

const directText = computed(() => humanBps(current.value?.directTxBps ?? 0))
const redirectText = computed(() => humanBps(current.value?.redirectBpsEst ?? 0))

const envLabel = computed(() => {
  if (env.value === 'docker') return 'Docker'
  if (env.value === 'windows') return 'Windows'
  if (env.value === 'linux') return 'Linux'
  return env.value || '—'
})

function humanBps(b: number): { value: string; unit: string } {
  if (!Number.isFinite(b) || b <= 0) return { value: '0', unit: 'B/s' }
  if (b < 1024) return { value: b.toFixed(0), unit: 'B/s' }
  if (b < 1024 * 1024) return { value: (b / 1024).toFixed(1), unit: 'KB/s' }
  if (b < 1024 * 1024 * 1024) return { value: (b / (1024 * 1024)).toFixed(1), unit: 'MB/s' }
  return { value: (b / (1024 * 1024 * 1024)).toFixed(2), unit: 'GB/s' }
}

function humanBytes(b: number): string {
  if (!Number.isFinite(b) || b <= 0) return '0'
  const g = b / (1024 * 1024 * 1024)
  if (g >= 1) return g.toFixed(1) + ' GB'
  const m = b / (1024 * 1024)
  return m.toFixed(0) + ' MB'
}
</script>

<template>
  <section class="sysmet-row" id="sysmet-row">
    <!-- CPU -->
    <div class="sysmet-card sysmet-cpu">
      <header class="sysmet-head">
        <span class="sysmet-title">CPU Usage</span>
        <span class="sysmet-chip">{{ envLabel }} · {{ cores }}c</span>
      </header>
      <div class="sysmet-value">
        {{ cpuPct.toFixed(0) }}<span class="sysmet-unit">%</span>
      </div>
      <mini-sparkline
        :data="cpuSeries"
        color="#22c55e"
        :width="260"
        :height="44"
        :stroke-width="2"
      />
    </div>

    <!-- RAM -->
    <div class="sysmet-card sysmet-ram">
      <header class="sysmet-head">
        <span class="sysmet-title">RAM Usage</span>
        <span class="sysmet-chip">{{ memUsed }} / {{ memTotal }}</span>
      </header>
      <div class="sysmet-value">
        {{ memPct.toFixed(0) }}<span class="sysmet-unit">%</span>
      </div>
      <mini-sparkline
        :data="memSeries"
        color="#3b82f6"
        :width="260"
        :height="44"
        :stroke-width="2"
      />
    </div>

    <!-- Network -->
    <div class="sysmet-card sysmet-net">
      <header class="sysmet-head">
        <span class="sysmet-title">Network Bandwidth</span>
        <span class="sysmet-chip">{{ activeSessions }} active</span>
      </header>
      <div class="sysmet-dual">
        <div class="sysmet-line sysmet-line-direct">
          <span class="dot"></span>
          <span class="lbl">Direct</span>
          <span class="val">{{ directText.value }}</span>
          <span class="unit">{{ directText.unit }}</span>
        </div>
        <div class="sysmet-line sysmet-line-redirect">
          <span class="dot"></span>
          <span class="lbl">302 Est</span>
          <span class="val">{{ redirectText.value }}</span>
          <span class="unit">{{ redirectText.unit }}</span>
        </div>
      </div>
      <div class="sysmet-net-spark">
        <mini-sparkline
          :data="directSeries"
          color="#f97316"
          :width="260"
          :height="22"
          :stroke-width="1.8"
        />
        <mini-sparkline
          :data="redirectSeries"
          color="#fbbf24"
          :width="260"
          :height="22"
          :stroke-width="1.8"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.sysmet-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.sysmet-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 18px 12px;
  background: var(--app-card-bg);
  border: 1px solid var(--app-card-border);
  border-radius: 14px;
  box-shadow: var(--app-shadow-1);
  overflow: hidden;
  min-height: 146px;
}

.sysmet-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 20px;
}

.sysmet-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text-muted);
  letter-spacing: 0.02em;
}

.sysmet-chip {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  white-space: nowrap;
}

.sysmet-value {
  font-size: 34px;
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.sysmet-unit {
  margin-left: 2px;
  font-size: 18px;
  font-weight: 600;
  opacity: 0.75;
}

.sysmet-cpu .sysmet-value { color: #22c55e; }
.sysmet-ram .sysmet-value { color: #3b82f6; }

/* Network 双行 */
.sysmet-dual {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 2px;
}

.sysmet-line {
  display: inline-flex;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
}

.sysmet-line .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  transform: translateY(-1px);
}

.sysmet-line-direct .dot { background: #f97316; }
.sysmet-line-redirect .dot { background: #fbbf24; }

.sysmet-line .lbl {
  font-size: 11px;
  color: var(--app-text-muted);
  min-width: 50px;
}

.sysmet-line .val {
  font-size: 18px;
  font-weight: 700;
  color: var(--app-text);
  font-variant-numeric: tabular-nums;
}

.sysmet-line .unit {
  font-size: 11px;
  color: var(--app-text-muted);
}

.sysmet-net-spark {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: auto;
}

.sysmet-net-spark :deep(.mini-sparkline:first-child) { color: #f97316; }
.sysmet-net-spark :deep(.mini-sparkline:last-child) { color: #fbbf24; }

/* sparkline 横向填满卡片,不再被写死的 width=260 约束;
   viewBox 仍用组件 props 的数值作为坐标系,preserveAspectRatio="none" +
   non-scaling-stroke 确保拉伸时线形正确、线粗稳定。 */
.sysmet-card :deep(.mini-sparkline) {
  width: 100%;
}

@media (max-width: 1200px) {
  .sysmet-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .sysmet-net { grid-column: 1 / -1; }
}
@media (max-width: 720px) {
  .sysmet-row { grid-template-columns: 1fr; }
  .sysmet-net { grid-column: auto; }
}
</style>
