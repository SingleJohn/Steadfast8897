<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  NButton, NInput, NSelect, NModal, NSpace, NIcon, NSpin, NTag, NScrollbar,
} from 'naive-ui'
import { FolderOutline, CloudUploadOutline, TrashOutline, RefreshOutline } from '@vicons/ionicons5'
import {
  getLibraryDetail, updateLibraryInfo, deleteLibraryById,
  addLibraryPath, removeLibraryPath, refreshSingleLibrary,
  uploadLibraryImage, deleteLibraryImage, browseDirectories,
} from '../api/client'
import { useToast } from '../composables/useToast'

const props = defineProps<{
  libraryId: string | null
}>()

const emit = defineEmits<{
  close: []
  deleted: []
  updated: []
}>()

const { showToast } = useToast()

const library = ref<any>(null)
const loading = ref(true)
const name = ref('')
const collectionType = ref('movies')
const saving = ref(false)
const scanning = ref(false)

const newPath = ref('')
const addingPath = ref(false)
const showBrowser = ref(false)
const browserPath = ref('/mnt')
const browserDirs = ref<{ Name: string; Path: string }[]>([])
const browserLoading = ref(false)
const uploadingImage = ref(false)
const imageTag = ref<string | null>(null)
const showDeleteConfirm = ref(false)
const coverKey = ref(0)

const typeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
]
const typeLabels: Record<string, string> = { movies: '电影', tvshows: '电视剧' }
const solidModalMenuProps = { class: 'solid-modal-menu' }

const visible = ref(false)

watch(() => props.libraryId, (id) => {
  if (id) {
    visible.value = true
    loadLibrary(id)
  } else {
    visible.value = false
  }
}, { immediate: true })

function handleClose() {
  visible.value = false
  emit('close')
}

function coverUrl() {
  const tag = imageTag.value || library.value?.ImageTag
  if (!tag || !props.libraryId) return ''
  return `/Items/${props.libraryId}/Images/Primary?tag=${tag}&v=${coverKey.value}`
}

