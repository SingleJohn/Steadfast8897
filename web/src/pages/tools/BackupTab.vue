<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useToast } from '@/composables/useToast'
import EmptyState from '@/components/EmptyState.vue'
import { NCard, NButton, NCheckbox, NSpace, NModal, NIcon, NTag, NAlert } from 'naive-ui'
import {
  CloudDownloadOutline,
  TrashOutline,
  RefreshOutline,
  AddOutline,
  DownloadOutline,
  TimeOutline,
} from '@vicons/ionicons5'
import {
  createBackup,
  listBackups,
  deleteBackup,
  restoreBackup,
  getBackupDownloadUrl,
  getToken,
} from '@/api/client'

const { showToast } = useToast()

const BACKUP_CATEGORIES = [
  { key: 'settings', label: '系统设置', icon: '⚙️' },
  { key: 'users', label: '用户数据', icon: '👤' },
  { key: 'libraries', label: '媒体库', icon: '📚' },
  { key: 'media', label: '媒体信息', icon: '🎬' },
  { key: 'activity', label: '播放活动', icon: '📊' },
]

const backups = ref<any[]>([])
const backupSelectedCats = ref<string[]>(['settings', 'users', 'libraries', 'media', 'activity'])
const backupCreating = ref(false)
const backupRestoring = ref(false)
const backupRestoreTarget = ref<any>(null)
const backupRestoreCats = ref<string[]>([])

async function loadBackups() {
  try {
    backups.value = (await listBackups()) || []
  } catch {
    backups.value = []
  }
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}

function catLabel(key: string) {
  return BACKUP_CATEGORIES.find((c) => c.key === key)?.label || key
}

function formatTime(filename: string) {
  const m = filename.match(/(\d{4})(\d{2})(\d{2})_(\d{2})(\d{2})(\d{2})/)
  if (!m) return filename
  return `${m[1]}-${m[2]}-${m[3]} ${m[4]}:${m[5]}:${m[6]}`
}

async function handleCreate() {
  if (backupSelectedCats.value.length === 0) return
  backupCreating.value = true
  try {
    const cats =
      backupSelectedCats.value.length === BACKUP_CATEGORIES.length ? ['all'] : backupSelectedCats.value
    await createBackup(cats)
    showToast('备份创建成功', 'success')
    await loadBackups()
  } catch {
    showToast('备份失败', 'error')
  } finally {
    backupCreating.value = false
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

async function handleRestore() {
  if (!backupRestoreTarget.value || backupRestoreCats.value.length === 0) return
  backupRestoring.value = true
  try {
    const cats =
      backupRestoreCats.value.length === BACKUP_CATEGORIES.length ? ['all'] : backupRestoreCats.value
    await restoreBackup(backupRestoreTarget.value.filename, cats)
    showToast('恢复成功，建议刷新页面', 'success')
    backupRestoreTarget.value = null
  } catch {
    showToast('恢复失败', 'error')
  } finally {
    backupRestoring.value = false
  }
}

function openRestore(b: any) {
  backupRestoreTarget.value = b
  backupRestoreCats.value = [...(b.categories || [])]
}

function openDownload(filename: string) {
  window.open(`${getBackupDownloadUrl(filename)}?api_key=${getToken()}`, '_blank')
}

const allSelected = () => backupSelectedCats.value.length === BACKUP_CATEGORIES.length
function toggleAll(v: boolean) {
  backupSelectedCats.value = v ? BACKUP_CATEGORIES.map((c) => c.key) : []
}
function toggleCat(key: string, checked: boolean) {
  backupSelectedCats.value = checked
    ? [...backupSelectedCats.value, key]
    : backupSelectedCats.value.filter((c) => c !== key)
}

onMounted(() => {
  void loadBackups()
})
</script>

<template>
  <div class="backup-layout">
    <!-- Create -->
    <n-card class="glass-card backup-card" :bordered="false">
      <template #header>
        <span class="card-title">创建备份</span>
      </template>
      <template #header-extra>
        <n-button
          type="primary"
          size="small"
          :loading="backupCreating"
          :disabled="backupCreating || backupSelectedCats.length === 0"
          @click="handleCreate"
        >
          <template #icon><n-icon :component="AddOutline" /></template>
          创建
        </n-button>
      </template>

      <p class="card-desc">选择要备份的数据类别，备份文件将保存在服务器上。</p>

      <div class="cat-grid">
        <label
          class="cat-item cat-item--all"
          :class="{ 'cat-item--checked': allSelected() }"
        >
          <n-checkbox :checked="allSelected()" @update:checked="toggleAll" />
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
          <span class="cat-item__label">{{ cat.label }}</span>
        </label>
      </div>
    </n-card>

    <!-- History -->
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
              <n-tag
                v-for="cat in (b.categories || [])"
                :key="cat"
                size="tiny"
                :bordered="false"
                round
              >
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
  </div>

  <!-- Restore Modal -->
  <n-modal
    :show="!!backupRestoreTarget"
    @update:show="(v: boolean) => { if (!v) backupRestoreTarget = null }"
    preset="card"
    title="恢复备份"
    class="glass-modal"
    style="width: 440px; max-width: 90vw"
  >
    <p class="modal-desc">
      从 <code>{{ backupRestoreTarget?.filename }}</code> 恢复，选择要恢复的数据类别：
    </p>
    <div class="restore-cats">
      <n-checkbox
        v-for="cat in BACKUP_CATEGORIES.filter(c => (backupRestoreTarget?.categories || []).includes(c.key))"
        :key="cat.key"
        :checked="backupRestoreCats.includes(cat.key)"
        @update:checked="(checked: boolean) => {
          backupRestoreCats = checked
            ? [...backupRestoreCats, cat.key]
            : backupRestoreCats.filter(c => c !== cat.key)
        }"
      >
        {{ cat.icon }} {{ cat.label }}
      </n-checkbox>
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
.backup-layout {
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
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.cat-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
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
}

.cat-item__icon {
  font-size: 16px;
  line-height: 1;
}

.cat-item__label {
  font-size: 13px;
  font-weight: 500;
  color: var(--app-text);
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
  gap: 10px;
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
