<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import ApiKeysTab from '@/pages/tools/ApiKeysTab.vue'
</script>

<template>
  <page-shell title="API 密钥" :icon="AppIcons.apikeys" description="管理第三方服务与自动化脚本的接入密钥">
    <ApiKeysTab />
  </page-shell>
</template>
