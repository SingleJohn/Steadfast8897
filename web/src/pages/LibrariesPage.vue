<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton, NInput, NSelect, NSwitch, NModal, NSpace, NIcon, NSpin, NScrollbar, NTabs, NTabPane,
} from 'naive-ui'
import { FolderOutline } from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import LibraryCard from '@/components/LibraryCard.vue'
import LibraryEditModal from '@/components/LibraryEditModal.vue'
import { AppIcons } from '@/icons/appIcons'
import {
  getLibraries, addLibrary, refreshLibrary,
  getSystemConfig, updateSystemConfig, getScanProgress, browseDirectories,
} from '@/api/client'

const { showToast } = useToast()

const scanThreadsOptions = [1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20].map((n) => ({ label: String(n), value: String(n) }))
const libTypeOptions = [
  { label: '电影', value: 'movies' },
  { label: '电视剧', value: 'tvshows' },
]

const libraries = ref<any[]>([])
const scanProgress = ref<any[]>([])
const scanThreads = ref('3')
const fileWatcherEnabled = ref(true)
const scanning = ref(false)
const savingConfig = ref(false)
const activeView = ref<'libraries' | 'scan'>('libraries')

const showAddLib = ref(false)
const newLibName = ref('')
const newLibType = ref('movies')
const newLibPaths = ref<string[]>([])
const newLibPathInput = ref('')
const solidModalMenuProps = { class: 'solid-modal-menu' }

const showDirBrowser = ref(false)
const dirBrowserPath = ref('/mnt')
const dirBrowserDirs = ref<{ Name: string; Path: string }[]>([])
const dirBrowserLoading = ref(false)

const editLibraryId = ref<string | null>(null)

function openEditModal(libId: string) {
  editLibraryId.value = libId
}

function closeEditModal() {
  editLibraryId.value = null
}

async function onLibraryUpdated() {
  libraries.value = await getLibraries()
}

async function onLibraryDeleted() {
  editLibraryId.value = null
  libraries.value = await getLibraries()
}

function scanProgForLib(libId: string) {
  return scanProgress.value.find((s: any) => s.LibraryId === libId)
}

function libNameForScan(libId: string) {
  return libraries.value.find((l) => l.ItemId === libId)?.Name || libId
}

async function handleScan() {
  scanning.value = true
  try {
    await refreshLibrary()
    showToast('媒体库扫描已开始，这可能需要一些时间。', 'success')
  } catch {
    showToast('启动扫描失败', 'error')
  }
  setTimeout(() => { scanning.value = false }, 3000)
}

async function saveLibrarySettings() {
  savingConfig.value = true
  try {
    await updateSystemConfig({ scan_threads: scanThreads.value, file_watcher_enabled: String(fileWatcherEnabled.value) })
    showToast('媒体库设置已保存', 'success')
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    savingConfig.value = false
  }
}

async function loadDirBrowser(path: string) {
  dirBrowserLoading.value = true
  try {
    const res = await browseDirectories(path)
    dirBrowserPath.value = res.Path
    dirBrowserDirs.value = res.Directories || []
  } catch {
    showToast('无法读取目录', 'error')
  } finally {
    dirBrowserLoading.value = false
  }
}

function openDirBrowser() { showDirBrowser.value = true; void loadDirBrowser('/mnt') }

function dirParentPath() {
  const p = dirBrowserPath.value
  if (p === '/') return
  void loadDirBrowser(p.substring(0, p.lastIndexOf('/')) || '/')
}

function addPathToList(path: string) {
  const p = path.trim()
  if (p && !newLibPaths.value.includes(p)) newLibPaths.value = [...newLibPaths.value, p]
}

function removePathFromList(index: number) {
  newLibPaths.value = newLibPaths.value.filter((_, i) => i !== index)
}

function handleAddPathManual() {
  addPathToList(newLibPathInput.value)
  newLibPathInput.value = ''
}

async function handleAddLibrary(e?: Event) {
  e?.preventDefault?.()
  if (!newLibName.value || newLibPaths.value.length === 0) return
  try {
    await addLibrary(newLibName.value, newLibType.value, newLibPaths.value)
    showAddLib.value = false
    newLibName.value = ''
    newLibPaths.value = []
    newLibPathInput.value = ''
    showToast('媒体库添加成功', 'success')
    libraries.value = await getLibraries()
  } catch {
    showToast('添加媒体库失败', 'error')
  }
}

