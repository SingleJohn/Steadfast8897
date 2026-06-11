// 外部播放器唤起:把 FYMS 绝对直链转成各播放器的自定义 scheme,用 window.open 唤起。
// scheme 参考 greasyfork embyLaunchPotplayer(@bpking),并修正 query 型 scheme 的 url 编码。

export type PlayerOS = 'windows' | 'macos' | 'ios' | 'android' | 'linux' | 'other'

export function detectOS(): PlayerOS {
  const u = navigator.userAgent || ''
  if (/iPhone|iPad|iPod/i.test(u)) return 'ios'
  if (/Android/i.test(u)) return 'android'
  if (/Macintosh|Mac OS X/i.test(u)) return 'macos'
  if (/Windows/i.test(u)) return 'windows'
  if (/Linux|Ubuntu|X11/i.test(u)) return 'linux'
  return 'other'
}

export interface LaunchContext {
  streamUrl: string // 绝对直链(含 api_key)
  subUrl?: string // 绝对外挂字幕直链(可空)
  title?: string
  positionMs?: number // 续播位置(毫秒)
}

export interface ExternalPlayer {
  id: string
  name: string
  os: PlayerOS[] // 适配平台(用于按当前系统过滤)
  build: (ctx: LaunchContext, os: PlayerOS) => string
}

// 续播位置转 HH:MM:SS(PotPlayer /seek 用)。
function seekHMS(ms = 0): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(Math.floor(s / 3600))}:${p(Math.floor((s % 3600) / 60))}:${p(s % 60)}`
}

// 前缀型 scheme(potplayer://、nplayer-、vlc://、intent:)整段直链作为参数,用 encodeURI 保持可读且可用。
// query 型 scheme(?url=)必须 encodeURIComponent,否则直链里的 & 会把外层参数截断。
const enc = encodeURI
const encq = encodeURIComponent

export const EXTERNAL_PLAYERS: ExternalPlayer[] = [
  {
    id: 'potplayer', name: 'PotPlayer', os: ['windows'],
    build: (c) =>
      `potplayer://${enc(c.streamUrl)}` +
      (c.subUrl ? ` /sub="${enc(c.subUrl)}"` : '') +
      ` /current` +
      (c.title ? ` /title="${enc(c.title)}"` : '') +
      (c.positionMs ? ` /seek=${seekHMS(c.positionMs)}` : ''),
  },
  {
    id: 'vlc', name: 'VLC', os: ['windows', 'macos', 'linux', 'ios', 'android'],
    build: (c, os) => {
      if (os === 'ios') {
        return `vlc-x-callback://x-callback-url/stream?url=${encq(c.streamUrl)}` +
          (c.subUrl ? `&sub=${encq(c.subUrl)}` : '')
      }
      if (os === 'android') {
        return `intent:${enc(c.streamUrl)}#Intent;package=org.videolan.vlc;type=video/*;` +
          (c.subUrl ? `S.subtitles_location=${enc(c.subUrl)};` : '') +
          (c.title ? `S.title=${enc(c.title)};` : '') +
          (c.positionMs ? `i.position=${c.positionMs};` : '') +
          'end'
      }
      // Windows/macOS/Linux 需安装 vlc-protocol 处理器(github.com/stefansundin/vlc-protocol)。
      return `vlc://${enc(c.streamUrl)}`
    },
  },
  {
    id: 'infuse', name: 'Infuse', os: ['ios', 'macos'],
    build: (c) => `infuse://x-callback-url/play?url=${encq(c.streamUrl)}`,
  },
  {
    id: 'iina', name: 'IINA', os: ['macos'],
    build: (c) => `iina://weblink?url=${encq(c.streamUrl)}&new_window=1`,
  },
  {
    id: 'nplayer', name: 'nPlayer', os: ['ios', 'android', 'windows', 'macos'],
    build: (c) => `nplayer-${enc(c.streamUrl)}`,
  },
  {
    id: 'mxplayer', name: 'MX Player', os: ['android'],
    build: (c) =>
      `intent:${enc(c.streamUrl)}#Intent;package=com.mxtech.videoplayer.ad;` +
      (c.title ? `S.title=${enc(c.title)};` : '') +
      (c.positionMs ? `i.position=${c.positionMs};` : '') +
      'end',
  },
]

// playersForOS 返回适配指定系统的播放器;os 传 null 返回全部。
export function playersForOS(os: PlayerOS | null): ExternalPlayer[] {
  if (!os) return EXTERNAL_PLAYERS
  return EXTERNAL_PLAYERS.filter((p) => p.os.includes(os))
}

export function launchExternal(player: ExternalPlayer, ctx: LaunchContext, os: PlayerOS): void {
  window.open(player.build(ctx, os), '_blank')
}

// copyText 优先用 clipboard API(需 https),回退 execCommand(兼容 http/内网)。
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 回退到 execCommand
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
