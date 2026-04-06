<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useToast } from '@/composables/useToast'
import { NButton, NCard, NEmpty, NForm, NFormItem, NGrid, NGridItem, NIcon, NInput, NProgress, NSelect, NSwitch } from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { SearchOutline, VideocamOutline, AddOutline, CloseOutline, ArrowForwardOutline } from '@vicons/ionicons5'
import {
  getSystemConfig, updateSystemConfig,
  scrapeAllMetadata, stopScrape,
  startProbe, stopProbe, getTaskSummary,
} from '@/api/client'

const { showToast } = useToast()

const tmdbLanguageOptions = [
  { label: 'zh-CN (简体中文)', value: 'zh-CN' },
  { label: 'en-US (English)', value: 'en-US' },
  { label: 'ja-JP (日本语)', value: 'ja-JP' },
]
const scrapeSaveModeOptions = [
  { label: '数据库（图片存服务器 data/）', value: 'database' },
  { label: '媒体目录（NFO+图片写到文件夹）', value: 'media_dir' },
  { label: '两者都写', value: 'both' },
]
const probeThreadsOptions = [1, 2, 3, 5, 8, 10, 15, 20].map((n) => ({ label: String(n), value: String(n) }))

const tmdbApiKeys = ref<string[]>([''])
const tmdbLanguage = ref('zh-CN')
const tmdbProxy = ref('')
const scrapeSaveMode = ref('database')
const autoScrape = ref(false)
const showApiKey = ref(false)
const savingConfig = ref(false)
const scraping = ref(false)
const scrapeProgress = ref<any>(null)

const probeProgress = ref<any>(null)
const probeThreads = ref('5')
const probePathMappings = ref<{ from: string; to: string }[]>([])
const savingProbe = ref(false)

async function refreshTaskSummary() {
  try {
    const summary = await getTaskSummary()
    scrapeProgress.value = summary.scrape
    probeProgress.value = summary.probe
  } catch {}
}

async function handleSaveConfig() {
  savingConfig.value = true
  try {
    await updateSystemConfig({
      tmdb_api_key: tmdbApiKeys.value.filter((k) => k.trim()).join(','),
      tmdb_language: tmdbLanguage.value,
      auto_scrape_enabled: String(autoScrape.value),
      tmdb_proxy: tmdbProxy.value,
      scrape_save_mode: scrapeSaveMode.value,
    })
    showToast('元数据设置已保存', 'success')
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    savingConfig.value = false
  }
}

async function handleScrapeAll() {
  scraping.value = true
  try {
    await scrapeAllMetadata()
    await refreshTaskSummary()
    showToast('正在刮削缺失元数据，这可能需要一些时间...', 'success')
  } catch {
    showToast('启动元数据刮削失败', 'error')
  }
  setTimeout(() => { scraping.value = false }, 3000)
}

async function saveProbeSettingsOnly() {
  try {
    await updateSystemConfig({
      probe_threads: probeThreads.value,
      probe_path_mappings: JSON.stringify(probePathMappings.value.filter((m) => m.from && m.to)),
    })
    showToast('探测设置已保存', 'success')
  } catch {
    showToast('保存失败', 'error')
  }
}

async function startProbeJob() {
  savingProbe.value = true
  try {
    await updateSystemConfig({
      probe_threads: probeThreads.value,
      probe_path_mappings: JSON.stringify(probePathMappings.value.filter((m) => m.from && m.to)),
    })
    await startProbe(parseInt(probeThreads.value, 10))
    await refreshTaskSummary()
    showToast('媒体信息探测已启动', 'success')
  } catch (err: any) {
    showToast(err.message || '启动探测失败', 'error')
  } finally {
    savingProbe.value = false
  }
}

async function stopProbeJob() {
  await stopProbe()
  await refreshTaskSummary()
  showToast('正在停止探测...', 'success')
}

const timers: ReturnType<typeof setInterval>[] = []

onMounted(() => {
  getSystemConfig().then((cfg: any) => {
    const keys = (cfg.tmdb_api_key || '').split(',').map((k: string) => k.trim()).filter((k: string) => k)
    tmdbApiKeys.value = keys.length > 0 ? keys : ['']
    tmdbLanguage.value = cfg.tmdb_language || 'zh-CN'
    autoScrape.value = cfg.auto_scrape_enabled === true || cfg.auto_scrape_enabled === 'true'
    tmdbProxy.value = cfg.tmdb_proxy || ''
    scrapeSaveMode.value = cfg.scrape_save_mode || 'database'
    try { probePathMappings.value = cfg.probe_path_mappings ? JSON.parse(cfg.probe_path_mappings) : [] } catch { probePathMappings.value = [] }
    probeThreads.value = cfg.probe_threads || '5'
  }).catch(() => {})
  void refreshTaskSummary()

  timers.push(setInterval(() => {
    void refreshTaskSummary()
  }, 3000))
})

