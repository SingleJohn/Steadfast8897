<script setup lang="ts">
import type { FormInst } from 'naive-ui'
import {
  NButton,
  NForm,
  NFormItem,
  NGrid,
  NGridItem,
  NInput,
  NAlert,
  NModal,
  NSpace,
  NText,
  useMessage,
} from 'naive-ui'
import { ref } from 'vue'

import PageShell from '@/components/PageShell.vue'
import StickyActionBar from '@/components/StickyActionBar.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import BackendListSection from '@/pages/backends/BackendListSection.vue'
import ResourcePoolsSection from '@/pages/backends/ResourcePoolsSection.vue'
import { useBackendsPage } from '@/composables/useBackendsPage'
import { AppIcons } from '@/icons/appIcons'
import type { Config } from '@/types'

const message = useMessage()
const {
  loading,
  saving,
  loadError,
  draft,
  hasChanges,
  resetDraft,
  saveDraft,
  s3CheckStatus,
  cdnTestOpen,
  cdnTestKey,
  cdnTestUid,
  cdnTestLoading,
  cdnTestResult,
  backendOptions,
  backendTypeOptions,
  pan123DirectLinkModeOptions,
  open115LinkModeOptions,
  sub115SelectionStrategyOptions,
  addBackend,
  addPool,
  checkS3Backend,
  openCdnTest,
  runCdnTest,
} = useBackendsPage(message)
const formRef = ref<FormInst | null>(null)

async function onSave() {
  if (!draft.value) return
  await formRef.value?.validate().catch(() => false)
  const ok = await saveDraft({ successMessage: '保存成功（立即生效）' })
  if (ok) message.success('保存成功')
}
</script>

<template>
  <page-shell title="后端存储" :icon="AppIcons.backends" description="管理对象存储与 CDN 节点，并配置主备切换策略。" body-class="config-content">
      <error-banner v-if="loadError" :message="loadError" class="page-error" />

      <n-form v-if="draft" ref="formRef" :model="draft as Config" label-placement="top" size="medium">
        
        <n-grid cols="1 l:3" x-gap="24" y-gap="24" responsive="screen">

          <!-- Column 1-2: Backends -->
          <n-grid-item span="1 l:2">
            <backend-list-section
              :draft="draft as Config"
              :backend-type-options="backendTypeOptions"
              :pan123-direct-link-mode-options="pan123DirectLinkModeOptions"
              :open115-link-mode-options="open115LinkModeOptions"
              :sub115-selection-strategy-options="sub115SelectionStrategyOptions"
              :s3-check-status="s3CheckStatus"
              @add-backend="addBackend('s3')"
              @check-s3="checkS3Backend"
              @open-cdn-test="openCdnTest"
            />
          </n-grid-item>

          <!-- Column 3: Pools -->
          <n-grid-item>
            <resource-pools-section :draft="draft as Config" :backend-options="backendOptions" @add-pool="addPool" />
          </n-grid-item>

        </n-grid>
      </n-form>

      <!-- CDN Test Modal -->
      <n-modal v-model:show="cdnTestOpen" preset="card" title="CDN 签名测试" class="glass-card glass-modal cdn-test-modal">
        <n-space vertical :size="16">
          <n-alert type="info" :show-icon="false" :bordered="false">
            基于当前已保存的配置生成。如果刚修改了 Backend，请先保存。
          </n-alert>
          <n-form label-placement="left" label-width="80">
            <n-form-item label="Object Key">
              <n-input v-model:value="cdnTestKey" placeholder="movies/avatar.mkv" />
            </n-form-item>
            <n-form-item label="UID (Opt)">
              <n-input v-model:value="cdnTestUid" placeholder="默认使用配置值" />
            </n-form-item>
          </n-form>
          
          <div v-if="cdnTestResult" class="result-box">
            <n-input 
              type="textarea" 
              :value="cdnTestResult.url" 
              readonly 
              :autosize="{ minRows: 3, maxRows: 6 }" 
              placeholder="生成结果" 
            />
            <div class="expiry-row">
              <n-text depth="3" size="small">过期时间: {{ new Date(cdnTestResult.expiry * 1000).toLocaleString() }}</n-text>
            </div>
          </div>

          <n-space justify="end">
            <n-button secondary @click="cdnTestOpen = false">关闭</n-button>
            <n-button type="primary" :loading="cdnTestLoading" @click="runCdnTest">生成</n-button>
          </n-space>
        </n-space>
      </n-modal>

      <sticky-action-bar
        v-if="draft"
        :dirty="hasChanges"
        :disabled="loading || saving"
        :primary-loading="saving"
        @primary="onSave"
        @secondary="resetDraft"
      />
  </page-shell>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
  padding-bottom: 80px;
}

.page-error {
  margin-bottom: 16px;
}

.result-box {
  background: var(--c-slate-100);
  padding: 12px;
  border-radius: 8px;
}

.cdn-test-modal {
  width: min(600px, 90vw);
}

.expiry-row {
  margin-top: 8px;
  text-align: right;
}
.app-dark .result-box { background: var(--c-slate-800); }
</style>
