<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
} from 'naive-ui'
import {
  AddOutline,
  ArrowForwardOutline,
  CopyOutline,
  KeyOutline,
  LinkOutline,
  CloseOutline,
  DocumentTextOutline,
} from '@vicons/ionicons5'
import { getSystemInfo, getSystemConfig, updateSystemConfig } from '@/api/client'

const { showToast } = useToast()

const serverInfo = ref<any>(null)
const webhookSecret = ref('')
const webhookPathMappings = ref<{ from: string; to: string }[]>([])
const savingWebhook = ref(false)

const webhookBaseUrl = computed(() => {
  const port = serverInfo.value?.LocalAddress?.split(':').pop() || '8960'
  return `${window.location.protocol}//${window.location.hostname}:${port}`
})

const webhookFullUrl = computed(() => `${webhookBaseUrl.value}/Library/Webhook/CloudDrive`)

const webhookTomlPreview = computed(() => {
  const base = webhookBaseUrl.value
  const sec = webhookSecret.value ? `\nX-Webhook-Secret = "${webhookSecret.value}"` : ''
  return `[global_params]
base_url = "${base}"
enabled = true

[global_params.default_headers]
content-type = "application/json"${sec}

[file_system_watcher]
url = "{base_url}/Library/Webhook/CloudDrive"
method = "POST"
enabled = true
body = '{"device_name":"{device_name}","user_name":"{user_name}","version":"{version}","event_category":"{event_category}","event_name":"{event_name}","event_time":"{event_time}","send_time":"{send_time}","data":[{"action":"{action}","is_dir":"{is_dir}","source_file":"{source_file}","destination_file":"{destination_file}"}]}'`
})

function copyWebhookUrl() {
  void navigator.clipboard.writeText(webhookFullUrl.value)
  showToast('已复制到剪贴板', 'success')
}

function copyWebhookToml() {
  void navigator.clipboard.writeText(webhookTomlPreview.value)
  showToast('webhook.toml 已复制到剪贴板', 'success')
}

function generateWebhookSecret() {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
  let result = ''
  for (let i = 0; i < 32; i++) result += chars.charAt(Math.floor(Math.random() * chars.length))
  webhookSecret.value = result
}

async function saveWebhookSettings() {
  savingWebhook.value = true
  try {
    await updateSystemConfig({
      webhook_secret: webhookSecret.value,
      webhook_path_mappings: JSON.stringify(webhookPathMappings.value.filter((m) => m.from && m.to)),
    })
    showToast('Webhook 设置已保存', 'success')
  } catch {
    showToast('保存 Webhook 设置失败', 'error')
  } finally {
    savingWebhook.value = false
  }
}

onMounted(() => {
  getSystemInfo().then((s) => (serverInfo.value = s)).catch(() => {})
  getSystemConfig().then((cfg: any) => {
    webhookSecret.value = cfg.webhook_secret || ''
    try {
      const mappings = cfg.webhook_path_mappings ? JSON.parse(cfg.webhook_path_mappings) : []
      webhookPathMappings.value = Array.isArray(mappings) ? mappings : []
    } catch {
      webhookPathMappings.value = []
    }
  }).catch(() => {})
})
</script>