onUnmounted(() => timers.forEach((t) => clearInterval(t)))
</script>

<template>
  <page-shell title="元数据" :icon="AppIcons.metadata" description="TMDB 刮削与 FFprobe 探测配置" body-class="config-content">
    <div class="two-col">
      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box tmdb">
              <n-icon :size="18"><SearchOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">TMDB 刮削</div>
              <div class="header-desc">配置 API Key、语言与保存方式，用于自动补全媒体元数据。</div>
            </div>
          </div>
        </template>

        <div v-if="scrapeProgress" class="stats-grid">
          <div class="stat-box">
            <div class="stat-value">{{ (scrapeProgress.status === 'running' || scrapeProgress.status === 'stopping') ? Math.max((scrapeProgress.total_items || 0) - (scrapeProgress.processed_items || 0), 0) : (scrapeProgress.missing_count || 0) }}</div>
            <div class="stat-name">待刮削<template v-if="scrapeProgress.items_total"> / {{ scrapeProgress.items_total }} 总项</template></div>
          </div>
          <div class="stat-box">
            <div class="stat-value ok">{{ scrapeProgress.success_items || 0 }}</div>
            <div class="stat-name">成功</div>
          </div>
          <div class="stat-box">
            <div class="stat-value err">{{ scrapeProgress.failed_items || 0 }}</div>
            <div class="stat-name">失败</div>
          </div>
        </div>

        <div v-if="scrapeProgress && (scrapeProgress.status === 'running' || scrapeProgress.status === 'stopping')" class="progress-panel">
          <div class="progress-row">
            <n-progress type="line" :percentage="scrapeProgress.percentage" :show-indicator="false" :color="scrapeProgress.status === 'stopping' ? '#ff9800' : undefined" style="flex: 1" />
            <span class="pct">{{ scrapeProgress.percentage }}%</span>
          </div>
          <div class="panel-meta">
            {{ scrapeProgress.processed_items }}/{{ scrapeProgress.total_items }}
            <span>成功 {{ scrapeProgress.success_items }}</span>
            <span>失败 {{ scrapeProgress.failed_items }}</span>
            <span v-if="scrapeProgress.current_item">当前: {{ scrapeProgress.current_item }}</span>
          </div>
          <div v-if="scrapeProgress.last_error" class="panel-error">{{ scrapeProgress.last_error }}</div>
        </div>
        <div v-else-if="scrapeProgress?.status === 'completed'" class="success-note">
          刮削完成: {{ scrapeProgress.success_items }} 成功, {{ scrapeProgress.failed_items }} 失败
        </div>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">访问密钥</div>
            <div v-for="(key, idx) in tmdbApiKeys" :key="idx" class="api-key-row">
              <span class="row-index">{{ idx + 1 }}</span>
              <n-input :value="tmdbApiKeys[idx]" @update:value="(v: string) => tmdbApiKeys[idx] = v" :type="showApiKey ? 'text' : 'password'" :placeholder="`TMDB API Key ${idx + 1}`" size="small" />
              <n-button v-if="tmdbApiKeys.length > 1" quaternary circle type="error" size="small" @click="tmdbApiKeys = tmdbApiKeys.filter((_, i) => i !== idx)">
                <template #icon><n-icon><CloseOutline /></n-icon></template>
              </n-button>
            </div>
            <div class="inline-actions">
              <n-button secondary size="tiny" @click="tmdbApiKeys = [...tmdbApiKeys, '']">
                <template #icon><n-icon><AddOutline /></n-icon></template>
                添加 Key
              </n-button>
              <n-button quaternary size="tiny" @click="showApiKey = !showApiKey">{{ showApiKey ? '隐藏 Key' : '显示 Key' }}</n-button>
            </div>
            <div class="hint-text">支持多个 Key 轮询使用，避免单个 Key 触发风控。</div>
          </div>

          <div class="subsection">
            <div class="subsection-title">基础设置</div>
            <n-grid cols="1 m:2" x-gap="12" responsive="screen">
              <n-grid-item>
                <n-form-item label="元数据语言">
                  <n-select v-model:value="tmdbLanguage" :options="tmdbLanguageOptions" size="small" />
                </n-form-item>
              </n-grid-item>
              <n-grid-item>
                <n-form-item label="保存位置">
                  <n-select v-model:value="scrapeSaveMode" :options="scrapeSaveModeOptions" size="small" />
                </n-form-item>
              </n-grid-item>
            </n-grid>
            <n-form-item label="代理">
              <n-input v-model:value="tmdbProxy" placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080" size="small" />
            </n-form-item>
            <div class="hint-text">支持 HTTP / HTTPS / SOCKS5，留空则直接访问 TMDB。</div>
          </div>

          <div class="subsection switch-section">
            <div class="switch-copy">
              <div class="switch-title">自动刮削</div>
              <div class="hint-text">新媒体入库时自动抓取元数据。</div>
            </div>
            <n-switch v-model:value="autoScrape" :round="false" />
          </div>
        </n-form>

        <div class="card-actions">
          <n-button type="primary" size="small" :loading="savingConfig" @click="handleSaveConfig">保存设置</n-button>
          <n-button v-if="scrapeProgress?.status !== 'running' && scrapeProgress?.status !== 'stopping'" secondary size="small" :loading="scraping" :disabled="scraping || (scrapeProgress?.missing_count === 0 && scrapeProgress?.status === 'idle')" @click="handleScrapeAll">刮削缺失元数据</n-button>
          <n-button v-else type="warning" size="small" :disabled="scrapeProgress?.status === 'stopping'" @click="async () => { await stopScrape(); await refreshTaskSummary() }">{{ scrapeProgress?.status === 'stopping' ? '停止中...' : '停止刮削' }}</n-button>
        </div>
      </n-card>

      <n-card :bordered="false" class="glass-card section-card metadata-card">
        <template #header>
          <div class="card-header-wrap">
            <div class="icon-box probe">
              <n-icon :size="18"><VideocamOutline /></n-icon>
            </div>
            <div class="header-copy">
              <div class="header-title">媒体信息探测</div>
              <div class="header-desc">对缺少媒体信息的 `strm` 执行 ffprobe 探测，补充视频与音频流信息。</div>
            </div>
          </div>
        </template>
        <template #header-extra>
          <span class="header-badge">FFprobe</span>
        </template>

        <div v-if="probeProgress" class="stats-grid">
          <div class="stat-box">
            <div class="stat-value">{{ probeProgress.status === 'idle' ? probeProgress.missingCount : probeProgress.totalItems - probeProgress.processedItems }}</div>
            <div class="stat-name">待探测<template v-if="probeProgress.versionsTotal"> / {{ probeProgress.versionsTotal }} 总版本</template></div>
          </div>
          <div class="stat-box">
            <div class="stat-value ok">{{ probeProgress.successItems || 0 }}</div>
            <div class="stat-name">成功</div>
          </div>
          <div class="stat-box">
            <div class="stat-value err">{{ probeProgress.failedItems || 0 }}</div>
            <div class="stat-name">失败</div>
          </div>
        </div>

        <div v-if="probeProgress && (probeProgress.status === 'running' || probeProgress.status === 'stopping')" class="progress-panel">
          <div class="progress-row">
            <n-progress type="line" :percentage="probeProgress.percentage" :show-indicator="false" :color="probeProgress.status === 'stopping' ? '#ff9800' : undefined" style="flex: 1" />
            <span class="pct">{{ probeProgress.percentage }}%</span>
          </div>
          <div class="panel-meta">
            {{ probeProgress.processedItems }}/{{ probeProgress.totalItems }}
            <span v-if="probeProgress.currentItem">当前: {{ probeProgress.currentItem }}</span>
            <span v-if="probeProgress.status === 'stopping'">正在停止...</span>
          </div>
        </div>
        <div v-else-if="probeProgress?.status === 'completed'" class="success-note">
          探测完成: {{ probeProgress.successItems }} 成功, {{ probeProgress.failedItems }} 失败
        </div>

        <n-form label-placement="top" size="small" class="config-form">
          <div class="subsection">
            <div class="subsection-title">执行设置</div>
            <n-grid cols="1 m:2" x-gap="12" responsive="screen">
              <n-grid-item span="1">
                <n-form-item label="并发线程">
                  <n-select v-model:value="probeThreads" :options="probeThreadsOptions" :disabled="probeProgress?.status === 'running'" size="small" />
                </n-form-item>
              </n-grid-item>
            </n-grid>
          </div>

          <div class="subsection">
            <div class="subsection-title">路径映射</div>
            <div class="hint-text mapping-hint">将 `strm` 中的路径映射到当前机器可访问的挂载路径。</div>
            <div class="mappings-box">
              <div class="mappings-head">
                <div>源路径</div>
                <div class="arrow-slot"></div>
                <div>目标路径</div>
                <div class="action-slot"></div>
              </div>

              <div v-if="probePathMappings.length === 0" class="mappings-empty">
                <n-empty size="small" description="暂无路径映射" />
              </div>

              <div v-else class="mappings-list">
                <div v-for="(m, i) in probePathMappings" :key="i" class="mapping-row">
                  <n-input v-model:value="m.from" placeholder="/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" />
                  <div class="arrow-slot">
                    <n-icon depth="3"><ArrowForwardOutline /></n-icon>
                  </div>
                  <n-input v-model:value="m.to" placeholder="/mnt/CloudNAS3/" size="small" :disabled="probeProgress?.status === 'running'" />
                  <div class="action-slot">
                    <n-button quaternary circle type="error" size="small" :disabled="probeProgress?.status === 'running'" @click="probePathMappings = probePathMappings.filter((_, idx) => idx !== i)">
                      <template #icon><n-icon><CloseOutline /></n-icon></template>
                    </n-button>
                  </div>
                </div>
              </div>

              <div class="mappings-footer">
                <n-button dashed size="small" block :disabled="probeProgress?.status === 'running'" @click="probePathMappings = [...probePathMappings, { from: '', to: '' }]">
                  <template #icon><n-icon><AddOutline /></n-icon></template>
                  添加映射
                </n-button>
              </div>
            </div>
          </div>
        </n-form>

        <div class="card-actions">
          <n-button v-if="probeProgress?.status !== 'running' && probeProgress?.status !== 'stopping'" type="primary" size="small" :loading="savingProbe" :disabled="savingProbe || (probeProgress?.missingCount === 0 && probeProgress?.status === 'idle')" @click="startProbeJob">开始探测</n-button>
          <n-button v-else type="warning" size="small" :disabled="probeProgress?.status === 'stopping'" @click="stopProbeJob">{{ probeProgress?.status === 'stopping' ? '停止中...' : '停止探测' }}</n-button>
          <n-button secondary size="small" :disabled="probeProgress?.status === 'running'" @click="saveProbeSettingsOnly">保存设置</n-button>
        </div>
      </n-card>
    </div><!-- /two-col -->
  </page-shell>
