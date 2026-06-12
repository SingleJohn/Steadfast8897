<script setup lang="ts">
import { FolderOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NInput, NModal, NScrollbar, NSelect, NSpace, NSpin } from 'naive-ui'

defineProps<{
  showAddLib: boolean
  newLibName: string
  newLibType: string
  libTypeOptions: { label: string; value: string }[]
  newLibPaths: string[]
  newLibPathInput: string
  showDirBrowser: boolean
  dirBrowserPath: string
  dirBrowserDirs: { Name: string; Path: string }[]
  dirBrowserLoading: boolean
  solidModalMenuProps: Record<string, any>
  forceSolidModalStyle: Record<string, string>
}>()

const emit = defineEmits<{
  updateShowAddLib: [value: boolean]
  updateNewLibName: [value: string]
  updateNewLibType: [value: string]
  updateNewLibPathInput: [value: string]
  removePath: [index: number]
  addPathManual: []
  submit: [event?: Event]
  openDirBrowser: []
  updateShowDirBrowser: [value: boolean]
  dirParentPath: []
  loadDirBrowser: [path: string]
  selectDir: []
}>()
</script>

<template>
  <n-modal
    :show="showAddLib"
    preset="card"
    title="添加媒体库"
    :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]"
    class="solid-modal-card force-solid-modal"
    @update:show="emit('updateShowAddLib', $event)"
  >
    <form @submit.prevent="emit('submit', $event)">
      <div class="form-group">
        <label class="form-label">名称</label>
        <n-input :value="newLibName" placeholder="例如：电影" @update:value="emit('updateNewLibName', $event)" />
      </div>
      <div class="form-group">
        <label class="form-label">类型</label>
        <n-select :value="newLibType" :options="libTypeOptions" :menu-props="solidModalMenuProps" @update:value="emit('updateNewLibType', $event)" />
      </div>
      <div>
        <label class="form-label">路径</label>
        <div v-if="newLibPaths.length > 0" class="path-list">
          <div v-for="(p, i) in newLibPaths" :key="i" class="path-chip">
            <span class="path-text">{{ p }}</span>
            <n-button text type="error" size="tiny" @click="emit('removePath', i)">×</n-button>
          </div>
        </div>
        <div class="path-input-row">
          <n-input :value="newLibPathInput" placeholder="输入路径，如 /mnt/media/movies" @update:value="emit('updateNewLibPathInput', $event)" @keydown.enter.prevent="emit('addPathManual')" />
          <n-button secondary @click="emit('addPathManual')">添加</n-button>
          <n-button secondary @click="emit('openDirBrowser')">
            <template #icon><n-icon><FolderOutline /></n-icon></template>
            浏览
          </n-button>
        </div>
      </div>
    </form>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShowAddLib', false)">取消</n-button>
        <n-button type="primary" @click="emit('submit')">添加</n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal
    :show="showDirBrowser"
    preset="card"
    title="选择文件夹"
    :style="[forceSolidModalStyle, { width: '500px', maxWidth: '90vw' }]"
    class="dir-browser-modal solid-modal-card force-solid-modal"
    @update:show="emit('updateShowDirBrowser', $event)"
  >
    <div class="dir-current">{{ dirBrowserPath }}</div>
    <n-scrollbar class="dir-scroll">
      <div v-if="dirBrowserPath !== '/'" class="dir-row dir-parent" @click="emit('dirParentPath')">← 上一级</div>
      <div v-if="dirBrowserLoading" class="dir-state"><n-spin size="small" /> 加载中...</div>
      <div v-else-if="dirBrowserDirs.length === 0" class="dir-state">没有子目录</div>
      <div v-else v-for="d in dirBrowserDirs" :key="d.Path" class="dir-row" @click="emit('loadDirBrowser', d.Path)">
        <n-icon size="16"><FolderOutline /></n-icon>
        {{ d.Name }}
      </div>
    </n-scrollbar>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShowDirBrowser', false)">取消</n-button>
        <n-button type="primary" @click="emit('selectDir')">选择当前目录</n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.form-group { margin-bottom: 20px; }
.form-label { display: block; font-size: 12px; color: var(--app-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500; }
.path-list { margin-bottom: 8px; }
.path-chip { display: flex; align-items: center; gap: 8px; padding: 6px 10px; background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(255,255,255,0.04))); border-radius: 4px; margin-bottom: 4px; font-size: 13px; color: var(--app-text); }
.path-text { flex: 1; word-break: break-all; }
.path-input-row { display: flex; gap: 6px; align-items: stretch; }
.path-input-row .n-input { flex: 1; }
.dir-current { background: var(--app-modal-panel-bg-soft, var(--app-surface-1, rgba(0,0,0,0.3))); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; color: var(--app-primary); word-break: break-all; font-family: monospace; }
.dir-scroll { max-height: min(400px, 50vh); }
.dir-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 14px; color: var(--app-text); }
.dir-row:hover { background: var(--app-modal-hover-bg, var(--app-surface-2, #2a2a2a)); }
.dir-parent { color: var(--app-text-muted); }
.dir-state { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 20px; text-align: center; color: var(--app-text-muted); }

@media (max-width: 640px) {
  .path-input-row {
    flex-wrap: wrap;
  }
}
</style>