<template>
  <div class="webhook-layout">
    <n-card :bordered="false" class="glass-card section-card tool-card">
      <template #header>
        <div class="card-header-wrap">
          <div class="icon-box webhook">
            <n-icon :size="18"><LinkOutline /></n-icon>
          </div>
          <div class="header-copy">
            <div class="header-title">Webhook</div>
            <div class="header-desc">配置 CloudDrive2 的文件变动回调，实现文件新增、删除、重命名后的自动入库。</div>
          </div>
        </div>
      </template>

      <div class="subsection">
        <div class="subsection-title">回调地址</div>
        <div class="code-row">
          <code class="code-block">{{ webhookFullUrl }}</code>
          <n-button secondary size="small" @click="copyWebhookUrl">
            <template #icon><n-icon><CopyOutline /></n-icon></template>
            复制
          </n-button>
        </div>
        <div class="hint-text">将此地址填入 CloudDrive2 的 `webhook.toml` 中。</div>
      </div>

      <div class="subsection">
        <div class="subsection-title">安全设置</div>
        <n-form label-placement="top" size="small">
          <n-form-item label="共享密钥">
            <div class="secret-row">
              <n-input v-model:value="webhookSecret" placeholder="设置一个密钥用于验证 Webhook 请求" />
              <n-button secondary @click="generateWebhookSecret">
                <template #icon><n-icon><KeyOutline /></n-icon></template>
                生成
              </n-button>
            </div>
          </n-form-item>
        </n-form>
        <div class="hint-text">CloudDrive2 中需要设置 `X-Webhook-Secret` 请求头。留空则不做校验。</div>
      </div>

      <div class="subsection">
        <div class="subsection-title">路径映射</div>
        <div class="hint-text mapping-hint">将 CloudDrive2 报告的路径前缀映射到 FYMS 可访问的本地路径，例如 `/CloudNAS/ -> /mnt/CloudNAS/`。</div>
        <div class="mappings-box">
          <div class="mappings-head">
            <div>源路径</div>
            <div class="arrow-slot"></div>
            <div>目标路径</div>
            <div class="action-slot"></div>
          </div>

          <div v-if="webhookPathMappings.length === 0" class="mappings-empty">
            <n-empty size="small" description="暂无路径映射" />
          </div>

          <div v-else class="mappings-list">
            <div v-for="(m, i) in webhookPathMappings" :key="i" class="mapping-row">
              <n-input v-model:value="m.from" placeholder="/CloudNAS/" size="small" />
              <div class="arrow-slot">
                <n-icon depth="3"><ArrowForwardOutline /></n-icon>
              </div>
              <n-input v-model:value="m.to" placeholder="/mnt/CloudNAS/" size="small" />
              <div class="action-slot">
                <n-button quaternary circle type="error" size="small" @click="webhookPathMappings = webhookPathMappings.filter((_, idx) => idx !== i)">
                  <template #icon><n-icon><CloseOutline /></n-icon></template>
                </n-button>
              </div>
            </div>
          </div>

          <div class="mappings-footer">
            <n-button dashed size="small" block @click="webhookPathMappings = [...webhookPathMappings, { from: '', to: '' }]">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              添加映射
            </n-button>
          </div>
        </div>
      </div>

      <div class="card-actions">
        <n-button type="primary" :loading="savingWebhook" @click="saveWebhookSettings">保存设置</n-button>
      </div>
    </n-card>

    <n-card :bordered="false" class="glass-card section-card tool-card">
      <template #header>
        <div class="card-header-wrap">
          <div class="icon-box toml">
            <n-icon :size="18"><DocumentTextOutline /></n-icon>
          </div>
          <div class="header-copy">
            <div class="header-title">`webhook.toml` 配置示例</div>
            <div class="header-desc">将以下内容保存到 CloudDrive2 配置目录下的 `webhook.toml` 文件中。需要 CloudDrive2 会员功能。</div>
          </div>
        </div>
      </template>

      <pre class="toml-preview">{{ webhookTomlPreview }}</pre>

      <div class="card-actions">
        <n-button secondary @click="copyWebhookToml">
          <template #icon><n-icon><CopyOutline /></n-icon></template>
          复制配置
        </n-button>
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.webhook-layout {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

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
}

.icon-box.webhook {
  color: #0ea5e9;
  background: rgba(14, 165, 233, 0.12);
}

.icon-box.toml {
  color: #8b5cf6;
  background: rgba(139, 92, 246, 0.12);
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

.subsection + .subsection {
  margin-top: 14px;
}

.subsection-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
  margin-bottom: 10px;
}

.hint-text {
  font-size: 11px;
  color: var(--app-text-muted);
  line-height: 1.5;
  margin-top: 6px;
}

.code-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.code-block {
  display: block;
  flex: 1;
  background: rgba(0,0,0,0.2);
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--app-text);
  word-break: break-all;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.secret-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.mapping-hint {
  margin-bottom: 10px;
  margin-top: -2px;
}

.mappings-box {
  background: var(--app-bg);
  border-radius: 10px;
  padding: 12px;
  border: 1px solid var(--app-border);
}

.mappings-head,
.mapping-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 24px minmax(0, 1fr) 32px;
  gap: 8px;
  align-items: center;
}

.mappings-head {
  padding: 0 4px 8px;
  margin-bottom: 8px;
  border-bottom: 1px dashed var(--app-border);
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text-muted);
}

.mappings-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mappings-empty {
  padding: 12px 0;
}

.mappings-footer {
  margin-top: 12px;
}

.arrow-slot,
.action-slot {
  display: flex;
  align-items: center;
  justify-content: center;
}

.toml-preview {
  margin: 0;
  padding: 14px;
  border-radius: 12px;
  background: rgba(0,0,0,0.2);
  color: var(--app-text);
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 16px;
}

@media (max-width: 640px) {
  .code-row,
  .secret-row {
    flex-direction: column;
    align-items: stretch;
  }

  .mappings-head,
  .mapping-row {
    grid-template-columns: 1fr;
  }

  .arrow-slot,
  .action-slot {
    justify-content: flex-start;
  }
}
</style>