async function loadLibrary(id?: string) {
  const libId = id || props.libraryId
  if (!libId) return
  loading.value = true
  try {
    const lib = await getLibraryDetail(libId)
    library.value = lib
    name.value = lib.Name
    collectionType.value = lib.CollectionType
    imageTag.value = lib.ImageTag || null
  } catch {
    showToast('加载媒体库信息失败', 'error')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  if (!props.libraryId || !name.value.trim()) return
  saving.value = true
  try {
    await updateLibraryInfo(props.libraryId, { Name: name.value.trim(), CollectionType: collectionType.value })
    showToast('媒体库设置已保存', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('保存失败', 'error')
  } finally {
    saving.value = false
  }
}

async function handleAddPath() {
  if (!props.libraryId || !newPath.value.trim()) return
  addingPath.value = true
  try {
    await addLibraryPath(props.libraryId, newPath.value.trim())
    newPath.value = ''
    showToast('文件夹已添加', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('添加文件夹失败', 'error')
  } finally {
    addingPath.value = false
  }
}

async function handleRemovePath(pathToRemove: string) {
  if (!props.libraryId) return
  if (library.value?.Locations?.length <= 1) {
    showToast('至少需要保留一个文件夹', 'error')
    return
  }
  try {
    await removeLibraryPath(props.libraryId, pathToRemove)
    showToast('文件夹已移除', 'success')
    await loadLibrary()
    emit('updated')
  } catch {
    showToast('移除文件夹失败', 'error')
  }
}

async function handleScan() {
  if (!props.libraryId) return
  scanning.value = true
  try {
    await refreshSingleLibrary(props.libraryId)
    showToast('媒体库扫描已开始', 'success')
  } catch {
    showToast('启动扫描失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function handleDelete() {
  if (!props.libraryId) return
  try {
    await deleteLibraryById(props.libraryId)
    showToast('媒体库已删除', 'success')
    showDeleteConfirm.value = false
    emit('deleted')
  } catch {
    showToast('删除媒体库失败', 'error')
  }
}

async function loadBrowserDir(path: string) {
  browserLoading.value = true
  try {
    const res = await browseDirectories(path)
    browserPath.value = res.Path
    browserDirs.value = res.Directories || []
  } catch {
    showToast('无法读取目录', 'error')
  } finally {
    browserLoading.value = false
  }
}

function openBrowser() {
  showBrowser.value = true
  loadBrowserDir('/mnt')
}
function selectBrowserPath() {
  newPath.value = browserPath.value
  showBrowser.value = false
}

function parentDir(): string {
  const p = browserPath.value
  if (p === '/') return '/'
  const idx = p.lastIndexOf('/')
  return idx <= 0 ? '/' : p.substring(0, idx) || '/'
}

async function onCoverChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !props.libraryId) return
  uploadingImage.value = true
  try {
    const res = await uploadLibraryImage(props.libraryId, file)
    imageTag.value = res.ImageTag
    coverKey.value++
    showToast('封面已上传', 'success')
    emit('updated')
  } catch {
    showToast('上传失败', 'error')
  } finally {
    uploadingImage.value = false
    input.value = ''
  }
}

async function onDeleteCover() {
  if (!props.libraryId) return
  try {
    await deleteLibraryImage(props.libraryId)
    imageTag.value = null
    if (library.value) library.value.ImageTag = null
    showToast('封面已删除', 'success')
    emit('updated')
  } catch {
    showToast('删除封面失败', 'error')
  }
}
</script>

<template>
  <n-modal
    :show="visible"
    preset="card"
    :title="library?.Name || '编辑媒体库'"
    style="width: 620px; max-width: 92vw"
    :mask-closable="true"
    @update:show="(v: boolean) => { if (!v) handleClose() }"
  >
    <!-- Loading -->
    <div v-if="loading || !library" style="padding: 40px; text-align: center">
      <n-spin size="medium" />
    </div>

    <template v-else>
      <!-- Banner: cover + info side by side -->
      <div class="em-banner">
        <div class="em-cover-wrap">
          <div class="em-cover-ratio">
            <img v-if="coverUrl()" :src="coverUrl()" alt="cover" class="em-cover-img" />
            <div v-else class="em-cover-placeholder">
              <span class="em-cover-emoji">{{ library.CollectionType === 'movies' ? '🎬' : '📺' }}</span>
            </div>
          </div>
          <div class="em-cover-actions">
            <label class="em-cover-btn">
              <n-icon :size="13"><CloudUploadOutline /></n-icon>
              {{ uploadingImage ? '...' : '上传' }}
              <input type="file" accept="image/*" style="display: none" :disabled="uploadingImage" @change="onCoverChange" />
            </label>
            <button v-if="coverUrl()" class="em-cover-btn em-cover-btn-del" @click="onDeleteCover">
              <n-icon :size="13"><TrashOutline /></n-icon>
              删除
            </button>
          </div>
        </div>

        <div class="em-banner-info">
          <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 10px">
            <n-tag :type="library.CollectionType === 'movies' ? 'info' : 'success'" size="small" round :bordered="false">
              {{ typeLabels[library.CollectionType] || library.CollectionType }}
            </n-tag>
            <span class="em-item-count">{{ library.ItemCount || 0 }} 个项目</span>
          </div>
          <div class="em-fields">
            <div>
              <label class="em-label">媒体库名称</label>
              <n-input v-model:value="name" size="small" />
            </div>
            <div>
              <label class="em-label">内容类型</label>
              <n-select v-model:value="collectionType" :options="typeOptions" size="small" :menu-props="solidModalMenuProps" />
            </div>
          </div>
          <div style="margin-top: 12px">
            <n-button type="primary" size="small" :loading="saving" @click="handleSave">保存修改</n-button>
          </div>
        </div>
      </div>

      <!-- Folders -->
      <div class="em-section">
        <h4 class="em-section-title">
          <n-icon :size="15"><FolderOutline /></n-icon>
          媒体文件夹
        </h4>
        <div class="em-folder-list">
          <div v-for="(p, i) in library.Locations || []" :key="i" class="em-folder-item">
            <n-icon :size="15" style="color: var(--app-text-muted); flex-shrink: 0"><FolderOutline /></n-icon>
            <span class="em-folder-path">{{ p }}</span>
            <n-button text type="error" size="tiny" @click="handleRemovePath(p)" title="移除">×</n-button>
          </div>
        </div>
        <div class="em-add-path">
          <n-input v-model:value="newPath" placeholder="/mnt/media/movies" size="small" @keydown.enter.prevent="handleAddPath" />
          <n-button secondary size="small" :disabled="addingPath || !newPath.trim()" :loading="addingPath" @click="handleAddPath">添加</n-button>
          <n-button secondary size="small" @click="openBrowser">浏览</n-button>
        </div>
      </div>

      <!-- Scan -->
      <div class="em-section">
        <h4 class="em-section-title">
          <n-icon :size="15"><RefreshOutline /></n-icon>
          扫描
        </h4>
        <p class="em-section-desc">扫描此媒体库中所有文件夹的媒体文件。</p>
        <n-button type="primary" size="small" :loading="scanning" @click="handleScan">立即扫描</n-button>
      </div>

      <!-- Danger -->
      <div class="em-section em-danger">
        <h4 class="em-section-title" style="color: var(--app-error)">
          <n-icon :size="15"><TrashOutline /></n-icon>
          危险操作
        </h4>
        <p class="em-section-desc">删除媒体库将移除所有关联的媒体信息（不会删除实际文件）。</p>
        <n-button type="error" ghost size="small" @click="showDeleteConfirm = true">删除此媒体库</n-button>
      </div>
    </template>

    <!-- Dir Browser Sub-Modal -->
    <n-modal v-model:show="showBrowser" preset="card" title="选择文件夹" style="max-width: 480px; max-height: 70vh">
      <div class="em-dir-current">{{ browserPath }}</div>
      <n-scrollbar style="max-height: min(350px, 45vh)">
        <div v-if="browserPath !== '/'" class="em-dir-row" @click="loadBrowserDir(parentDir())">← 上一级</div>
        <div v-if="browserLoading" style="padding: 20px; text-align: center; color: var(--app-text-muted)"><n-spin size="small" /></div>
        <div v-else-if="browserDirs.length === 0" style="padding: 20px; text-align: center; color: var(--app-text-muted)">没有子目录</div>
        <div v-else v-for="d in browserDirs" :key="d.Path" class="em-dir-row" @click="loadBrowserDir(d.Path)">
          <n-icon :size="16"><FolderOutline /></n-icon> {{ d.Name }}
        </div>
      </n-scrollbar>
      <template #action>
        <n-space justify="end">
          <n-button @click="showBrowser = false">取消</n-button>
          <n-button type="primary" @click="selectBrowserPath">选择当前目录</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Delete Confirm Sub-Modal -->
    <n-modal v-model:show="showDeleteConfirm" preset="dialog" type="error" title="删除媒体库" positive-text="删除" negative-text="取消" @positive-click="handleDelete">
      <p style="color: var(--app-text-muted); font-size: 14px">
        确定要删除媒体库「<strong style="color: var(--app-text)">{{ library?.Name }}</strong>」吗？此操作不可撤销。
      </p>
    </n-modal>
  </n-modal>
</template>

<style scoped>
.em-banner {
  display: flex;
  gap: 18px;
  margin-bottom: 16px;
}

.em-cover-wrap {
  flex-shrink: 0;
  width: 200px;
}

.em-cover-ratio {
  position: relative;
  width: 100%;
  padding-bottom: 56.25%; /* 16:9 */
  border-radius: 8px;
  overflow: hidden;
  background: linear-gradient(135deg, #1a1a2e 0%, #1e293b 40%, #334155 100%);
}

.em-cover-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.em-cover-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.em-cover-emoji {
  font-size: 28px;
  opacity: 0.35;
}

.em-cover-actions {
  display: flex;
  gap: 5px;
  margin-top: 6px;
}
.em-cover-btn {
  display: inline-flex; align-items: center; gap: 3px;
  padding: 3px 8px; font-size: 11px; border-radius: 5px;
  background: rgba(128,128,128,0.1); border: 1px solid var(--app-border);
  color: var(--app-text-muted); cursor: pointer; transition: all 0.15s;
}
.em-cover-btn:hover { border-color: var(--app-primary); color: var(--app-primary); }
.em-cover-btn-del:hover { border-color: var(--app-error); color: var(--app-error); }

.em-banner-info {
  flex: 1;
  min-width: 0;
}
.em-item-count { font-size: 13px; color: var(--app-text-muted); }

.em-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.em-label {
  display: block; font-size: 11px; color: var(--app-text-muted);
  margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500;
}

.em-section {
  background: rgba(128,128,128,0.04);
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 16px 18px;
  margin-bottom: 12px;
}
.em-danger { border-color: rgba(239,68,68,0.2); }

.em-section-title {
  font-size: 13px; font-weight: 600; color: var(--app-text);
  margin: 0 0 10px; padding-bottom: 8px;
  border-bottom: 1px solid var(--app-border);
  display: flex; align-items: center; gap: 6px;
}

.em-section-desc {
  font-size: 12px; color: var(--app-text-muted); margin: 0 0 10px;
}

.em-folder-list { margin-bottom: 10px; }
.em-folder-item {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: rgba(128,128,128,0.04);
  border-radius: 6px;
  margin-bottom: 4px;
}
.em-folder-path {
  flex: 1; font-size: 12px; color: var(--app-text);
  word-break: break-all; font-family: 'SF Mono', 'Fira Code', monospace;
}

.em-add-path { display: flex; gap: 6px; align-items: stretch; }

.em-dir-current {
  background: rgba(128,128,128,0.06); padding: 8px 12px;
  border-radius: 6px; margin-bottom: 10px;
  font-size: 12px; color: var(--app-primary);
  word-break: break-all; font-family: monospace;
}
.em-dir-row {
  display: flex; align-items: center; gap: 8px; padding: 6px 10px;
  cursor: pointer; border-radius: 4px; font-size: 13px; color: var(--app-text);
  transition: background 0.15s;
}
.em-dir-row:hover { background: rgba(128,128,128,0.08); }

@media (max-width: 500px) {
  .em-banner { flex-direction: column; }
  .em-cover-wrap { width: 100%; }
  .em-fields { grid-template-columns: 1fr !important; }
}
</style>
