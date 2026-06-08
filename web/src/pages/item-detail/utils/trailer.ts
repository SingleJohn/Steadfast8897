import type { TrailerInfo } from '../types'

export function normalizeTrailerIndex(index: number | string): number {
  const value = typeof index === 'number' ? index : Number(index)
  return Number.isFinite(value) ? value : 0
}

export function trailerTabLabel(trailer: TrailerInfo, index: number | string): string {
  return trailer.Name || `预告片 ${normalizeTrailerIndex(index) + 1}`
}

export function canPlayTrailerInline(url: string): boolean {
  const lower = url.split('?')[0].toLowerCase()
  return lower.includes('/videos/') || /\.(mp4|m4v|webm|mov|m3u8|mkv)$/.test(lower)
}
