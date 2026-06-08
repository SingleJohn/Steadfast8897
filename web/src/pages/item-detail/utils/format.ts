export function formatFileSize(bytes: number | null | undefined) {
  if (!bytes) return ''
  const gb = bytes / 1024 / 1024 / 1024
  return gb >= 1 ? `${gb.toFixed(2)} GB` : `${(bytes / 1024 / 1024).toFixed(0)} MB`
}

export function formatBitrate(bps: number | null | undefined): string {
  if (!bps || bps <= 0) return ''
  const mbps = bps / 1_000_000
  if (mbps >= 1) return `${mbps.toFixed(mbps >= 10 ? 0 : 1)} Mbps`
  const kbps = bps / 1000
  return `${Math.round(kbps)} Kbps`
}

export function streamTypeLabel(type: string): string {
  if (type === 'Video') return '视频'
  if (type === 'Audio') return '音频'
  if (type === 'Subtitle') return '字幕'
  return type
}

export function formatStream(s: any): string {
  if (s?.DisplayTitle) return s.DisplayTitle
  const parts: string[] = []
  if (s?.Codec) parts.push(String(s.Codec).toUpperCase())
  if (s?.Type === 'Video') {
    if (s.Width && s.Height) parts.push(`${s.Width}×${s.Height}`)
    if (s.BitDepth) parts.push(`${s.BitDepth}-bit`)
    if (s.PixelFormat) parts.push(s.PixelFormat)
    if (s.BitRate) parts.push(formatBitrate(s.BitRate))
  } else if (s?.Type === 'Audio') {
    if (s.Channels) parts.push(`${s.Channels} 声道`)
    if (s.SampleRate) parts.push(`${Math.round(s.SampleRate / 1000)} kHz`)
    if (s.Language) parts.push(s.Language)
    if (s.BitRate) parts.push(formatBitrate(s.BitRate))
  } else if (s?.Type === 'Subtitle') {
    if (s.Language) parts.push(s.Language)
    if (s.Title) parts.push(s.Title)
  }
  return parts.join(' · ')
}

export function groupedStreams(src: any): { type: string; streams: any[] }[] {
  const streams = (src?.MediaStreams || []) as any[]
  const order = ['Video', 'Audio', 'Subtitle']
  return order
    .map((type) => ({ type, streams: streams.filter((s) => s.Type === type) }))
    .filter((g) => g.streams.length > 0)
}

export function formatRuntime(ticks: number): string {
  const min = Math.round(ticks / 10_000_000 / 60)
  const hr = Math.floor(min / 60)
  const m = min % 60
  if (hr > 0 && m > 0) return `${hr}小时${m}分钟`
  if (hr > 0) return `${hr}小时`
  return `${m}分钟`
}

export function endTimeStr(ticks: number): string {
  const mins = Math.round(ticks / 10_000_000 / 60)
  const end = new Date(Date.now() + mins * 60000)
  return `${end.getHours().toString().padStart(2, '0')}:${end.getMinutes().toString().padStart(2, '0')}`
}
