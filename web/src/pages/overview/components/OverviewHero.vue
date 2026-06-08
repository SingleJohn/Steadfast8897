<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NDivider, NIcon } from 'naive-ui'
import { CloudDownloadOutline, CopyOutline, RefreshOutline, ServerOutline } from '@vicons/ionicons5'
import type { UpdateStatus } from '@/api/client'
import { formatVersion, isUpdateBusy } from '../utils'

const props = defineProps<{
  serverInfo: any
  updateStatus: UpdateStatus | null
  isRunning: boolean
  runStatusText: string
  fullServerId: string
  checkingUpdate: boolean
  applyingUpdate: boolean
  isManualUpdate: boolean
}>()

const emit = defineEmits<{
  checkUpdate: []
  applyUpdate: []
  manualDownload: []
  restart: []
  shutdown: []
  copyServerId: []
}>()

const versionText = computed(() => props.serverInfo ? formatVersion(props.serverInfo) : 'dev')
</script>

<template>
  <section class="hero-bar">
    <div class="hero-left">
      <div class="hero-icon">
        <n-icon :component="ServerOutline" :size="22" />
      </div>
      <div class="hero-meta">
        <div class="hero-title">
          <span class="hero-name">{{ serverInfo?.ServerName || 'FYMS' }}</span>
          <code class="hero-version">{{ versionText }}</code>
          <span v-if="serverInfo?.OperatingSystemDisplayName || serverInfo?.OperatingSystem" class="hero-os">
            · {{ serverInfo.OperatingSystemDisplayName || serverInfo.OperatingSystem }}
          </span>
          <span class="hero-status">
            <span class="status-dot" :class="isRunning ? 'is-online' : 'is-error'"></span>
            {{ runStatusText }}
          </span>
        </div>
        <div class="hero-sub">
          <span v-if="fullServerId" class="hero-sub-item hero-id">
            <span class="hero-id-label">ID</span>
            <code class="hero-id-value">{{ fullServerId }}</code>
            <button
              class="hero-id-copy"
              type="button"
              aria-label="复制服务器 ID"
              @click.stop="emit('copyServerId')"
            >
              <n-icon :component="CopyOutline" :size="13" />
            </button>
          </span>
          <span v-if="serverInfo?.LocalAddress" class="hero-sub-item">· {{ serverInfo.LocalAddress }}</span>
          <span v-if="updateStatus?.hasUpdate" class="hero-update-hint">
            · <n-icon :component="CloudDownloadOutline" :size="13" /> 有新版本 v{{ updateStatus.latestVersion }}
          </span>
        </div>
      </div>
    </div>
    <div class="hero-actions">
      <n-button size="small" secondary :loading="checkingUpdate" @click="emit('checkUpdate')">
        <template #icon><n-icon :component="RefreshOutline" /></template>
        检查更新
      </n-button>
      <n-button
        v-if="!isManualUpdate"
        size="small"
        type="primary"
        :disabled="!updateStatus?.hasUpdate || isUpdateBusy(updateStatus?.status)"
        :loading="applyingUpdate"
        @click="emit('applyUpdate')"
      >
        <template #icon><n-icon :component="CloudDownloadOutline" /></template>
        立即更新
      </n-button>
      <n-button
        v-else
        size="small"
        type="primary"
        :disabled="!updateStatus?.hasUpdate"
        @click="emit('manualDownload')"
      >
        <template #icon><n-icon :component="CloudDownloadOutline" /></template>
        下载更新包
      </n-button>
      <n-divider vertical />
      <n-button size="small" secondary type="warning" @click="emit('restart')">重启</n-button>
      <n-button size="small" secondary type="error" @click="emit('shutdown')">关闭</n-button>
    </div>
  </section>
</template>