</template>

<style scoped>
.config-content {
  max-width: 1200px;
  margin: 0 auto;
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  align-items: stretch;
}

@media (max-width: 900px) {
  .two-col {
    grid-template-columns: 1fr;
  }
}

.metadata-card {
  display: flex;
  flex-direction: column;
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
  background: var(--c-slate-100);
  color: var(--app-primary);
}

.app-dark .icon-box {
  background: var(--c-slate-800);
}

.icon-box.tmdb {
  color: #0ea5e9;
  background: rgba(14, 165, 233, 0.12);
}

.icon-box.probe {
  color: #10b981;
  background: rgba(16, 185, 129, 0.12);
}

.header-copy {
  min-width: 0;
}

.header-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
  line-height: 1.3;
}

.header-desc {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.header-badge {
  font-size: 10px;
  font-weight: 600;
  color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.1);
  padding: 2px 7px;
  border-radius: 4px;
  letter-spacing: 0.3px;
}

.config-form {
  margin-top: 16px;
  flex: 1;
}

.subsection {
  border: 1px solid var(--app-border, rgba(255,255,255,0.05));
  border-radius: 12px;
  padding: 14px;
  background: rgba(255,255,255,0.02);
  margin-bottom: 14px;
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
  opacity: 0.8;
}

.api-key-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.row-index {
  font-size: 11px;
  color: var(--app-text-muted);
  width: 18px;
  text-align: right;
  flex-shrink: 0;
  opacity: 0.6;
}

