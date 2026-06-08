<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NCheckboxGroup,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NPopconfirm,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
} from 'naive-ui'
import {
  AddOutline,
  ArrowForwardOutline,
  CloseOutline,
  CopyOutline,
  DocumentTextOutline,
  KeyOutline,
  SaveOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { getSystemInfo, getSystemConfig, updateSystemConfig } from '@/api/client'
import {
  createWebhookSubscription,
  deleteWebhookSubscription,
  getNotificationSamplePayload,
  getSupportedNotificationEvents,
  listWebhookSubscriptions,
  testWebhookSubscription,
  updateWebhookSubscription,
  type NotificationEvent,
  type WebhookSubscription,
} from '@/api/notifications'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'

const { showToast } = useToast()

const activeTab = ref<'inbound' | 'outbound'>('inbound')

const serverInfo = ref<any>(null)
const webhookSecret = ref('')
const webhookPathMappings = ref<{ from: string; to: string }[]>([])
const savingWebhook = ref(false)

const subscriptions = ref<WebhookSubscription[]>([])
const supportedEvents = ref<NotificationEvent[]>([])
const loadingOutbound = ref(false)
const savingOutbound = ref(false)
const testingId = ref('')
const sampleLoading = ref(false)
const sampleEvent = ref<NotificationEvent>('library.new')
const samplePayload = ref<Record<string, unknown> | null>(null)

type SubscriptionDraft = {
  id: string
  name: string
  url: string
  events: NotificationEvent[]
  enabled: boolean
}

const defaultOutboundEvents: NotificationEvent[] = ['library.new', 'library.deleted']
const draft = ref<SubscriptionDraft>({
  id: '',
  name: '',
  url: '',
  events: [...defaultOutboundEvents],
  enabled: true,
})

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

const eventLabels: Record<string, string> = {
  'library.new': '新增媒体',
  'library.deleted': '移除媒体',
  'item.rate': '收藏状态',
  'item.markplayed': '标记已播放',
  'item.markunplayed': '标记未播放',
  'playback.start': '播放开始',
  'playback.stop': '播放停止',
  'system.notificationtest': '测试通知',
}

const eventGroups = computed(() => {
  const available = supportedEvents.value.length ? supportedEvents.value : Object.keys(eventLabels)
  const has = (event: string) => available.includes(event)
  return [
    {
      title: '媒体库',
      events: ['library.new', 'library.deleted'].filter(has),
    },
    {
      title: '用户行为',
      events: ['item.rate', 'item.markplayed', 'item.markunplayed'].filter(has),
    },
    {
      title: '播放',
      events: ['playback.start', 'playback.stop'].filter(has),
    },
    {
      title: '系统',
      events: ['system.notificationtest'].filter(has),
    },
  ].filter((g) => g.events.length > 0)
})

const eventSelectOptions = computed(() =>
  (supportedEvents.value.length ? supportedEvents.value : Object.keys(eventLabels)).map((value) => ({
    label: eventLabel(value),
    value,
  })),
)

const prettySamplePayload = computed(() => (samplePayload.value ? JSON.stringify(samplePayload.value, null, 2) : ''))

function eventLabel(event: string) {
  return eventLabels[event] || event
}

function copyWebhookUrl() {
  void navigator.clipboard.writeText(webhookFullUrl.value)
  showToast('已复制到剪贴板', 'success')
}

function copyWebhookToml() {
  void navigator.clipboard.writeText(webhookTomlPreview.value)
  showToast('webhook.toml 已复制到剪贴板', 'success')
}

function copySamplePayload() {
  if (!prettySamplePayload.value) return
  void navigator.clipboard.writeText(prettySamplePayload.value)
  showToast('示例负载已复制', 'success')
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

async function loadInbound() {
  getSystemInfo().then((s) => (serverInfo.value = s)).catch(() => {})
  try {
    const cfg: any = await getSystemConfig()
    webhookSecret.value = cfg.webhook_secret || ''
    try {
      const mappings = cfg.webhook_path_mappings ? JSON.parse(cfg.webhook_path_mappings) : []
      webhookPathMappings.value = Array.isArray(mappings) ? mappings : []
    } catch {
      webhookPathMappings.value = []
    }
  } catch {
    showToast('加载入站 Webhook 设置失败', 'error')
  }
}

async function loadOutbound() {
  loadingOutbound.value = true
  try {
    const [eventsResp, subs] = await Promise.all([
      getSupportedNotificationEvents().catch(() => ({ events: [] as NotificationEvent[] })),
      listWebhookSubscriptions(),
    ])
    supportedEvents.value = eventsResp.events || []
    subscriptions.value = subs
  } catch (err: any) {
    showToast(err?.message || '加载出站通知设置失败', 'error')
  } finally {
    loadingOutbound.value = false
  }
}

function resetDraft() {
  draft.value = {
    id: '',
    name: '',
    url: '',
    events: [...defaultOutboundEvents],
    enabled: true,
  }
}

function editSubscription(sub: WebhookSubscription) {
  draft.value = {
    id: sub.id,
    name: sub.name,
    url: sub.url,
    events: [...sub.events],
    enabled: sub.enabled,
  }
}

async function saveOutbound() {
  const name = draft.value.name.trim()
  const url = draft.value.url.trim()
  if (!url) {
    showToast('请填写目标 URL', 'error')
    return
  }
  if (draft.value.events.length === 0) {
    showToast('至少选择一个事件', 'error')
    return
  }

  savingOutbound.value = true
  try {
    const input = {
      name: name || url,
      url,
      events: draft.value.events,
      enabled: draft.value.enabled,
      group_items: false,
    }
    if (draft.value.id) {
      await updateWebhookSubscription(draft.value.id, input)
      showToast('订阅已更新', 'success')
    } else {
      await createWebhookSubscription(input)
      showToast('订阅已创建', 'success')
    }
    resetDraft()
    await loadOutbound()
  } catch (err: any) {
    showToast(err?.message || '保存订阅失败', 'error')
  } finally {
    savingOutbound.value = false
  }
}

async function removeSubscription(id: string) {
  try {
    await deleteWebhookSubscription(id)
    if (draft.value.id === id) resetDraft()
    await loadOutbound()
    showToast('订阅已删除', 'success')
  } catch (err: any) {
    showToast(err?.message || '删除订阅失败', 'error')
  }
}

async function testSubscription(id: string) {
  testingId.value = id
  try {
    await testWebhookSubscription(id)
    await loadOutbound()
    showToast('测试通知已发送', 'success')
  } catch (err: any) {
    await loadOutbound().catch(() => {})
    showToast(err?.message || '测试通知发送失败', 'error')
  } finally {
    testingId.value = ''
  }
}

async function refreshSamplePayload() {
  sampleLoading.value = true
  try {
    samplePayload.value = await getNotificationSamplePayload(sampleEvent.value)
  } catch (err: any) {
    showToast(err?.message || '加载示例负载失败', 'error')
  } finally {
    sampleLoading.value = false
  }
}

function statusTagType(status?: number) {
  if (!status) return 'default'
  if (status >= 200 && status < 300) return 'success'
  if (status >= 500 || status === 0) return 'error'
  return 'warning'
}

function statusText(status?: number) {
  if (!status) return '未发送'
  return `HTTP ${status}`
}

function formatDate(raw?: string) {
  if (!raw) return '未发送'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString()
}

watch(sampleEvent, () => {
  void refreshSamplePayload()
})

onMounted(() => {
  void loadInbound()
  void loadOutbound().then(() => refreshSamplePayload())
})
</script>

<template>
  <page-shell
    title="Webhook"
    description="管理文件变动入站回调，以及面向第三方工具的 Emby 格式出站通知。"
    :icon="AppIcons.webhook"
    body-class="webhook-layout"
  >
    <n-tabs v-model:value="activeTab" type="segment" animated class="webhook-tabs">
      <n-tab-pane name="inbound" tab="入站回调">
        <n-card :bordered="false" class="glass-card section-card tool-card">
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
            <n-button type="primary" :loading="savingWebhook" @click="saveWebhookSettings">
              <template #icon><n-icon><SaveOutline /></n-icon></template>
              保存设置
            </n-button>
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
      </n-tab-pane>

      <n-tab-pane name="outbound" tab="出站通知">
        <div class="outbound-grid">
          <n-card :bordered="false" class="glass-card section-card tool-card">
            <template #header>
              <div class="card-header-wrap">
                <div class="icon-box outbound">
                  <n-icon :size="18"><ArrowForwardOutline /></n-icon>
                </div>
                <div class="header-copy">
                  <div class="header-title">{{ draft.id ? '编辑订阅' : '新增订阅' }}</div>
                  <div class="header-desc">POST `application/json`，负载保持 Emby 原生通知格式。</div>
                </div>
              </div>
            </template>

            <n-alert type="info" :show-icon="false" class="outbound-alert">
              未启用内置刮削的媒体库也会在扫库落库后触发 `library.new`；有刮削任务时会短暂等待元数据落定。
            </n-alert>

            <n-form label-placement="top" size="small">
              <n-form-item label="名称">
                <n-input v-model:value="draft.name" placeholder="例如：emby-ai" maxlength="128" />
              </n-form-item>
              <n-form-item label="目标 URL">
                <n-input v-model:value="draft.url" placeholder="https://example.com/emby/webhook" />
              </n-form-item>
              <n-form-item label="状态">
                <div class="switch-row">
                  <n-switch v-model:value="draft.enabled" />
                  <span>{{ draft.enabled ? '启用' : '停用' }}</span>
                </div>
              </n-form-item>
              <n-form-item label="事件">
                <n-checkbox-group v-model:value="draft.events">
                  <div class="event-groups">
                    <div v-for="group in eventGroups" :key="group.title" class="event-group">
                      <div class="event-group-title">{{ group.title }}</div>
                      <div class="event-options">
                        <n-checkbox v-for="event in group.events" :key="event" :value="event">
                          {{ eventLabel(event) }}
                          <span class="event-code">{{ event }}</span>
                        </n-checkbox>
                      </div>
                    </div>
                  </div>
                </n-checkbox-group>
              </n-form-item>
            </n-form>

            <div class="card-actions split-actions">
              <n-button secondary @click="resetDraft">重置</n-button>
              <n-button type="primary" :loading="savingOutbound" @click="saveOutbound">
                <template #icon><n-icon><SaveOutline /></n-icon></template>
                {{ draft.id ? '保存订阅' : '创建订阅' }}
              </n-button>
            </div>
          </n-card>

          <n-card :bordered="false" class="glass-card section-card tool-card">
            <template #header>
              <div class="card-header-wrap">
                <div class="icon-box list">
                  <n-icon :size="18"><DocumentTextOutline /></n-icon>
                </div>
                <div class="header-copy">
                  <div class="header-title">订阅列表</div>
                  <div class="header-desc">最近一次投递状态会在测试和后台发送后更新。</div>
                </div>
              </div>
            </template>

            <div v-if="subscriptions.length === 0" class="subscriptions-empty">
              <n-empty size="small" :description="loadingOutbound ? '正在加载订阅' : '暂无出站订阅'" />
            </div>

            <div v-else class="subscriptions-list">
              <div v-for="sub in subscriptions" :key="sub.id" class="subscription-row">
                <div class="subscription-main">
                  <div class="subscription-title-row">
                    <div class="subscription-name">{{ sub.name }}</div>
                    <div class="subscription-tags">
                      <n-tag size="small" :type="sub.enabled ? 'success' : 'default'" :bordered="false">
                        {{ sub.enabled ? '启用' : '停用' }}
                      </n-tag>
                      <n-tag size="small" :type="statusTagType(sub.last_status)" :bordered="false">
                        {{ statusText(sub.last_status) }}
                      </n-tag>
                    </div>
                  </div>
                  <code class="subscription-url">{{ sub.url }}</code>
                  <div class="event-tag-row">
                    <n-tag v-for="event in sub.events" :key="event" size="small" :bordered="false">
                      {{ eventLabel(event) }}
                    </n-tag>
                  </div>
                  <div class="subscription-meta">
                    最近发送：{{ formatDate(sub.last_sent_at) }}
                    <span v-if="sub.last_error" class="last-error"> · {{ sub.last_error }}</span>
                  </div>
                </div>

                <div class="subscription-actions">
                  <n-button size="small" secondary :loading="testingId === sub.id" @click="testSubscription(sub.id)">
                    测试
                  </n-button>
                  <n-button size="small" secondary @click="editSubscription(sub)">编辑</n-button>
                  <n-popconfirm @positive-click="removeSubscription(sub.id)">
                    <template #trigger>
                      <n-button size="small" tertiary type="error">
                        <template #icon><n-icon><TrashOutline /></n-icon></template>
                      </n-button>
                    </template>
                    删除这个出站订阅？
                  </n-popconfirm>
                </div>
              </div>
            </div>
          </n-card>
        </div>

        <n-card :bordered="false" class="glass-card section-card tool-card">
          <template #header>
            <div class="sample-header">
              <div class="card-header-wrap">
                <div class="icon-box sample">
                  <n-icon :size="18"><DocumentTextOutline /></n-icon>
                </div>
                <div class="header-copy">
                  <div class="header-title">示例负载</div>
                  <div class="header-desc">用于对照第三方工具解析字段。</div>
                </div>
              </div>
              <div class="sample-actions">
                <n-select v-model:value="sampleEvent" size="small" :options="eventSelectOptions" class="sample-select" />
                <n-button secondary size="small" :loading="sampleLoading" @click="refreshSamplePayload">刷新</n-button>
                <n-button secondary size="small" :disabled="!prettySamplePayload" @click="copySamplePayload">
                  <template #icon><n-icon><CopyOutline /></n-icon></template>
                  复制
                </n-button>
              </div>
            </div>
          </template>

          <pre class="sample-preview">{{ prettySamplePayload }}</pre>
        </n-card>
      </n-tab-pane>
    </n-tabs>
  </page-shell>
</template>

<style scoped>
:deep(.webhook-layout) {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.webhook-tabs {
  width: 100%;
}

:deep(.webhook-tabs .n-tab-pane) {
  padding-top: 16px;
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

.icon-box.outbound {
  color: #0ea5e9;
  background: rgba(14, 165, 233, 0.12);
}

.icon-box.list {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.12);
}

.icon-box.sample,
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

.subsection-title,
.event-group-title {
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

.code-block,
.subscription-url {
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

.toml-preview,
.sample-preview {
  margin: 0;
  padding: 14px;
  border-radius: 12px;
  background: rgba(0,0,0,0.2);
  color: var(--app-text);
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 16px;
}

.split-actions {
  justify-content: space-between;
}

.outbound-grid {
  display: grid;
  grid-template-columns: minmax(320px, 0.9fr) minmax(360px, 1.1fr);
  gap: 16px;
  margin-bottom: 16px;
}

.outbound-alert {
  margin-bottom: 16px;
}

.switch-row {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--app-text-muted);
  font-size: 12px;
}

.event-groups {
  display: grid;
  gap: 12px;
  width: 100%;
}

.event-group {
  border: 1px solid var(--app-border);
  border-radius: 10px;
  padding: 12px;
  background: rgba(255,255,255,0.02);
}

.event-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
}

.event-code {
  margin-left: 6px;
  color: var(--app-text-muted);
  font-size: 11px;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.subscriptions-empty {
  padding: 28px 0;
}

.subscriptions-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.subscription-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--app-border);
  border-radius: 12px;
  background: rgba(255,255,255,0.02);
}

.subscription-main {
  min-width: 0;
}

.subscription-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}

.subscription-name {
  min-width: 0;
  font-size: 14px;
  font-weight: 700;
  color: var(--app-text);
  word-break: break-word;
}

.subscription-tags,
.event-tag-row,
.subscription-actions,
.sample-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.event-tag-row {
  margin-top: 10px;
}

.subscription-meta {
  margin-top: 10px;
  color: var(--app-text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.last-error {
  color: #ef4444;
}

.sample-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.sample-select {
  width: 220px;
}

@media (max-width: 980px) {
  .outbound-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .code-row,
  .secret-row,
  .sample-header {
    flex-direction: column;
    align-items: stretch;
  }

  .mappings-head,
  .mapping-row,
  .subscription-row,
  .event-options {
    grid-template-columns: 1fr;
  }

  .arrow-slot,
  .action-slot {
    justify-content: flex-start;
  }

  .sample-select {
    width: 100%;
  }
}
</style>
