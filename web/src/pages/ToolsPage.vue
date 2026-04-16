<script setup lang="ts">
import { ref, watch } from 'vue'
import { NTabs, NTabPane } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import WebhookTab from '@/pages/tools/WebhookTab.vue'
import ApiKeysTab from '@/pages/tools/ApiKeysTab.vue'
import BackupTab from '@/pages/tools/BackupTab.vue'
import EmbyMigrateTab from '@/pages/tools/EmbyMigrateTab.vue'

const route = useRoute()
const router = useRouter()

// 批次 1：tab 由路由 meta 驱动；保留 query.tab 作 fallback。
const tabToRoute: Record<string, string> = {
  webhook: 'system_webhook',
  'api-keys': 'system_api_keys',
  backup: 'system_backup',
  'emby-migrate': 'system_emby_migrate',
}

function pickTabFromRoute(): string {
  const metaTab = (route.meta as { tab?: string })?.tab
  if (metaTab) return metaTab
  const q = route.query.tab
  return typeof q === 'string' && q ? q : 'webhook'
}

const activeTab = ref<string>(pickTabFromRoute())

function onTabChange(tab: string) {
  activeTab.value = tab
  const targetName = tabToRoute[tab]
  if (targetName && route.name !== targetName) {
    void router.push({ name: targetName })
  }
}

watch(
  () => route.fullPath,
  () => { activeTab.value = pickTabFromRoute() },
)
</script>

<template>
  <PageShell title="工具" description="Webhook、API 密钥、备份与迁移" :icon="AppIcons.tools">
    <n-tabs
      :value="activeTab"
      type="segment"
      size="large"
      class="tools-tabs"
      @update:value="onTabChange"
    >
      <n-tab-pane name="webhook" tab="Webhook">
        <WebhookTab />
      </n-tab-pane>
      <n-tab-pane name="api-keys" tab="API 密钥">
        <ApiKeysTab />
      </n-tab-pane>
      <n-tab-pane name="backup" tab="备份">
        <BackupTab />
      </n-tab-pane>
      <n-tab-pane name="emby-migrate" tab="Emby 迁移">
        <EmbyMigrateTab />
      </n-tab-pane>
    </n-tabs>
  </PageShell>
</template>

<style scoped>
.tools-tabs {
  margin-top: 4px;
}

.tools-tabs :deep(.n-tab-pane) {
  padding-top: 20px;
}
</style>