.inline-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  flex-wrap: wrap;
}

.switch-section {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.switch-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.switch-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
}

.card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 14px;
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
  margin-top: auto;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.stat-box {
  padding: 12px;
  border-radius: 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  background: rgba(255,255,255,0.02);
}

.stat-value {
  font-size: 24px;
  font-weight: 700;
  line-height: 1.2;
  color: var(--app-text);
}

.stat-value.ok {
  color: #22c55e;
}

.stat-value.err {
  color: var(--app-danger, #e53935);
}

.stat-name {
  margin-top: 4px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.progress-panel {
  margin-top: 16px;
  padding: 12px;
  background: rgba(128,128,128,0.04);
  border-radius: 10px;
}

.progress-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.pct {
  font-size: 11px;
  color: var(--app-primary);
  font-weight: 600;
  min-width: 32px;
  text-align: right;
}

.panel-meta {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 8px;
  font-size: 11px;
  color: var(--app-text-muted);
}

.panel-error {
  margin-top: 6px;
  font-size: 11px;
  color: var(--app-danger, #e53935);
}

.success-note {
  margin-top: 16px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(34, 197, 94, 0.08);
  color: #16a34a;
  font-size: 12px;
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

.arrow-slot,
.action-slot {
  display: flex;
  align-items: center;
  justify-content: center;
}

.mappings-footer {
  margin-top: 12px;
}

@media (max-width: 500px) {
  .stats-grid {
    grid-template-columns: 1fr;
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
