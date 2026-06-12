import { computed } from 'vue'

import type { TaskSnapshot } from '@/api/tasks'
import { useTaskStream } from '@/composables/useTaskStream'

export type LibraryScanStatus = 'idle' | 'scanning' | 'completed' | 'failed' | string

export interface LibraryScanProgress {
  LibraryId: string
  LibraryName: string
  Status: LibraryScanStatus
  TotalItems: number
  ProcessedItems: number
  Percentage: number
  CurrentItem?: string
  StartedAt: number
  CompletedAt: number
  Error?: string
}

function legacyStatus(s: TaskSnapshot['status']): LibraryScanStatus {
  if (s === 'running') return 'scanning'
  if (s === 'succeeded') return 'completed'
  if (s === 'failed') return 'failed'
  return s
}

function childToProgress(c: TaskSnapshot): LibraryScanProgress {
  const libraryId = (c.message ?? '').replace(/^library=/, '')
  return {
    LibraryId: libraryId,
    LibraryName: c.phase ?? '',
    Status: legacyStatus(c.status),
    TotalItems: c.total,
    ProcessedItems: c.processed,
    Percentage: c.percent,
    CurrentItem: c.current ?? undefined,
    StartedAt: c.startedAt ?? 0,
    CompletedAt: c.completedAt ?? 0,
    Error: c.error ?? undefined,
  }
}

export function useLibraryScanState() {
  const { snapshots } = useTaskStream()

  const scanTask = computed(() => snapshots.scan ?? null)
  const scanProgress = computed<LibraryScanProgress[]>(() => {
    const s = scanTask.value
    if (!s?.children) return []
    return s.children.map(childToProgress)
  })

  const isAnyScanning = computed(() => scanProgress.value.some((s) => s.Status === 'scanning'))

  return {
    snapshots,
    scanTask,
    scanProgress,
    isAnyScanning,
  }
}
