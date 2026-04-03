<script setup lang="ts">
import type { FormInst } from 'naive-ui'
import {
  NButton,
  NCard,
  NDivider,
  NForm,
  NFormItem,
  NGrid,
  NGridItem,
  NInput,
  NSpace,
  useMessage,
  NIcon,
  NText,
  NEmpty
} from 'naive-ui'
import { onMounted, ref } from 'vue'
import { CloseOutline, AddOutline, MapOutline, ArrowForwardOutline } from '@vicons/ionicons5'

import PageHeader from '@/components/PageHeader.vue'
import StickyActionBar from '@/components/StickyActionBar.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import { useConfigDraft } from '@/composables/useConfigDraft'
import type { Config, PathRuleSetConfig } from '@/types'
import { AppIcons } from '@/icons/appIcons'

const message = useMessage()
const { loading, saving, loadError, draft, hasChanges, refresh, resetDraft, saveDraft } = useConfigDraft(message)
const formRef = ref<FormInst | null>(null)

function genId(prefix: string) {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
}

function addPathRuleSet() {
  if (!draft.value) return
  const rs: PathRuleSetConfig = {
    id: genId('paths'),
    name: '',
    mappings: [],
  }
  draft.value.path_rule_sets.push(rs)
}

async function onSave() {
  if (!draft.value) return
  await formRef.value?.validate().catch(() => false)
  const ok = await saveDraft({ successMessage: '保存成功（立即生效）' })
  if (ok) message.success('保存成功')
}

onMounted(() => {
  refresh()
})
</script>

<template>
  <div class="path-rules-page">
    <page-header title="路径映射" :icon="AppIcons.pathMapping" description="将 Emby 媒体路径映射到对象存储 Key（From -> To）。" />

    <div class="config-content">
      <error-banner v-if="loadError" :message="loadError" style="margin-bottom: 16px" />

      <n-form v-if="draft" ref="formRef" :model="draft as Config" label-placement="top" size="medium">
        
        <n-space vertical :size="16">
          <n-card :bordered="false" class="glass-card section-card" title="规则集 (Rule Sets)">
            <template #header-extra>
              <n-button size="small" secondary @click="addPathRuleSet">
                <template #icon><n-icon><AddOutline /></n-icon></template>
                新增规则集
              </n-button>
            </template>

            <n-space v-if="draft.path_rule_sets.length === 0" vertical :size="8">
              <n-empty description="暂无 Path Rule Set，请点击右上角新增" />
            </n-space>

            <n-space v-else vertical :size="24">
              <n-card 
                v-for="(rs, idx) in draft.path_rule_sets" 
                :key="rs.id || idx" 
                size="small" 
                :bordered="false" 
                class="glass-card ruleset-card"
              >
                <template #header>
                  <n-space align="center" :size="12">
                    <div class="icon-box">
                      <n-icon :size="20"><MapOutline /></n-icon>
                    </div>
                    <div class="header-inputs">
                      <n-input 
                        v-model:value="rs.name" 
                        placeholder="规则集名称 (例如: Anime Mappings)" 
                        class="name-input" 
                        size="small" 
                      />
                      <n-text depth="3" class="id-text">{{ rs.id }}</n-text>
                    </div>
                  </n-space>
                </template>
                <template #header-extra>
                  <n-button quaternary circle size="small" type="error" @click="draft.path_rule_sets.splice(idx, 1)">
                    <template #icon><n-icon><CloseOutline /></n-icon></template>
                  </n-button>
                </template>

                <div class="mappings-container">
                  <div class="mappings-header">
                    <n-grid cols="24" x-gap="12">
                      <n-grid-item span="11">
                        <n-text depth="3" size="small" strong>源路径 (Emby Path)</n-text>
                      </n-grid-item>
                      <n-grid-item span="1" class="arrow-col"></n-grid-item>
                      <n-grid-item span="11">
                        <n-text depth="3" size="small" strong>目标路径 (Object Key / Path)</n-text>
                      </n-grid-item>
                      <n-grid-item span="1"></n-grid-item>
                    </n-grid>
                  </div>

                  <div class="mappings-list">
                    <div v-for="(m, mIdx) in rs.mappings" :key="mIdx" class="mapping-row">
                      <n-grid cols="24" x-gap="12" align="center">
                        <n-grid-item span="11">
                          <n-input v-model:value="m.from" placeholder="/mnt/media/movies" />
                        </n-grid-item>
                        <n-grid-item span="1" class="arrow-col">
                          <n-icon depth="3"><ArrowForwardOutline /></n-icon>
                        </n-grid-item>
                        <n-grid-item span="11">
                          <n-input v-model:value="m.to" placeholder="/movies" />
                        </n-grid-item>
                        <n-grid-item span="1" style="text-align: right;">
                          <n-button quaternary circle type="error" size="small" @click="rs.mappings.splice(mIdx, 1)">
                            <template #icon><n-icon><CloseOutline /></n-icon></template>
                          </n-button>
                        </n-grid-item>
                      </n-grid>
                    </div>
                    
                    <div v-if="rs.mappings.length === 0" class="empty-mappings">
                      <n-text depth="3" size="small">暂无映射规则</n-text>
                    </div>
                  </div>

                  <div class="mappings-footer">
                    <n-button dashed size="small" block @click="rs.mappings.push({ from: '', to: '' })">
                      <template #icon><n-icon><AddOutline /></n-icon></template>
                      添加映射
                    </n-button>
                  </div>
                </div>
              </n-card>
            </n-space>
          </n-card>
        </n-space>
      </n-form>

      <sticky-action-bar
        v-if="draft"
        :dirty="hasChanges"
        :disabled="loading || saving"
        :primary-loading="saving"
        @primary="onSave"
        @secondary="resetDraft"
      />
    </div>
  </div>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
  padding-bottom: 80px;
}

.ruleset-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  transition: all 0.2s ease;
}
.ruleset-card:hover {
  border-color: var(--app-border-hover);
  box-shadow: var(--app-shadow-card);
}

.icon-box {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: var(--c-slate-100);
  color: var(--app-primary);
  display: grid;
  place-items: center;
}
.app-dark .icon-box {
  background: var(--c-slate-800);
}

.header-inputs {
  display: flex;
  align-items: center;
  gap: 12px;
}

.name-input {
  width: 240px;
}

.id-text {
  font-family: monospace;
  font-size: 12px;
  opacity: 0.5;
}

.mappings-container {
  background: var(--app-bg);
  border-radius: 8px;
  padding: 12px;
  border: 1px solid var(--app-border);
}

.mappings-header {
  padding: 0 4px 8px 4px;
  margin-bottom: 4px;
  border-bottom: 1px dashed var(--app-border);
}

.mappings-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.arrow-col {
  display: flex;
  justify-content: center;
  align-items: center;
}

.empty-mappings {
  text-align: center;
  padding: 16px;
  opacity: 0.7;
}

.mappings-footer {
  margin-top: 12px;
}
</style>