const timers: ReturnType<typeof setInterval>[] = []

onMounted(() => {
  getLibraries().then((l) => (libraries.value = l)).catch(() => {})
  getScanProgress().then((r: any) => (scanProgress.value = r.Items || [])).catch(() => {})
  getSystemConfig().then((cfg: any) => {
    scanThreads.value = cfg.scan_threads || '3'
    fileWatcherEnabled.value = cfg.file_watcher_enabled !== 'false'
  }).catch(() => {})
  timers.push(setInterval(() => {
    getScanProgress().then((r: any) => (scanProgress.value = r.Items || [])).catch(() => {})
  }, 3000))
})

onUnmounted(() => timers.forEach((t) => clearInterval(t)))
</script>

<template>
  <page-shell title="媒体库" :icon="AppIcons.library" description="管理媒体库文件夹与扫描设置">
    <n-tabs
      :value="activeView"
      type="segment"
      size="large"
      class="libraries-tabs"
      @update:value="(value) => activeView = value as 'libraries' | 'scan'"
    >
      <n-tab-pane name="libraries" tab="媒体库">
        <n-space justify="center" class="libraries-actions">
          <n-button secondary @click="showAddLib = true">+ 添加媒体库</n-button>
          <n-button type="primary" @click="handleScan" :disabled="scanning || scanProgress.some((s: any) => s.Status === 'scanning')" :loading="scanning">
            {{ scanProgress.some((s: any) => s.Status === 'scanning') ? '扫描中...' : '扫描所有媒体库' }}
          </n-button>
        </n-space>

        <div v-if="libraries.length === 0" class="lib-empty-card">
          <div class="lib-empty">尚未配置媒体库。点击"添加媒体库"开始使用。</div>
        </div>
        <div v-else class="lib-grid">
          <LibraryCard
            v-for="lib in libraries"
            :key="lib.ItemId"
            :lib="lib"
            :scan-prog="scanProgForLib(lib.ItemId)"
            @click="openEditModal"
          />
        </div>
      </n-tab-pane>

      <n-tab-pane name="scan" tab="扫描设置">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">扫描设置</h3>
              <div class="settings-card-desc">将全局扫描相关配置单独收纳，操作方式更接近 Emby 的标签切换。</div>
            </div>
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">并发扫描线程数</div>
              <div class="setting-desc">同时处理的媒体文件数量，值越大扫描越快但占用资源越多</div>
            </div>
            <n-select v-model:value="scanThreads" :options="scanThreadsOptions" style="width: 100px" />
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">文件监听</div>
              <div class="setting-desc">实时监听媒体库目录变动，文件新增/删除后自动入库</div>
            </div>
            <n-switch v-model:value="fileWatcherEnabled" />
          </div>
          <div class="settings-actions">
            <n-button type="primary" @click="saveLibrarySettings" :loading="savingConfig">保存设置</n-button>
          </div>
        </div>
      </n-tab-pane>
    </n-tabs>

    <!-- Add Library Modal -->
    <n-modal v-model:show="showAddLib" preset="card" title="添加媒体库" style="width: 500px; max-width: 90vw" class="solid-modal-card">
      <form @submit.prevent="handleAddLibrary">
        <div class="form-group">
          <label class="form-label">名称</label>
          <n-input v-model:value="newLibName" placeholder="例如：电影" />
        </div>
        <div class="form-group">
          <label class="form-label">类型</label>
          <n-select v-model:value="newLibType" :options="libTypeOptions" :menu-props="solidModalMenuProps" />
        </div>
        <div>
          <label class="form-label">路径</label>
          <div v-if="newLibPaths.length > 0" style="margin-bottom: 8px">
            <div v-for="(p, i) in newLibPaths" :key="i" class="path-chip">
              <span style="flex: 1; word-break: break-all">{{ p }}</span>
              <n-button text type="error" size="tiny" @click="removePathFromList(i)">&times;</n-button>
            </div>
          </div>
          <div style="display: flex; gap: 6px; align-items: stretch">
            <n-input v-model:value="newLibPathInput" placeholder="输入路径，如 /mnt/media/movies" style="flex: 1" @keydown.enter.prevent="handleAddPathManual" />
            <n-button secondary @click="handleAddPathManual">添加</n-button>
            <n-button secondary @click="openDirBrowser">
              <template #icon><n-icon><FolderOutline /></n-icon></template>
              浏览
            </n-button>
          </div>
        </div>
      </form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddLib = false">取消</n-button>
          <n-button type="primary" @click="handleAddLibrary">添加</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Dir Browser Modal -->
    <n-modal v-model:show="showDirBrowser" preset="card" title="选择文件夹" style="width: 500px; max-width: 90vw" class="dir-browser-modal">
      <div class="dir-current">{{ dirBrowserPath }}</div>
      <n-scrollbar style="max-height: min(400px, 50vh)">
        <div v-if="dirBrowserPath !== '/'" @click="dirParentPath" class="dir-row" style="color: var(--app-text-muted)">&#8592; 上一级</div>
        <div v-if="dirBrowserLoading" style="padding: 20px; text-align: center; color: var(--app-text-muted)"><n-spin size="small" /> 加载中...</div>
        <div v-else-if="dirBrowserDirs.length === 0" style="padding: 20px; text-align: center; color: var(--app-text-muted)">没有子目录</div>
        <div v-else v-for="d in dirBrowserDirs" :key="d.Path" @click="loadDirBrowser(d.Path)" class="dir-row">
          <n-icon size="16"><FolderOutline /></n-icon>
          {{ d.Name }}
        </div>
      </n-scrollbar>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDirBrowser = false">取消</n-button>
          <n-button type="primary" @click="addPathToList(dirBrowserPath); showDirBrowser = false">选择当前目录</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Library Edit Modal -->
    <LibraryEditModal
      :library-id="editLibraryId"
      @close="closeEditModal"
      @updated="onLibraryUpdated"
      @deleted="onLibraryDeleted"
    />
  </page-shell>
