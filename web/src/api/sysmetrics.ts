import { requestJson } from './client'

export interface SysMetricsSnapshot {
  env: string           // 'windows' | 'linux' | 'docker'
  cpuPercent: number    // 0-100
  cpuCores: number
  memUsed: number       // bytes
  memTotal: number      // bytes（容器限额时为限额）
  memPercent: number
  directTxBps: number   // 本进程 HTTP 出口 bytes/sec
  redirectBpsEst: number // 302 转发估算 bytes/sec
  activeSessions: number
  ts: number            // unix ms
}

export interface SysMetricsHistory {
  current: SysMetricsSnapshot
  history: SysMetricsSnapshot[]
}

export async function getSystemMetrics() {
  return requestJson<SysMetricsHistory>('/System/Metrics')
}

// EventSource 不支持自定义 header，token 通过 query 传递。
export function systemMetricsStreamUrl(token: string | null): string {
  const base = '/System/Metrics/stream'
  return token ? `${base}?api_key=${encodeURIComponent(token)}` : base
}
