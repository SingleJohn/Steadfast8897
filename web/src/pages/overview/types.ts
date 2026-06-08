import type { Component } from 'vue'

export type UpdateChannel = 'stable' | 'nightly'

export type ScanProgressItem = {
  LibraryId: string
  LibraryName: string
  Status: string
  TotalItems: number
  ProcessedItems: number
  Percentage: number
  CurrentItem?: string
  StartedAt: number
  CompletedAt: number
  Error?: string
}

export type OverviewKpiItem = {
  key: string
  title: string
  value: string | number
  valueSub?: string
  hint?: string
  type: 'primary' | 'success' | 'warning' | 'info'
  icon: Component
  routeName?: string
  sparkline?: number[]
  sparklineColor?: string
  live?: boolean
}
