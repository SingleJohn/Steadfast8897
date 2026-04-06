<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useToast } from '@/composables/useToast'
import {
  NButton, NInput, NSelect, NSwitch, NModal, NSpace, NIcon, NSpin, NScrollbar, NTabs, NTabPane, NProgress,
} from 'naive-ui'
import { FolderOutline } from '@vicons/ionicons5'
import PageShell from '@/components/PageShell.vue'
import LibraryCard from '@/components/LibraryCard.vue'
import LibraryEditModal from '@/components/LibraryEditModal.vue'
import { AppIcons } from '@/icons/appIcons'
import { getPlatformIcon } from '@/icons/PlatformIcons'
import {
  getLibraries, addLibrary, refreshLibrary,
  getSystemConfig, updateSystemConfig, getScanProgress, browseDirectories,
  getPlatforms, addPlatformLibrary, setPlatformEnable, deletePlatformLibrary, scanPlatformStudios, scanPlatformByFilename, rescrapeMissingStudio, getTaskSummary, updateLibrarySortOrder,
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
const activeView = ref<'libraries' | 'scan' | 'platforms'>('libraries')

// Platform libraries
const platformsData = ref<{ GlobalEnabled: boolean; Platforms: any[] }>({ GlobalEnabled: false, Platforms: [] })
const newPlatformName = ref('')
const platformScanning = ref(false)
const platformTask = ref<any>(null)
const platformPosition = ref<'before' | 'after'>('after')
const showLibraryItemCount = ref(true)

async function loadPlatforms() {
  try { platformsData.value = await getPlatforms() } catch {}
}

async function loadTaskSummary() {
  try {
    const summary = await getTaskSummary()
    platformTask.value = summary.platform
    rescrapeStatus.value = summary.platform?.rescrape || null
  } catch {}
}

async function toggleGlobalPlatform(enabled: boolean) {
  try {
    await updateSystemConfig({ platform_libraries_enabled: String(enabled) })
    platformsData.value.GlobalEnabled = enabled
    showToast(enabled ? '平台库已启用' : '平台库已禁用', 'success')
  } catch { showToast('操作失败', 'error') }
}

async function togglePlatform(name: string, enabled: boolean) {
  try {
    await setPlatformEnable(name, enabled)
    await loadPlatforms()
  } catch { showToast('操作失败', 'error') }
}

async function handleAddPlatform() {
  const name = newPlatformName.value.trim()
  if (!name) return
  try {
    await addPlatformLibrary(name)
    newPlatformName.value = ''
    await loadPlatforms()
    showToast('平台已添加', 'success')
  } catch { showToast('添加失败', 'error') }
}

async function handleDeletePlatform(id: string) {
  try {
    await deletePlatformLibrary(id)
    await loadPlatforms()
    showToast('平台已删除', 'success')
  } catch { showToast('删除失败', 'error') }
}

async function handleScanStudios() {
  platformScanning.value = true
  try {
    const res = await scanPlatformStudios()
    await loadTaskSummary()
    showToast(`正在从 TMDB 扫描 ${res.total} 个项目`, 'success')
  } catch { showToast('扫描失败', 'error') }
  setTimeout(() => { platformScanning.value = false; loadPlatforms(); void loadTaskSummary() }, 5000)
}

const filenameScanning = ref(false)
async function handleScanFilename() {
  filenameScanning.value = true
  try {
    const res = await scanPlatformByFilename()
    showToast(`从文件名识别完成，更新了 ${res.updated} 个项目`, 'success')
    await loadPlatforms()
    await loadTaskSummary()
  } catch { showToast('扫描失败', 'error') }
  filenameScanning.value = false
}

const rescraping = ref(false)
const rescrapeStatus = ref<any>(null)
let rescrapeTimer: ReturnType<typeof setInterval> | null = null

async function pollRescrapeProgress() {
  try {
    await loadTaskSummary()
    const p = rescrapeStatus.value
    if (!p.running && rescrapeTimer) {
      clearInterval(rescrapeTimer)
      rescrapeTimer = null
      rescraping.value = false
      loadPlatforms()
    }
  } catch {}
}

async function handleRescrape() {
  rescraping.value = true
  rescrapeStatus.value = null
  try {
    const res = await rescrapeMissingStudio()
    showToast(`正在重新刮削 ${res.total} 个项目`, 'success')
    if (rescrapeTimer) clearInterval(rescrapeTimer)
    rescrapeTimer = setInterval(pollRescrapeProgress, 2000)
  } catch { showToast('刮削失败', 'error'); rescraping.value = false }
}

const showAddLib = ref(false)
const newLibName = ref('')
const newLibType = ref('movies')
const newLibPaths = ref<string[]>([])
const newLibPathInput = ref('')
const solidModalMenuProps = { class: 'solid-modal-menu' }
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

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

async function moveLibrary(index: number, direction: 'up' | 'down') {
  const swapIdx = direction === 'up' ? index - 1 : index + 1
  if (swapIdx < 0 || swapIdx >= libraries.value.length) return
  const arr = [...libraries.value]
  ;[arr[index], arr[swapIdx]] = [arr[swapIdx], arr[index]]
  libraries.value = arr
  const orders = arr.map((lib: any, i: number) => ({ Id: lib.ItemId, SortOrder: i }))
  try {
    await updateLibrarySortOrder(orders)
  } catch { showToast('排序失败', 'error') }
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
    await updateSystemConfig({
      scan_threads: scanThreads.value,
      file_watcher_enabled: String(fileWatcherEnabled.value),
      platform_libraries_position: platformPosition.value,
      library_show_item_count: String(showLibraryItemCount.value),
    })
    showToast('设置已保存', 'success')
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
    platformPosition.value = cfg.platform_libraries_position === 'before' ? 'before' : 'after'
    showLibraryItemCount.value = cfg.library_show_item_count !== 'false'
  }).catch(() => {})
  loadPlatforms()
  loadTaskSummary().then(() => {
    if (rescrapeStatus.value?.running) {
      rescraping.value = true
      rescrapeTimer = setInterval(pollRescrapeProgress, 2000)
    }
  }).catch(() => {})
  timers.push(setInterval(() => {
    getScanProgress().then((r: any) => (scanProgress.value = r.Items || [])).catch(() => {})
    void loadTaskSummary()
  }, 3000))
})

