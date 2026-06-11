<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useToast } from '@/composables/useToast'
import EmptyState from '@/components/EmptyState.vue'
import { NCard, NButton, NCheckbox, NSpace, NModal, NIcon, NTag, NAlert, NSpin } from 'naive-ui'
import {
  CloudDownloadOutline,
  TrashOutline,
  RefreshOutline,
  DownloadOutline,
  TimeOutline,
  CloudUploadOutline,
  DocumentOutline,
} from '@vicons/ionicons5'
import {
  createBackup,
  listBackups,
  deleteBackup,
  restoreBackup,
  uploadBackup,
  getBackupSummary,
  getBackupDownloadUrl,
  getToken,
  type BackupSummary,
} from '@/api/client'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'

const { showToast } = useToast()

// 类别定义。配置类(媒体库/平台库/网关/设置)排在前面,默认导出这些;
// 媒体信息/播放活动与具体文件、本机绑定,迁移到新实例通常不需要。
const BACKUP_CATEGORIES = [
  { key: 'libraries', label: '媒体库', icon: '📚', desc: '名称 / 类型 / 扫描路径 / 排序 / 刮削配置' },
  { key: 'platforms', label: '平台·虚拟库', icon: '🎭', desc: '片商 / 番号 / 演员虚拟库 + 整体混排顺序' },
  { key: 'gateway', label: '302 网关', icon: '🌐', desc: '115 / Alist / WebDAV 回源配置（含密钥）' },
  { key: 'settings', label: '系统设置', icon: '⚙️', desc: '扫描线程 / 文件监听 / 品牌 / 刮削开关等' },
  { key: 'users', label: '用户数据', icon: '👤', desc: '账号 / 权限 / API Key / 令牌' },
  { key: 'media', label: '媒体信息', icon: '🎬', desc: '已入库条目 / 版本 / 演职员（与文件绑定）' },
  { key: 'activity', label: '播放活动', icon: '📊', desc: '播放记录与进度' },
]

// 与后端 tablesForCategory 对应,用于把按表统计的行数聚合到类别上展示。
const CATEGORY_TABLES: Record<string, string[]> = {
  settings: ['system_config'],
  users: ['users', 'user_policies', 'api_keys', 'access_tokens', 'user_library_access'],
  libraries: ['libraries'],
  platforms: ['platform_libraries', 'library_display_order'],
  gateway: ['gateway_config'],
  media: ['genres', 'items', 'item_genres', 'cast_members', 'media_versions', 'media_streams', 'user_item_data'],
  activity: ['playback_activity'],
}

const CONFIG_CATEGORIES = ['libraries', 'platforms', 'gateway', 'settings']

const backups = ref<any[]>([])

// ===== 导出 =====
const backupSelectedCats = ref<string[]>([...CONFIG_CATEGORIES])
const backupCreating = ref(false)

// ===== 导入(上传 → 解析 → 可选恢复)=====
const fileInput = ref<HTMLInputElement | null>(null)
const importing = ref(false)
const importSummary = ref<BackupSummary | null>(null)
const importSelectedCats = ref<string[]>([])
const importRestoring = ref(false)
const dragActive = ref(false)

// ===== 历史恢复 =====
const backupRestoring = ref(false)
const backupRestoreTarget = ref<any>(null)
const backupRestoreCats = ref<string[]>([])
const restoreSummary = ref<BackupSummary | null>(null)

function catMeta(key: string) {
  return BACKUP_CATEGORIES.find((c) => c.key === key)
}
function catLabel(key: string) {
  return catMeta(key)?.label || key
}
function catCount(cat: string, counts?: Record<string, number>) {
  if (!counts) return null
  return (CATEGORY_TABLES[cat] || []).reduce((sum, t) => sum + (counts[t] || 0), 0)
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}
function formatTime(filename: string) {
  const m = filename.match(/(\d{4})(\d{2})(\d{2})_(\d{2})(\d{2})(\d{2})/)
  if (!m) return filename
  return `${m[1]}-${m[2]}-${m[3]} ${m[4]}:${m[5]}:${m[6]}`
}

