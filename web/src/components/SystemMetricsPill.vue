<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'

import { useSystemMetrics } from '@/composables/useSystemMetrics'

const { current } = useSystemMetrics()
const router = useRouter()

const cpu = computed(() =>
  current.value ? `${Math.round(current.value.cpuPercent)}%` : '—',
)
const ram = computed(() =>
  current.value ? `${Math.round(current.value.memPercent)}%` : '—',
)
const net = computed(() => {
  const s = current.value
  if (!s) return '—'
  return humanBps(s.directTxBps + s.redirectBpsEst)
})

function humanBps(b: number): string {
  if (!Number.isFinite(b) || b <= 0) return '0 B/s'
  if (b < 1024) return `${b.toFixed(0)} B/s`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB/s`
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB/s`
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB/s`
}

function goDetail() {
  router
    .push({ name: 'admin_overview' })
    .then(() => {
      setTimeout(() => {
        const el = document.getElementById('sysmet-row')
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      }, 50)
    })
    .catch(() => { /* ignore */ })
}
</script>

<template>
  <button class="sysmet-pill" type="button" aria-label="系统资源" @click="goDetail">
    <span class="seg seg-cpu"><span class="dot"></span>CPU {{ cpu }}</span>
    <span class="sep"></span>
    <span class="seg seg-ram"><span class="dot"></span>RAM {{ ram }}</span>
    <span class="sep"></span>
    <span class="seg seg-net"><span class="dot"></span>{{ net }}</span>
  </button>
</template>

<style scoped>
.sysmet-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 12px;
  border: 0;
  border-radius: 999px;
  background: var(--app-surface-2, rgba(128,128,128,0.08));
  color: var(--app-text);
  cursor: pointer;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  transition: background 0.15s;
}

.sysmet-pill:hover { background: var(--app-primary-soft); }

.seg {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-weight: 600;
  white-space: nowrap;
}

.seg .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.seg-cpu .dot { background: #22c55e; }
.seg-ram .dot { background: #3b82f6; }
.seg-net .dot { background: #f97316; }

.sep {
  width: 1px;
  height: 11px;
  background: var(--app-border);
  opacity: 0.55;
}

@media (max-width: 900px) {
  .sysmet-pill { display: none; }
}
</style>