onUnmounted(() => {
  timers.forEach((t) => clearInterval(t))
  if (rescrapeTimer) clearInterval(rescrapeTimer)
})
</script>

<template>
  <page-shell title="媒体库" :icon="AppIcons.library" description="管理媒体库文件夹与扫描设置">
    <n-tabs
      :value="activeView"
      type="segment"
      size="large"
      class="libraries-tabs"
      @update:value="(value) => activeView = value as 'libraries' | 'scan' | 'platforms'"
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
          <div v-for="(lib, idx) in libraries" :key="lib.ItemId" class="lib-card-wrapper">
            <LibraryCard
              :lib="lib"
              :scan-prog="scanProgForLib(lib.ItemId)"
              @click="openEditModal"
            />
            <div class="lib-sort-btns">
              <n-button text size="tiny" :disabled="idx === 0" @click.stop="moveLibrary(idx, 'up')">&#9650;</n-button>
              <n-button text size="tiny" :disabled="idx === libraries.length - 1" @click.stop="moveLibrary(idx, 'down')">&#9660;</n-button>
            </div>
          </div>
        </div>
      </n-tab-pane>

      <n-tab-pane name="platforms" tab="平台库">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">平台媒体库</h3>
              <div class="settings-card-desc">根据 TMDB 的出品平台信息（Netflix、HBO 等）自动生成虚拟媒体库，在播放器中可见。</div>
            </div>
          </div>

          <div class="setting-row">
            <div>
              <div class="setting-label">启用平台库</div>
              <div class="setting-desc">开启后，已启用的平台将作为虚拟媒体库显示在播放器中</div>
            </div>
            <n-switch :value="platformsData.GlobalEnabled" @update:value="toggleGlobalPlatform" />
          </div>

          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">从 TMDB 获取平台</div>
              <div class="setting-desc">通过 TMDB API 获取 networks/出品公司（需已刮削，速度较慢）</div>
              <div v-if="platformTask" class="setting-desc" style="margin-top: 6px">
                当前待平台识别 {{ platformTask.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，其中可直接扫描 {{ platformTask.pending_tmdb_ready_total || 0 }} 项
              </div>
            </div>
            <n-button secondary @click="handleScanStudios" :loading="platformScanning" :disabled="platformScanning || ((platformTask?.pending_tmdb_ready_total || 0) === 0 && !platformTask?.scan_running)">
              {{ platformScanning ? '扫描中...' : 'TMDB 扫描' }}
            </n-button>
          </div>
          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">从文件名识别平台</div>
              <div class="setting-desc">分析文件名中的平台标识（NF/DSNP/ATVP/AMZN/HMAX 等），速度快覆盖广</div>
            </div>
            <n-button secondary @click="handleScanFilename" :loading="filenameScanning" :disabled="filenameScanning">
              {{ filenameScanning ? '扫描中...' : '文件名扫描' }}
            </n-button>
          </div>
          <div class="setting-row" style="flex-wrap: wrap; gap: 8px">
            <div style="flex: 1">
              <div class="setting-label">重新刮削无平台项目</div>
              <div class="setting-desc">对仍无平台信息的 Movie/Series 重新执行完整 TMDB 刮削（耗时较长）</div>
              <div v-if="platformTask && !rescrapeStatus?.running" class="setting-desc" style="margin-top: 6px">
                当前待重新刮削 {{ rescrapeStatus?.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，仍缺少 TMDB 的有 {{ platformTask.pending_metadata_total || 0 }} 项
              </div>
            </div>
            <n-button secondary type="warning" @click="handleRescrape" :loading="rescraping" :disabled="rescraping || (!!rescrapeStatus && !rescrapeStatus.running && (rescrapeStatus.pending_total || 0) === 0)">
              {{ rescraping ? '刮削中...' : '重新刮削' }}
            </n-button>
          </div>
          <div v-if="rescrapeStatus && rescrapeStatus.running" class="rescrape-progress">
            <n-progress type="line" :percentage="rescrapeStatus.percentage" :show-indicator="true" status="info" />
            <div class="rescrape-stats">
              已处理 {{ rescrapeStatus.processed }} / {{ rescrapeStatus.total }}
              <span style="color: #18a058; margin-left: 12px">成功 {{ rescrapeStatus.success }}</span>
              <span style="color: #f0a020; margin-left: 12px">未找到 {{ rescrapeStatus.not_found }}</span>
              <span style="color: #d03050; margin-left: 12px">请求失败 {{ rescrapeStatus.fetch_error }}</span>
            </div>
          </div>
          <div v-else-if="rescrapeStatus && !rescrapeStatus.running && rescrapeStatus.total > 0" class="rescrape-progress">
            <div class="rescrape-stats">
              刮削完成: 共 {{ rescrapeStatus.total }} 项
              <span style="color: #18a058; margin-left: 12px">成功 {{ rescrapeStatus.success }}</span>
              <span style="color: #f0a020; margin-left: 12px">TMDB未收录 {{ rescrapeStatus.not_found }}</span>
              <span style="color: #d03050; margin-left: 12px">网络错误 {{ rescrapeStatus.fetch_error }}</span>
            </div>
          </div>

          <div style="margin-top: 16px; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); padding-top: 16px">
            <div class="setting-label" style="margin-bottom: 12px">平台列表</div>
            <div v-for="p in platformsData.Platforms" :key="p.Id" class="platform-row">
              <img v-if="p.LogoUrl" :src="p.LogoUrl" class="platform-logo-icon" />
              <n-icon v-else size="28" style="margin-right: 10px; flex-shrink: 0"><component :is="getPlatformIcon(p.PlatformName)" /></n-icon>
              <div style="flex: 1">
                <span class="platform-name">{{ p.PlatformName }}</span>
                <span class="platform-count">{{ p.ItemCount }} 部</span>
              </div>
              <n-switch :value="p.Enabled" @update:value="(v: boolean) => togglePlatform(p.PlatformName, v)" size="small" />
              <n-button text type="error" size="tiny" @click="handleDeletePlatform(p.Id)" style="margin-left: 8px">&times;</n-button>
            </div>
          </div>

          <div style="margin-top: 16px; display: flex; gap: 8px">
            <n-input v-model:value="newPlatformName" placeholder="自定义平台名称" size="small" style="flex: 1" @keydown.enter.prevent="handleAddPlatform" />
            <n-button secondary size="small" @click="handleAddPlatform">添加</n-button>
          </div>
        </div>
      </n-tab-pane>

      <n-tab-pane name="scan" tab="高级设置">
        <div class="settings-card">
          <div class="settings-card-header">
            <div>
              <h3 class="settings-card-title">高级设置</h3>
              <div class="settings-card-desc">扫描参数与媒体库显示偏好。</div>
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
          <div class="setting-row">
            <div>
              <div class="setting-label">平台库排列位置</div>
              <div class="setting-desc">控制平台虚拟媒体库在播放器中的排列位置，位于普通媒体库前面或后面</div>
            </div>
            <n-select
              v-model:value="platformPosition"
              :options="[{ label: '媒体库前面', value: 'before' }, { label: '媒体库后面', value: 'after' }]"
              style="width: 140px"
            />
          </div>
          <div class="setting-row">
            <div>
              <div class="setting-label">显示媒体总数</div>
              <div class="setting-desc">在媒体库和平台库卡片右上角显示媒体总数角标</div>
            </div>
            <n-switch v-model:value="showLibraryItemCount" />
          </div>
          <div class="settings-actions">
            <n-button type="primary" @click="saveLibrarySettings" :loading="savingConfig">保存设置</n-button>
          </div>
        </div>
      </n-tab-pane>
    </n-tabs>

    <!-- Add Library Modal -->
    <n-modal v-model:show="showAddLib" preset="card" title="添加媒体库" :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]" class="solid-modal-card force-solid-modal">
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
    <n-modal v-model:show="showDirBrowser" preset="card" title="选择文件夹" :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]" class="dir-browser-modal solid-modal-card force-solid-modal">
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

.lib-card-wrapper { position: relative; }
.lib-sort-btns {
  position: absolute;
  top: 4px;
  left: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  z-index: 5;
  opacity: 0;
  transition: opacity 0.2s;
}
.lib-card-wrapper:hover .lib-sort-btns { opacity: 1; }
.lib-sort-btns .n-button {
  background: rgba(0,0,0,0.6);
  border-radius: 4px;
  padding: 2px 6px;
  color: #fff !important;
  font-size: 10px;
}

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
.path-chip { display: flex; align-items: center; gap: 8px; padding: 6px 10px; background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(255,255,255,0.04))); border-radius: 4px; margin-bottom: 4px; font-size: 13px; color: var(--app-text); }

.dir-current { background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(0,0,0,0.3))); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; color: var(--app-primary); word-break: break-all; font-family: monospace; }
.dir-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 14px; color: var(--app-text); }
.dir-row:hover { background: var(--app-modal-hover-bg, var(--app-surface-2, #2a2a2a)); }

.rescrape-progress { margin-top: 12px; padding: 12px 0; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.rescrape-stats { font-size: 13px; color: var(--app-text-muted); margin-top: 6px; }

.platform-logo-icon { width: 28px; height: 28px; border-radius: 6px; object-fit: cover; margin-right: 10px; flex-shrink: 0; }
.platform-row { display: flex; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.platform-row:last-child { border-bottom: none; }
.platform-name { font-size: 14px; color: var(--app-text); font-weight: 500; }
.platform-count { font-size: 12px; color: var(--app-text-muted); margin-left: 8px; }

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
