<script setup lang="ts">
import { computed } from 'vue'

/**
 * 画质胶囊组件。
 *
 * 主胶囊按分辨率(4K 紫、1080p 蓝、720p 灰);次胶囊(HDR/DV)金/橙;
 * 可选底部小字展示 source + codec。变体 "compact" 仅展示主胶囊,用于列表卡角标。
 */
const props = withDefaults(
  defineProps<{
    resolution?: string | null
    hdr?: string | null
    source?: string | null
    videoCodec?: string | null
    audioCodec?: string | null
    label?: string | null
    compact?: boolean
  }>(),
  {
    resolution: '',
    hdr: '',
    source: '',
    videoCodec: '',
    audioCodec: '',
    label: '',
    compact: false,
  },
)

const resolutionLabel = computed(() => {
  const r = (props.resolution || '').toLowerCase()
  if (!r || r === 'unknown') return ''
  return { '8k': '8K', '4k': '4K', '1440p': '1440p', '1080p': '1080p', '720p': '720p', sd: 'SD' }[r] || r.toUpperCase()
})
const hdrLabel = computed(() => {
  const h = (props.hdr || '').toLowerCase()
  return { 'hdr10+': 'HDR10+', hdr10: 'HDR', dv: 'DV' }[h] || ''
})
const sourceLabel = computed(() => {
  const s = (props.source || '').toLowerCase()
  return {
    remux: 'Remux',
    bluray: 'BluRay',
    bdrip: 'BDRip',
    'web-dl': 'WEB-DL',
    webrip: 'WEBRip',
    hdtv: 'HDTV',
    dvdrip: 'DVDRip',
  }[s] || ''
})
const videoCodecLabel = computed(() => {
  const c = (props.videoCodec || '').toLowerCase()
  return { x265: 'x265', x264: 'x264', av1: 'AV1' }[c] || ''
})
const audioCodecLabel = computed(() => {
  const a = (props.audioCodec || '').toLowerCase()
  return {
    atmos: 'Atmos',
    truehd: 'TrueHD',
    'dts-hd': 'DTS-HD',
    dts: 'DTS',
    eac3: 'EAC3',
    ac3: 'AC3',
    flac: 'FLAC',
    aac: 'AAC',
  }[a] || ''
})

const resolutionVariant = computed(() => {
  switch ((props.resolution || '').toLowerCase()) {
    case '8k':
    case '4k':
      return 'uhd'
    case '1440p':
    case '1080p':
      return 'hd'
    case '720p':
      return 'std'
    case 'sd':
      return 'sd'
    default:
      return 'na'
  }
})
const hdrVariant = computed(() => {
  switch ((props.hdr || '').toLowerCase()) {
    case 'dv':
      return 'dv'
    case 'hdr10+':
      return 'hdrplus'
    case 'hdr10':
      return 'hdr'
    default:
      return ''
  }
})

const hasAny = computed(() => !!(resolutionLabel.value || hdrLabel.value || sourceLabel.value || videoCodecLabel.value || audioCodecLabel.value))
</script>

<template>
  <div v-if="hasAny" :class="['quality-badge', compact ? 'is-compact' : '']">
    <span v-if="resolutionLabel" :class="['pill', 'pill-res', `pill-res-${resolutionVariant}`]">{{ resolutionLabel }}</span>
    <span v-if="hdrLabel" :class="['pill', 'pill-hdr', `pill-hdr-${hdrVariant}`]">{{ hdrLabel }}</span>
    <template v-if="!compact">
      <span v-if="sourceLabel" class="pill pill-source">{{ sourceLabel }}</span>
      <span v-if="videoCodecLabel" class="pill pill-codec">{{ videoCodecLabel }}</span>
      <span v-if="audioCodecLabel" class="pill pill-codec">{{ audioCodecLabel }}</span>
    </template>
  </div>
</template>

<style scoped>
.quality-badge {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
}
.pill {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  line-height: 1;
  color: #fff;
  background: rgba(255, 255, 255, 0.14);
  border: 1px solid rgba(255, 255, 255, 0.08);
}
.pill-res-uhd {
  background: linear-gradient(135deg, #8e5bff 0%, #6f3bff 100%);
  border-color: rgba(255, 255, 255, 0.18);
}
.pill-res-hd {
  background: linear-gradient(135deg, #4a9eff 0%, #1f6fff 100%);
}
.pill-res-std {
  background: rgba(120, 130, 150, 0.55);
}
.pill-res-sd {
  background: rgba(90, 100, 115, 0.55);
}
.pill-res-na {
  background: rgba(110, 120, 130, 0.4);
}
.pill-hdr-dv {
  background: linear-gradient(135deg, #ff9f1c 0%, #ff6b1c 100%);
}
.pill-hdr-hdrplus {
  background: linear-gradient(135deg, #f5c518 0%, #d1a100 100%);
  color: #1a1200;
}
.pill-hdr-hdr {
  background: linear-gradient(135deg, #f5c518 0%, #e08b00 100%);
  color: #1a1200;
}
.pill-source {
  background: rgba(255, 255, 255, 0.08);
  font-weight: 500;
}
.pill-codec {
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.88);
  font-weight: 500;
}
.quality-badge.is-compact .pill {
  height: 18px;
  font-size: 10px;
  padding: 0 6px;
  border-radius: 5px;
}
</style>