async function loadBackups() {
  try {
    backups.value = (await listBackups()) || []
  } catch {
    backups.value = []
  }
}

// ===== 导出逻辑 =====
const allSelected = computed(() => backupSelectedCats.value.length === BACKUP_CATEGORIES.length)
function toggleAll(v: boolean) {
  backupSelectedCats.value = v ? BACKUP_CATEGORIES.map((c) => c.key) : []
}
function toggleCat(key: string, checked: boolean) {
  backupSelectedCats.value = checked
    ? [...backupSelectedCats.value, key]
    : backupSelectedCats.value.filter((c) => c !== key)
}
function selectConfigOnly() {
  backupSelectedCats.value = [...CONFIG_CATEGORIES]
}

function openDownload(filename: string) {
  window.open(`${getBackupDownloadUrl(filename)}?api_key=${getToken()}`, '_blank')
}

async function handleCreate() {
  if (backupSelectedCats.value.length === 0) return
  backupCreating.value = true
  try {
    const cats = allSelected.value ? ['all'] : backupSelectedCats.value
    const res = await createBackup(cats)
    await loadBackups()
    if (res?.filename) {
      openDownload(res.filename)
      showToast('已导出并开始下载', 'success')
    } else {
      showToast('导出成功', 'success')
    }
  } catch {
    showToast('导出失败', 'error')
  } finally {
    backupCreating.value = false
  }
}

// ===== 导入逻辑 =====
function triggerFilePick() {
  fileInput.value?.click()
}
function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) void ingestFile(file)
  input.value = '' // 允许重复选择同一文件
}
function onDrop(e: DragEvent) {
  e.preventDefault()
  dragActive.value = false
  const file = e.dataTransfer?.files?.[0]
  if (file) void ingestFile(file)
}
async function ingestFile(file: File) {
  if (!file.name.toLowerCase().endsWith('.json')) {
    showToast('请选择 .json 导出文件', 'error')
    return
  }
  importing.value = true
  importSummary.value = null
  try {
    const summary = await uploadBackup(file)
    importSummary.value = summary
    importSelectedCats.value = [...(summary.categories || [])]
    await loadBackups()
    showToast('解析成功，请勾选要恢复的内容', 'success')
  } catch {
    showToast('解析失败，请确认是有效的导出文件', 'error')
  } finally {
    importing.value = false
  }
}
function toggleImportCat(key: string, checked: boolean) {
  importSelectedCats.value = checked
    ? [...importSelectedCats.value, key]
    : importSelectedCats.value.filter((c) => c !== key)
}
function clearImport() {
  importSummary.value = null
  importSelectedCats.value = []
}
async function handleImportRestore() {
  if (!importSummary.value || importSelectedCats.value.length === 0) return
  importRestoring.value = true
  try {
    await restoreBackup(importSummary.value.filename, importSelectedCats.value)
    showToast('恢复成功，建议刷新页面（网关配置需重启生效）', 'success')
    clearImport()
  } catch {
    showToast('恢复失败', 'error')
  } finally {
    importRestoring.value = false
  }
}

// ===== 历史恢复逻辑 =====
async function openRestore(b: any) {
  backupRestoreTarget.value = b
  backupRestoreCats.value = [...(b.categories || [])]
  restoreSummary.value = null
  try {
    const summary = await getBackupSummary(b.filename)
    restoreSummary.value = summary
    if (summary.categories?.length) backupRestoreCats.value = [...summary.categories]
  } catch {
    // 摘要可选,失败仍可按文件头的 categories 恢复
  }
}
function toggleRestoreCat(key: string, checked: boolean) {
  backupRestoreCats.value = checked
    ? [...backupRestoreCats.value, key]
    : backupRestoreCats.value.filter((c) => c !== key)
}
async function handleRestore() {
  if (!backupRestoreTarget.value || backupRestoreCats.value.length === 0) return
  backupRestoring.value = true
  try {
    // 显式传所选类别,绝不传 'all':'all' 会在后端展开为全部 7 个类别并 TRUNCATE
    // 备份里并不存在的表,造成现有数据被清空。
    await restoreBackup(backupRestoreTarget.value.filename, backupRestoreCats.value)
    showToast('恢复成功，建议刷新页面（网关配置需重启生效）', 'success')
    backupRestoreTarget.value = null
  } catch {
    showToast('恢复失败', 'error')
  } finally {
    backupRestoring.value = false
  }
}

