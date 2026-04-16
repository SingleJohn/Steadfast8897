<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  NButton,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
} from 'naive-ui'
import { AddOutline, CopyOutline, TrashOutline } from '@vicons/ionicons5'

import { getApiKeys, createApiKey, deleteApiKey } from '@/api/client'
import { useToast } from '@/composables/useToast'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'

const { showToast } = useToast()

const apiKeyList = ref<any[]>([])
const newKeyName = ref('')
const creatingKey = ref(false)
const newlyCreatedKey = ref<string | null>(null)

async function loadApiKeys() {
  try {
    const res = await getApiKeys()
    apiKeyList.value = res.Items || []
  } catch {
    apiKeyList.value = []
  }
}

async function createNewApiKey() {
  creatingKey.value = true
  try {
    const res = await createApiKey(newKeyName.value.trim())
    newlyCreatedKey.value = res.Key
    newKeyName.value = ''
    await loadApiKeys()
    showToast('API 密钥已创建', 'success')
  } catch {
    showToast('创建 API 密钥失败', 'error')
  } finally {
    creatingKey.value = false
  }
}

async function deleteApiKeyEntry(k: any) {
  if (!confirm(`确定要删除密钥 "${k.Name}" 吗？`)) return
  try {
    await deleteApiKey(k.Id)
    apiKeyList.value = apiKeyList.value.filter((x) => x.Id !== k.Id)
    showToast('API 密钥已删除', 'success')
  } catch {
    showToast('删除失败', 'error')
  }
}

function copyNewlyCreatedKey() {
  if (!newlyCreatedKey.value) return
  void navigator.clipboard.writeText(newlyCreatedKey.value)
  showToast('已复制到剪贴板', 'success')
}

function maskKey(key: string) {
  if (key.length <= 12) return '••••••••'
  return key.substring(0, 8) + '••••' + key.substring(key.length - 4)
}

onMounted(() => {
  void loadApiKeys()
})
</script>

<template>
  <page-shell
    title="API 密钥"
    description="管理第三方服务与自动化脚本的接入密钥。密钥拥有管理员权限，请谨慎保管。"
    :icon="AppIcons.apikeys"
  >
    <div class="subsection">
      <div class="subsection-title">创建密钥</div>
      <n-form label-placement="top" size="small">
        <n-form-item label="名称">
          <div class="create-row">
            <n-input v-model:value="newKeyName" placeholder="例如：自动化脚本 / Home Assistant / 同步服务" @keydown.enter.prevent="createNewApiKey" />
            <n-button type="primary" :disabled="creatingKey || !newKeyName.trim()" :loading="creatingKey" @click="createNewApiKey">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              生成密钥
            </n-button>
          </div>
        </n-form-item>
      </n-form>
      <div class="hint-text">新建后只会完整展示一次，请立即复制保存。</div>
    </div>

    <div v-if="newlyCreatedKey" class="result-box">
      <div class="result-head">
        <span class="result-title">新密钥</span>
        <n-button secondary size="small" @click="copyNewlyCreatedKey">
          <template #icon><n-icon><CopyOutline /></n-icon></template>
          复制
        </n-button>
      </div>
      <code class="key-code">{{ newlyCreatedKey }}</code>
    </div>

    <div class="subsection">
      <div class="subsection-title">已有密钥</div>

      <div v-if="apiKeyList.length === 0" class="empty-wrap">
        <n-empty description="暂无 API 密钥" />
      </div>

      <div v-else class="key-list">
        <div v-for="k in apiKeyList" :key="k.Id" class="key-item">
          <div class="key-main">
            <div class="key-name">{{ k.Name }}</div>
            <div class="key-meta">
              <code class="key-mask">{{ maskKey(k.Key) }}</code>
              <span>创建者: {{ k.CreatedBy }}</span>
              <span>创建于: {{ new Date(k.CreatedAt).toLocaleDateString() }}</span>
              <span v-if="k.LastUsedAt">最近使用: {{ new Date(k.LastUsedAt).toLocaleDateString() }}</span>
            </div>
          </div>
          <n-button quaternary circle type="error" size="small" @click="deleteApiKeyEntry(k)">
            <template #icon><n-icon><TrashOutline /></n-icon></template>
          </n-button>
        </div>
      </div>
    </div>
  </page-shell>
</template>

<style scoped>
.tool-card {
  min-height: 100%;
}

.card-header-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
}

.icon-box {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.12);
}

.header-copy {
  min-width: 0;
}

.header-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}

.header-desc {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-muted);
  line-height: 1.5;
}

.subsection {
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 12px;
  padding: 14px;
  background: rgba(255,255,255,0.02);
}

.subsection + .subsection,
.result-box + .subsection,
.subsection + .result-box {
  margin-top: 14px;
}

.subsection-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
  margin-bottom: 10px;
}

.create-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.hint-text {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.5;
}

.result-box {
  margin-top: 14px;
  padding: 14px;
  border-radius: 12px;
  border: 1px solid rgba(var(--app-primary-rgb), 0.24);
  background: rgba(var(--app-primary-rgb), 0.08);
}

.result-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.result-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-primary);
}

.key-code {
  display: block;
  background: rgba(0,0,0,0.2);
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--app-text);
  word-break: break-all;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.empty-wrap {
  padding: 8px 0;
}

.key-list {
  display: flex;
  flex-direction: column;
}

.key-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
}

.key-item + .key-item {
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
}

.key-main {
  flex: 1;
  min-width: 0;
}

.key-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
}

.key-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  font-size: 12px;
  color: var(--app-text-muted);
  margin-top: 4px;
}

.key-mask {
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 11px;
  background: rgba(128,128,128,0.08);
  padding: 1px 6px;
  border-radius: 4px;
}

@media (max-width: 640px) {
  .create-row {
    flex-direction: column;
  }

  .result-head {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
