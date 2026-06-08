<script setup lang="ts">
import { NIcon } from 'naive-ui'
import { FolderOpenOutline } from '@vicons/ionicons5'
import QualityBadge from '@/components/QualityBadge.vue'
import { formatBitrate, formatFileSize, formatStream, groupedStreams, streamTypeLabel } from '../utils/format'

defineProps<{
  item: any
  isAdmin: boolean
}>()
</script>

<template>
  <div v-if="item.MediaSources?.length" class="media-info-section">
    <h3 class="section-heading">媒体信息</h3>
    <div class="ms-list">
      <article
        v-for="(src, idx) in item.MediaSources"
        :key="src.Id || idx"
        class="ms-card"
      >
        <header class="ms-header">
          <div class="ms-title">
            <strong v-if="(item.MediaSources?.length || 0) > 1">
              {{ src.Name || `版本 ${idx + 1}` }}
            </strong>
            <span v-if="src.Container" class="ms-container">{{ src.Container.toUpperCase() }}</span>
          </div>
          <quality-badge
            :resolution="src.FymsResolution"
            :hdr="src.FymsHdrFormat"
            :source="src.FymsSource"
            :video-codec="src.FymsVideoCodec"
            :audio-codec="src.FymsAudioCodec"
          />
        </header>

        <div class="ms-facts">
          <div v-if="src.Bitrate" class="ms-fact">
            <span class="ms-fact-label">总码率</span>
            <span class="ms-fact-value">{{ formatBitrate(src.Bitrate) }}</span>
          </div>
          <div v-if="src.Size" class="ms-fact">
            <span class="ms-fact-label">大小</span>
            <span class="ms-fact-value">{{ formatFileSize(src.Size) }}</span>
          </div>
          <div v-if="isAdmin && src.Path" class="ms-fact ms-fact-path">
            <span class="ms-fact-label">
              <n-icon :size="13" style="vertical-align: -2px"><FolderOpenOutline /></n-icon>
              路径
            </span>
            <code class="ms-path">{{ src.Path }}</code>
          </div>
        </div>

        <div
          v-for="group in groupedStreams(src)"
          :key="group.type"
          class="ms-stream-group"
        >
          <h4 class="ms-stream-type">{{ streamTypeLabel(group.type) }}</h4>
          <ul class="ms-stream-list">
            <li
              v-for="(s, si) in group.streams"
              :key="`${group.type}-${si}`"
              class="ms-stream"
            >
              <span class="ms-stream-text">{{ formatStream(s) }}</span>
              <span v-if="s.IsDefault" class="ms-stream-flag">默认</span>
              <span v-if="s.IsForced" class="ms-stream-flag">强制</span>
            </li>
          </ul>
        </div>
      </article>
    </div>

    <div v-if="isAdmin && item.Path && !item.MediaSources?.length" class="ms-card ms-fallback">
      <div class="ms-fact ms-fact-path">
        <span class="ms-fact-label">
          <n-icon :size="13" style="vertical-align: -2px"><FolderOpenOutline /></n-icon>
          路径
        </span>
        <code class="ms-path">{{ item.Path }}</code>
      </div>
    </div>
  </div>
</template>