async function handleDelete(filename: string) {
  try {
    await deleteBackup(filename)
    showToast('已删除', 'success')
    await loadBackups()
  } catch {
    showToast('删除失败', 'error')
  }
}

const restoreCatList = computed(() => {
  const cats = restoreSummary.value?.categories || backupRestoreTarget.value?.categories || []
  return BACKUP_CATEGORIES.filter((c) => cats.includes(c.key))
})

onMounted(() => {
  void loadBackups()
})
</script>

<template>
  <page-shell
    title="备份与迁移"
    description="导出媒体库与各项配置，迁移到新的 FYMS 实例；或上传备份文件后按需恢复。"
    :icon="AppIcons.backup"
    body-class="backup-layout"
  >
    <!-- 导出 -->
    <n-card class="glass-card backup-card" :bordered="false">
      <template #header>
        <span class="card-title">导出配置</span>
      </template>
      <template #header-extra>
        <n-space :size="8" align="center">
          <n-button text size="small" @click="selectConfigOnly">仅配置</n-button>
          <n-button
            type="primary"
            size="small"
            :loading="backupCreating"
            :disabled="backupCreating || backupSelectedCats.length === 0"
            @click="handleCreate"
          >
            <template #icon><n-icon :component="DownloadOutline" /></template>
            导出并下载
          </n-button>
        </n-space>
      </template>

      <p class="card-desc">
        勾选要导出的内容，生成的 JSON 会自动下载，可在新实例的「导入」区域上传恢复。
        默认仅导出配置（媒体库 / 平台库 / 网关 / 设置）。
      </p>

      <div class="cat-grid">
        <label class="cat-item cat-item--all" :class="{ 'cat-item--checked': allSelected }">
          <n-checkbox :checked="allSelected" @update:checked="toggleAll" />
          <span class="cat-item__label">全选</span>
        </label>
        <label
          v-for="cat in BACKUP_CATEGORIES"
          :key="cat.key"
          class="cat-item"
          :class="{ 'cat-item--checked': backupSelectedCats.includes(cat.key) }"
        >
          <n-checkbox
            :checked="backupSelectedCats.includes(cat.key)"
            @update:checked="(v: boolean) => toggleCat(cat.key, v)"
          />
          <span class="cat-item__icon">{{ cat.icon }}</span>
          <span class="cat-item__text">
            <span class="cat-item__label">{{ cat.label }}</span>
            <span class="cat-item__desc">{{ cat.desc }}</span>
          </span>
        </label>
      </div>
    </n-card>

    <!-- 导入 -->
    <n-card class="glass-card backup-card" :bordered="false">
      <template #header>
        <span class="card-title">导入配置</span>
      </template>
      <template #header-extra>
        <n-button v-if="importSummary" text size="small" @click="clearImport">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          重新选择
        </n-button>
      </template>

      <!-- 上传区 -->
      <div
        v-if="!importSummary"
        class="dropzone"
        :class="{ 'dropzone--active': dragActive, 'dropzone--loading': importing }"
        @click="triggerFilePick"
        @dragover.prevent="dragActive = true"
        @dragleave.prevent="dragActive = false"
        @drop="onDrop"
      >
        <n-spin v-if="importing" size="small" />
        <n-icon v-else :component="CloudUploadOutline" :size="34" class="dropzone__icon" />
        <div class="dropzone__title">{{ importing ? '正在解析…' : '点击选择，或拖拽备份 JSON 到此处' }}</div>
        <div class="dropzone__hint">上传后会先解析展示内容，再由你勾选恢复</div>
        <input ref="fileInput" type="file" accept=".json,application/json" hidden @change="onFileChange" />
      </div>

      <!-- 解析结果 + 可选恢复 -->
      <div v-else class="import-result">
        <div class="import-result__head">
          <n-icon :component="DocumentOutline" :size="16" />
          <span class="import-result__file">{{ importSummary.filename }}</span>
          <n-tag v-if="importSummary.created_at" size="tiny" :bordered="false" round>
            {{ importSummary.created_at.slice(0, 19).replace('T', ' ') }}
          </n-tag>
        </div>

        <p class="card-desc" style="margin: 14px 0 12px">解析到以下内容，勾选要恢复的类别（括号为行数）：</p>

        <div class="cat-grid">
          <label
            v-for="cat in importSummary.categories"
            :key="cat"
            class="cat-item"
            :class="{ 'cat-item--checked': importSelectedCats.includes(cat) }"
          >
            <n-checkbox
              :checked="importSelectedCats.includes(cat)"
              @update:checked="(v: boolean) => toggleImportCat(cat, v)"
            />
            <span class="cat-item__icon">{{ catMeta(cat)?.icon || '📦' }}</span>
            <span class="cat-item__text">
              <span class="cat-item__label">
                {{ catLabel(cat) }}
                <span class="cat-item__count">({{ catCount(cat, importSummary.counts) ?? '—' }})</span>
              </span>
              <span class="cat-item__desc">{{ catMeta(cat)?.desc }}</span>
            </span>
          </label>
        </div>

        <n-alert type="warning" :bordered="false" style="margin-top: 16px">
          恢复会覆盖所选类别的当前数据，不可撤销。网关配置恢复后需重启服务才生效。
        </n-alert>

        <n-space justify="end" style="margin-top: 16px">
          <n-button
            type="primary"
            :loading="importRestoring"
            :disabled="importRestoring || importSelectedCats.length === 0"
            @click="handleImportRestore"
          >
            <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            恢复所选 ({{ importSelectedCats.length }})
          </n-button>
        </n-space>
      </div>
    </n-card>

    <!-- 历史 -->
    <n-card class="glass-card backup-card" :bordered="false">
      <template #header>
        <span class="card-title">备份历史</span>
      </template>
      <template #header-extra>
        <n-button text size="small" @click="loadBackups">
          <template #icon><n-icon :component="RefreshOutline" /></template>
        </n-button>
      </template>

      <empty-state v-if="backups.length === 0" description="暂无备份记录" />

      <div v-else class="backup-list">
        <div v-for="b in backups" :key="b.filename" class="backup-item">
          <div class="backup-item__info">
            <div class="backup-item__time">
              <n-icon :component="TimeOutline" :size="14" />
              {{ formatTime(b.filename) }}
            </div>
            <div class="backup-item__meta">
              <n-tag size="tiny" :bordered="false" round>{{ formatSize(b.size) }}</n-tag>
              <n-tag v-for="cat in (b.categories || [])" :key="cat" size="tiny" :bordered="false" round>
                {{ catLabel(cat) }}
              </n-tag>
            </div>
          </div>
          <div class="backup-item__actions">
            <n-button text size="small" @click="openDownload(b.filename)">
              <template #icon><n-icon :component="DownloadOutline" /></template>
            </n-button>
            <n-button text size="small" type="primary" @click="openRestore(b)">
              <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            </n-button>
            <n-button text size="small" type="error" @click="handleDelete(b.filename)">
              <template #icon><n-icon :component="TrashOutline" /></template>
            </n-button>
          </div>
        </div>
      </div>
    </n-card>
  </page-shell>

  <!-- 历史恢复弹窗 -->
  <n-modal
    :show="!!backupRestoreTarget"
    @update:show="(v: boolean) => { if (!v) backupRestoreTarget = null }"
    preset="card"
    title="恢复备份"
    class="glass-modal"
    style="width: 460px; max-width: 90vw"
  >
    <p class="modal-desc">
      从 <code>{{ backupRestoreTarget?.filename }}</code> 恢复，勾选要恢复的类别：
    </p>
    <div class="restore-cats">
      <label
        v-for="cat in restoreCatList"
        :key="cat.key"
        class="restore-cat"
        :class="{ 'restore-cat--checked': backupRestoreCats.includes(cat.key) }"
      >
        <n-checkbox
          :checked="backupRestoreCats.includes(cat.key)"
          @update:checked="(v: boolean) => toggleRestoreCat(cat.key, v)"
        />
        <span class="cat-item__icon">{{ cat.icon }}</span>
        <span class="cat-item__label">
          {{ cat.label }}
          <span v-if="restoreSummary" class="cat-item__count">({{ catCount(cat.key, restoreSummary.counts) ?? '—' }})</span>
        </span>
      </label>
    </div>

    <n-alert type="warning" :bordered="false" style="margin-top: 16px">
      恢复将覆盖当前数据，此操作不可撤销。建议先创建一个新备份。
    </n-alert>

    <template #footer>
      <n-space justify="end">
        <n-button @click="backupRestoreTarget = null">取消</n-button>
        <n-button
          type="error"
          :loading="backupRestoring"
          :disabled="backupRestoring || backupRestoreCats.length === 0"
          @click="handleRestore"
        >
          确认恢复
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
:deep(.backup-layout) {
  max-width: 820px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--app-section-gap);
}

