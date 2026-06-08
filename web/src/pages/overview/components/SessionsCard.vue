<script setup lang="ts">
import { NCard, NIcon } from 'naive-ui'
import { PeopleOutline } from '@vicons/ionicons5'

defineProps<{
  sessions: any[]
}>()
</script>

<template>
  <n-card class="section-card" title="正在播放" size="small">
    <template #header-extra>
      <span class="subtle-count">{{ sessions.length }}</span>
    </template>
    <div v-if="sessions.length === 0" class="empty-state">
      <n-icon :component="PeopleOutline" :size="22" />
      <span>当前无人播放</span>
    </div>
    <ul v-else class="session-list">
      <li v-for="s in sessions" :key="s.Id" class="session-item">
        <div class="session-avatar">{{ (s.UserName || '?')[0].toUpperCase() }}</div>
        <div class="session-main">
          <div class="session-row">
            <span class="session-name">{{ s.UserName }}</span>
            <span class="session-meta">{{ s.Client }}{{ s.ApplicationVersion ? ' · ' + s.ApplicationVersion : '' }} · {{ s.DeviceName }}</span>
          </div>
        </div>
        <code class="session-ip">{{ s.RemoteEndPoint || '-' }}</code>
      </li>
    </ul>
  </n-card>
</template>
