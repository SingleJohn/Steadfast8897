// 浏览器直出兼容性判定:按「容器 + 视频编码 + 音频编码」综合判断当前浏览器能否直接播放。
// 从 PlayerPage 抽出,供播放页与详情页(外部播放器入口)共用,避免重复维护。

export interface SupportSource {
  Container?: string
  Protocol?: string
  IsRemote?: boolean
  FymsVideoCodec?: string
  FymsAudioCodec?: string
}

function normalizeCodec(value?: string): string {
  return (value || '').trim().toLowerCase()
}

// 'no' = 确定不支持(拦截) / 'ok' = 浏览器可解 / 'unknown' = 交给浏览器尝试。
function classifyVideoCodec(c: string): 'ok' | 'no' | 'unknown' {
  if (!c) return 'unknown'
  if (/h264|avc|x264/.test(c)) return 'ok'
  if (/vp0?8|vp0?9/.test(c)) return 'ok'
  if (/av0?1/.test(c)) return 'ok' // 现代浏览器普遍支持 AV1 解码
  if (/hevc|h265|x265|265|vc-?1|mpeg-?2|mpeg2|wmv|divx|xvid|mpeg-?4|msmpeg/.test(c)) return 'no'
  return 'unknown'
}

function classifyAudioCodec(c: string): 'ok' | 'no' | 'unknown' {
  if (!c) return 'unknown'
  if (/aac|mp3|mp2|opus|vorbis|flac|alac|pcm/.test(c)) return 'ok'
  if (/eac-?3|ac-?3|ac3|dts|truehd|mlp/.test(c)) return 'no'
  return 'unknown'
}

// 'no' = 浏览器无法解封装(拦截) / 'attempt' = 让浏览器尝试(含 mkv 与未知)。
function classifyContainer(c: string): 'ok' | 'no' | 'attempt' {
  if (!c) return 'attempt'
  if (/mp4|m4v|mov|webm|ogg|ogv/.test(c)) return 'ok'
  if (/mkv|matroska/.test(c)) return 'attempt' // Chromium 多数能直放 h264+aac 的 mkv
  if (/m2ts|mts|\bts\b|avi|wmv|asf|flv|rmvb|\brm\b|vob|3gp/.test(c)) return 'no'
  return 'attempt'
}

// resolveUnsupportedReason 返回非空字符串表示「浏览器无法直出」及原因;空字符串表示可尝试直出。
export function resolveUnsupportedReason(source: SupportSource | null): string {
  if (!source) return ''
  const container = normalizeCodec(source.Container)
  const videoCodec = normalizeCodec(source.FymsVideoCodec)
  const audioCodec = normalizeCodec(source.FymsAudioCodec)

  if (container === 'm3u8' || container === 'm3u') {
    if (!(source.IsRemote || normalizeCodec(source.Protocol) === 'http')) {
      return '当前版本仅支持远端 HLS(m3u8) 直链播放，本地 HLS 播单暂不支持。'
    }
    return ''
  }

  // 编码层优先:确定不支持的编码,容器再友好也放不了。
  if (classifyVideoCodec(videoCodec) === 'no') {
    return `当前浏览器无法解码该视频编码(${videoCodec.toUpperCase()})，建议使用外部播放器。`
  }
  if (classifyAudioCodec(audioCodec) === 'no') {
    return `当前浏览器无法解码该音频编码(${audioCodec.toUpperCase()})，建议使用外部播放器。`
  }
  // 容器层:仅拦确定无法解封装的;mkv / 未知编码放行让浏览器尝试,失败由运行时兜底。
  if (classifyContainer(container) === 'no') {
    return `当前浏览器无法解封装该容器(${container.toUpperCase()})，建议使用外部播放器。`
  }
  return ''
}

// canBrowserPlay 是 resolveUnsupportedReason 的布尔便捷封装。
export function canBrowserPlay(source: SupportSource | null): boolean {
  return resolveUnsupportedReason(source) === ''
}
