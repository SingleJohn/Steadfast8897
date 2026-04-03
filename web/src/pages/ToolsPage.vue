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

const activeTab = ref<string>((route.query.tab as string) || 'webhook')

function onTabChange(tab: string) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
}

watch(
  () => route.query.tab,
  (tab) => {
    activeTab.value = typeof tab === 'string' && tab ? tab : 'webhook'
  },
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