.backup-card {
  overflow: hidden;
}

.card-title {
  font-size: 16px;
  font-weight: 600;
}

.card-desc {
  font-size: 13px;
  color: var(--app-text-muted);
  margin-bottom: 18px;
  line-height: 1.6;
}

/* Category selector grid */
.cat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px;
}

.cat-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-radius: var(--app-radius);
  border: 1px solid var(--app-border);
  background: transparent;
  cursor: pointer;
  transition: border-color 0.2s ease, background 0.2s ease;
}

.cat-item:hover {
  border-color: var(--app-border-hover);
  background: rgba(148, 163, 184, 0.04);
}

.cat-item--checked {
  border-color: rgba(var(--app-primary-rgb), 0.3);
  background: rgba(var(--app-primary-rgb), 0.06);
}

.cat-item--all {
  border-style: dashed;
  grid-column: 1 / -1;
}

.cat-item__icon {
  font-size: 18px;
  line-height: 1;
  flex-shrink: 0;
}

.cat-item__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.cat-item__label {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
}

.cat-item__count {
  font-size: 12px;
  font-weight: 400;
  color: var(--app-text-muted);
  font-variant-numeric: tabular-nums;
}

.cat-item__desc {
  font-size: 11px;
  color: var(--app-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Dropzone */
.dropzone {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 36px 20px;
  border: 1.5px dashed var(--app-border);
  border-radius: var(--app-radius);
  cursor: pointer;
  text-align: center;
  transition: border-color 0.2s ease, background 0.2s ease;
}
.dropzone:hover,
.dropzone--active {
  border-color: rgba(var(--app-primary-rgb), 0.5);
  background: rgba(var(--app-primary-rgb), 0.05);
}
.dropzone--loading {
  cursor: default;
}
.dropzone__icon {
  color: var(--app-text-muted);
}
.dropzone__title {
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
}
.dropzone__hint {
  font-size: 12px;
  color: var(--app-text-muted);
}

/* Import result */
.import-result__head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--app-text);
}
.import-result__file {
  font-weight: 500;
  word-break: break-all;
}

/* Backup list */
.backup-list {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.backup-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 0;
  border-bottom: 1px solid var(--app-border);
  transition: background 0.15s ease;
}

.backup-item:last-child {
  border-bottom: none;
}

.backup-item__info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.backup-item__time {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
  font-variant-numeric: tabular-nums;
}

.backup-item__meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.backup-item__actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

/* Restore modal */
.modal-desc {
  font-size: 13px;
  color: var(--app-text-muted);
  margin-bottom: 16px;
  line-height: 1.6;
}

.modal-desc code {
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(148, 163, 184, 0.1);
  font-size: 12px;
  color: var(--app-text);
}

.restore-cats {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.restore-cat {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--app-radius);
  border: 1px solid var(--app-border);
  cursor: pointer;
  transition: border-color 0.2s ease, background 0.2s ease;
}
.restore-cat--checked {
  border-color: rgba(var(--app-primary-rgb), 0.3);
  background: rgba(var(--app-primary-rgb), 0.06);
}

@media (max-width: 640px) {
  .backup-item {
    flex-direction: column;
    align-items: flex-start;
  }

  .backup-item__actions {
    align-self: flex-end;
  }
}
</style>