</template>

<style scoped>
.libraries-tabs {
  margin-top: 4px;
}

.libraries-tabs :deep(.n-tabs-nav) {
  justify-content: center;
}

.libraries-tabs :deep(.n-tab-pane) {
  padding-top: 20px;
}

.libraries-actions {
  margin-bottom: 18px;
}

.lib-grid {
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(2, 1fr);
}
@media (min-width: 600px)  { .lib-grid { grid-template-columns: repeat(3, 1fr); } }
@media (min-width: 960px)  { .lib-grid { grid-template-columns: repeat(4, 1fr); } }
@media (min-width: 1400px) { .lib-grid { grid-template-columns: repeat(5, 1fr); } }

.lib-empty-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 8px 0;
}
.lib-empty { padding: 20px 24px; color: var(--app-text-muted); font-size: 14px; }

.settings-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 20px 24px;
}
.settings-card-header { margin-bottom: 10px; }
.settings-card-title { font-size: 15px; font-weight: 600; color: var(--app-text); margin-bottom: 6px; }
.settings-card-desc { font-size: 13px; color: var(--app-text-muted); }
.setting-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 0; }
.setting-row + .setting-row { border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.setting-label { font-size: 14px; color: var(--app-text); }
.setting-desc { font-size: 12px; color: var(--app-text-muted); margin-top: 2px; }
.settings-actions { margin-top: 16px; }

.form-group { margin-bottom: 20px; }
.form-label { display: block; font-size: 12px; color: var(--app-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500; }
.path-chip { display: flex; align-items: center; gap: 8px; padding: 6px 10px; background: var(--app-surface-1, rgba(255,255,255,0.04)); border-radius: 4px; margin-bottom: 4px; font-size: 13px; color: var(--app-text); }

.dir-current { background: var(--app-surface-1, rgba(0,0,0,0.3)); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; color: var(--app-primary); word-break: break-all; font-family: monospace; }
.dir-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 14px; color: var(--app-text); }
.dir-row:hover { background: var(--app-surface-2, #2a2a2a); }

@media (max-width: 640px) {
  .libraries-actions {
    justify-content: stretch !important;
  }

  .setting-row {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
}
</style>
