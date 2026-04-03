<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton, NInput, NSelect, NModal, NSpace, NIcon, NSpin, NTag, NProgress,
} from 'naive-ui'
import { ArrowBackOutline, FolderOutline, CloudUploadOutline, TrashOutline, RefreshOutline } from '@vicons/ionicons5'
import {
  getLibraryDetail, updateLibraryInfo, deleteLibraryById,
  addLibraryPath, removeLibraryPath, refreshSingleLibrary,
  uploadLibraryImage, deleteLibraryImage, browseDirectories,
} from '../api/client'
import { useToast } from '../composables/useToast'

const route = useRoute()
const router = useRouter()
const { showToast } = useToast()

const libraryId = computed(() => route.params.libraryId as string)
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

async function loadLibrary() {
  if (!libraryId.value) return
  loading.value = true
  try {
    const lib = await getLibraryDetail(libraryId.value)
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

onMounted(loadLibrary)
watch(libraryId, () => loadLibrary())

const coverUrl = computed(() => {
  const tag = imageTag.value || library.value?.ImageTag
  if (!tag) return ''
  return `/Items/${libraryId.value}/Images/Primary?tag=${tag}&v=${coverKey.value}`
})

async function handleSave() {
  if (!libraryId.value || !name.value.trim()) return
  saving.value = true
  try {
    await updateLibraryInfo(libraryId.value, { Name: name.value.trim(), CollectionType: collectionType.value })
    showToast('媒体库设置已保存', 'success')
    loadLibrary()
  } catch {
    showToast('保存失败', 'error')
  } finally {
    saving.value = false
  }
}

async function handleAddPath() {
  if (!libraryId.value || !newPath.value.trim()) return
  addingPath.value = true
  try {
    await addLibraryPath(libraryId.value, newPath.value.trim())
    newPath.value = ''
    showToast('文件夹已添加', 'success')
    loadLibrary()
  } catch {
    showToast('添加文件夹失败', 'error')
  } finally {
    addingPath.value = false
  }
}

async function handleRemovePath(pathToRemove: string) {
  if (!libraryId.value) return
  if (library.value?.Locations?.length <= 1) {
    showToast('至少需要保留一个文件夹', 'error')
    return
  }
  try {
    await removeLibraryPath(libraryId.value, pathToRemove)
    showToast('文件夹已移除', 'success')
    loadLibrary()
  } catch {
    showToast('移除文件夹失败', 'error')
  }
}

async function handleScan() {
  if (!libraryId.value) return
  scanning.value = true
  try {
    await refreshSingleLibrary(libraryId.value)
    showToast('媒体库扫描已开始', 'success')
  } catch {
    showToast('启动扫描失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function handleDelete() {
  if (!libraryId.value) return
  try {
    await deleteLibraryById(libraryId.value)
    router.push({ name: 'libraries' })
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
  if (!file || !libraryId.value) return
  uploadingImage.value = true
  try {
    const res = await uploadLibraryImage(libraryId.value, file)
    imageTag.value = res.ImageTag
    coverKey.value++
    showToast('封面已上传', 'success')
  } catch {
    showToast('上传失败', 'error')
  } finally {
    uploadingImage.value = false
    input.value = ''
  }
}

async function onDeleteCover() {
  if (!libraryId.value) return
  try {
    await deleteLibraryImage(libraryId.value)
    imageTag.value = null
    if (library.value) library.value.ImageTag = null
    showToast('封面已删除', 'success')
  } catch {
    showToast('删除封面失败', 'error')
  }
}
</script>

<template>
  <div v-if="loading || !library" style="padding: 60px; text-align: center"><n-spin size="medium" /></div>
  <div v-else style="max-width: 720px; margin: 0 auto">
    <!-- Header -->
    <div style="margin-bottom: 16px">
      <n-button text size="small" @click="router.push({ name: 'libraries' })">
        <template #icon><n-icon :size="18"><ArrowBackOutline /></n-icon></template>
        媒体库
      </n-button>
    </div>

    <div class="lib-banner">
      <div class="lib-cover-wrapper">
        <div v-if="coverUrl" class="lib-cover">
          <img :src="coverUrl" alt="cover" />
        </div>
        <div v-else class="lib-cover lib-cover-empty">
          <span>{{ library.CollectionType === 'movies' ? '🎬' : '📺' }}</span>
        </div>
        <div class="cover-actions">
          <label class="cover-btn">
            <n-icon :size="14"><CloudUploadOutline /></n-icon>
            {{ uploadingImage ? '...' : '上传' }}
            <input type="file" accept="image/*" style="display: none" :disabled="uploadingImage" @change="onCoverChange" />
          </label>
          <button v-if="coverUrl" class="cover-btn cover-btn-del" @click="onDeleteCover">
            <n-icon :size="14"><TrashOutline /></n-icon>
            删除
          </button>
        </div>
      </div>
      <div class="lib-banner-info">
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 8px">
          <n-tag :type="library.CollectionType === 'movies' ? 'info' : 'success'" size="small" round :bordered="false">
            {{ typeLabels[library.CollectionType] || library.CollectionType }}
          </n-tag>
          <span class="item-count">{{ library.ItemCount || 0 }} 个项目</span>
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 12px">
          <div>
            <label class="form-label">媒体库名称</label>
            <n-input v-model:value="name" />
          </div>
          <div>
            <label class="form-label">内容类型</label>
            <n-select v-model:value="collectionType" :options="typeOptions" />
          </div>
        </div>
        <div style="margin-top: 14px">
          <n-button type="primary" size="small" :loading="saving" @click="handleSave">保存修改</n-button>
        </div>
      </div>
    </div>

    <!-- Folders -->
    <div class="section-card">
      <h3 class="section-title">
        <n-icon :size="16"><FolderOutline /></n-icon>
        媒体文件夹
      </h3>
      <div class="folder-list">
        <div v-for="(p, i) in library.Locations || []" :key="i" class="folder-item">
          <n-icon :size="16" style="color: var(--app-text-muted); flex-shrink: 0"><FolderOutline /></n-icon>
          <span class="folder-path">{{ p }}</span>
          <n-button text type="error" size="tiny" @click="handleRemovePath(p)" title="移除">×</n-button>
        </div>
      </div>
      <div class="add-path-row">
        <n-input v-model:value="newPath" placeholder="输入路径，如 /mnt/media/movies" size="small" @keydown.enter.prevent="handleAddPath" />
        <n-button secondary size="small" :disabled="addingPath || !newPath.trim()" :loading="addingPath" @click="handleAddPath">添加</n-button>
        <n-button secondary size="small" @click="openBrowser">浏览</n-button>
      </div>
    </div>

    <!-- Dir Browser -->
    <n-modal v-model:show="showBrowser" preset="card" title="选择文件夹" style="max-width: 500px; max-height: 70vh" class="glass-modal">
      <div class="dir-current">{{ browserPath }}</div>
      <div style="overflow-y: auto; min-height: 200px; max-height: 40vh">
        <div v-if="browserPath !== '/'" class="dir-row" @click="loadBrowserDir(parentDir())">← 上一级</div>
        <div v-if="browserLoading" style="padding: 20px; text-align: center; color: var(--app-text-muted)"><n-spin size="small" /></div>
        <div v-else-if="browserDirs.length === 0" style="padding: 20px; text-align: center; color: var(--app-text-muted)">没有子目录</div>
        <div v-else v-for="d in browserDirs" :key="d.Path" class="dir-row" @click="loadBrowserDir(d.Path)">
          <n-icon :size="16"><FolderOutline /></n-icon> {{ d.Name }}
        </div>
      </div>
      <template #action>
        <n-space justify="end">
          <n-button @click="showBrowser = false">取消</n-button>
          <n-button type="primary" @click="selectBrowserPath">选择当前目录</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Scan -->
    <div class="section-card">
      <h3 class="section-title">
        <n-icon :size="16"><RefreshOutline /></n-icon>
        扫描
      </h3>
      <p class="section-desc">扫描此媒体库中所有文件夹的媒体文件，新增的媒体将自动添加到库中。</p>
      <n-button type="primary" size="small" :loading="scanning" @click="handleScan">立即扫描此媒体库</n-button>
    </div>

    <!-- Danger -->
    <div class="section-card danger-card">
      <h3 class="section-title" style="color: var(--app-error)">
        <n-icon :size="16"><TrashOutline /></n-icon>
        危险操作
      </h3>
      <p class="section-desc">删除媒体库将移除所有关联的媒体信息（不会删除实际文件）。</p>
      <n-button type="error" ghost size="small" @click="showDeleteConfirm = true">删除此媒体库</n-button>
    </div>

    <n-modal v-model:show="showDeleteConfirm" preset="dialog" type="error" title="删除媒体库" positive-text="删除" negative-text="取消" @positive-click="handleDelete">
      <p style="color: var(--app-text-muted); font-size: 14px">
        确定要删除媒体库「<strong style="color: var(--app-text)">{{ library.Name }}</strong>」吗？此操作不可撤销。
      </p>
    </n-modal>
  </div>
</template>

<style scoped>
.form-label {
  display: block; font-size: 12px; color: var(--app-text-muted);
  margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500;
}

.lib-banner {
  display: flex;
  gap: 24px;
  padding: 24px;
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  margin-bottom: 20px;
}

.lib-cover-wrapper {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.lib-cover {
  width: 120px; height: 180px; border-radius: 8px; overflow: hidden;
  background: rgba(128,128,128,0.1);
  display: flex; align-items: center; justify-content: center;
}
.lib-cover img {
  width: 100%; height: 100%; object-fit: cover;
}
.lib-cover-empty {
  border: 2px dashed var(--app-border);
  font-size: 36px;
}

.cover-actions {
  display: flex; gap: 6px;
}
.cover-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 10px; font-size: 12px; border-radius: 6px;
  background: rgba(128,128,128,0.1); border: 1px solid var(--app-border);
  color: var(--app-text-muted); cursor: pointer; transition: all 0.15s;
}
.cover-btn:hover { border-color: var(--app-primary); color: var(--app-primary); }
.cover-btn-del:hover { border-color: var(--app-error); color: var(--app-error); }

.lib-banner-info {
  flex: 1; min-width: 0;
}

.item-count {
  font-size: 13px; color: var(--app-text-muted);
}

.section-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  padding: 20px 24px;
  margin-bottom: 16px;
}

.danger-card {
  border-color: rgba(239,68,68,0.2);
}

.section-title {
  font-size: 14px; font-weight: 600; color: var(--app-text);
  margin: 0 0 12px; padding-bottom: 10px;
  border-bottom: 1px solid var(--app-border);
  display: flex; align-items: center; gap: 8px;
}

.section-desc {
  font-size: 13px; color: var(--app-text-muted); margin: 0 0 14px;
}

.folder-list {
  margin-bottom: 12px;
}

.folder-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px;
  background: rgba(128,128,128,0.04);
  border-radius: 8px;
  margin-bottom: 6px;
}

.folder-path {
  flex: 1; font-size: 13px; color: var(--app-text);
  word-break: break-all; font-family: 'SF Mono', 'Fira Code', monospace;
}

.add-path-row {
  display: flex; gap: 6px; align-items: stretch;
}

.dir-current {
  background: rgba(128,128,128,0.06); padding: 10px 14px;
  border-radius: 6px; margin-bottom: 12px;
  font-size: 13px; color: var(--app-primary);
  word-break: break-all; font-family: monospace;
}

.dir-row {
  display: flex; align-items: center; gap: 8px; padding: 8px 12px;
  cursor: pointer; border-radius: 4px; font-size: 14px; color: var(--app-text);
  transition: background 0.15s;
}
.dir-row:hover { background: rgba(128,128,128,0.08); }

@media (max-width: 600px) {
  .lib-banner { flex-direction: column; align-items: center; text-align: center; }
  .lib-banner-info > div { grid-template-columns: 1fr !important; }
}
</style>
