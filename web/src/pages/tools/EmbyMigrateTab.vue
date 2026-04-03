<script setup lang="ts">
import { ref, computed } from 'vue'
import { useToast } from '@/composables/useToast'
import { NCard, NButton, NCheckbox, NInputNumber, NIcon, NTag, NAlert } from 'naive-ui'
import { CloudUploadOutline, CheckmarkCircleOutline, AlertCircleOutline, SwapHorizontalOutline } from '@vicons/ionicons5'
import { embyMigrate } from '@/api/client'

const { showToast } = useToast()

const embyFile = ref<File | null>(null)
const embyMigrating = ref(false)
const embyResult = ref<any>(null)
const isDragOver = ref(false)
const enableRemoteAccess = ref(true)
const enableMediaPlayback = ref(true)
const enableAudioTranscoding = ref(true)
const enableVideoTranscoding = ref(true)
const enableContentDownloading = ref(true)
const enableAllFolders = ref(true)
const simultaneousStreamLimit = ref(0)
const remoteClientBitrateLimit = ref(0)
const embyFileInputRef = ref<HTMLInputElement | null>(null)

const fileSizeStr = computed(() => {
  if (!embyFile.value) return ''
  const kb = embyFile.value.size / 1024
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb.toFixed(0)} KB`
})

function onDragOver(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = true
}

function onDragLeave() {
  isDragOver.value = false
}

function onDrop(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = false
  const file = e.dataTransfer?.files?.[0]
  if (file && file.name.endsWith('.db')) {
    embyFile.value = file
  } else {
    showToast('请上传 .db 文件', 'warning')
  }
}

async function handleEmbyMigrate() {
  if (!embyFile.value) return
  embyMigrating.value = true
  embyResult.value = null
  try {
    const initSqlJs = (await import('sql.js')).default
    const SQL = await initSqlJs({
      locateFile: () => '/sql-wasm.wasm',
    })
    const buf = await embyFile.value.arrayBuffer()
    const db = new SQL.Database(new Uint8Array(buf))
    const rows = db.exec('SELECT data FROM LocalUsersv2')
    db.close()

    if (!rows.length || !rows[0].values.length) {
      showToast('未找到用户数据', 'error')
      embyMigrating.value = false
      return
    }

    const users: { name: string; password: string }[] = []
    for (const row of rows[0].values) {
      try {
        const raw = row[0]
        const txt = raw instanceof Uint8Array ? new TextDecoder().decode(raw) : String(raw)
        const obj = JSON.parse(txt)
        if (obj.Name) {
          users.push({ name: obj.Name, password: obj.Password || '' })
        }
      } catch {
        /* skip */
      }
    }

    if (!users.length) {
      showToast('未解析到有效用户', 'error')
      embyMigrating.value = false
      return
    }

    const policy = {
      enable_remote_access: enableRemoteAccess.value,
      enable_media_playback: enableMediaPlayback.value,
      enable_audio_transcoding: enableAudioTranscoding.value,
      enable_video_transcoding: enableVideoTranscoding.value,
      enable_content_downloading: enableContentDownloading.value,
      enable_all_folders: enableAllFolders.value,
      simultaneous_stream_limit: simultaneousStreamLimit.value,
      remote_client_bitrate_limit: remoteClientBitrateLimit.value,
    }
    const res = await embyMigrate(users, policy)
    embyResult.value = res
    showToast(`迁移完成：导入 ${res.imported} 个用户，跳过 ${res.skipped} 个`, 'success')
  } catch (e: any) {
    showToast(`迁移失败：${e.message}`, 'error')
  } finally {
    embyMigrating.value = false
  }
}

function onEmbyFileChange(e: Event) {
  const t = e.target as HTMLInputElement
  embyFile.value = t.files?.[0] || null
}

function triggerEmbyFileInput() {
  embyFileInputRef.value?.click()
}

function resetFile() {
  embyFile.value = null
  embyResult.value = null
  if (embyFileInputRef.value) embyFileInputRef.value.value = ''
}
</script>

<template>
  <div class="migrate-layout">
    <div class="migrate-top">
      <!-- Left: Upload -->
      <n-card class="glass-card migrate-card" :bordered="false">
        <template #header>
          <div class="card-header">
            <span class="step-badge">1</span>
            <span class="card-title">上传 users.db</span>
          </div>
        </template>

        <p class="card-desc">
          从 Emby 服务器的 <code>data</code> 目录中找到 <code>users.db</code> 文件上传。
          将导入所有用户名和密码，用户登录时自动兼容 Emby 密码。已存在的用户名会自动跳过。
        </p>

        <div
          class="drop-zone"
          :class="{ 'drop-zone--active': isDragOver, 'drop-zone--has-file': !!embyFile }"
          @dragover="onDragOver"
          @dragleave="onDragLeave"
          @drop="onDrop"
          @click="triggerEmbyFileInput"
        >
          <input
            ref="embyFileInputRef"
            type="file"
            accept=".db"
            style="display: none"
            @change="onEmbyFileChange"
          />

          <template v-if="!embyFile">
            <n-icon :component="CloudUploadOutline" :size="36" class="drop-zone__icon" />
            <span class="drop-zone__text">拖拽文件到此处或点击选择</span>
            <span class="drop-zone__hint">仅支持 .db 格式</span>
          </template>

          <template v-else>
            <n-icon :component="CheckmarkCircleOutline" :size="28" class="drop-zone__icon drop-zone__icon--ok" />
            <span class="drop-zone__file">{{ embyFile.name }}</span>
            <span class="drop-zone__size">{{ fileSizeStr }}</span>
            <n-button text size="tiny" class="drop-zone__reset" @click.stop="resetFile">重新选择</n-button>
          </template>
        </div>
      </n-card>

      <!-- Right: Policy -->
      <n-card class="glass-card migrate-card" :bordered="false">
        <template #header>
          <div class="card-header">
            <span class="step-badge">2</span>
            <span class="card-title">统一用户策略</span>
          </div>
        </template>

        <p class="card-desc">以下设置将应用于所有迁移的用户，迁移后可在用户管理中单独修改。</p>

        <div class="policy-grid">
          <n-checkbox v-model:checked="enableRemoteAccess">允许远程访问</n-checkbox>
          <n-checkbox v-model:checked="enableMediaPlayback">允许媒体播放</n-checkbox>
          <n-checkbox v-model:checked="enableAudioTranscoding">允许音频转码</n-checkbox>
          <n-checkbox v-model:checked="enableVideoTranscoding">允许视频转码</n-checkbox>
          <n-checkbox v-model:checked="enableContentDownloading">允许内容下载</n-checkbox>
          <n-checkbox v-model:checked="enableAllFolders">访问所有媒体库</n-checkbox>
        </div>

        <div class="number-grid">
          <div class="number-field">
            <label class="number-field__label">同时播放流数限制</label>
            <n-input-number
              v-model:value="simultaneousStreamLimit"
              :min="0"
              placeholder="0 = 不限"
              size="small"
            />
          </div>
          <div class="number-field">
            <label class="number-field__label">远程码率限制（bps）</label>
            <n-input-number
              v-model:value="remoteClientBitrateLimit"
              :min="0"
              placeholder="0 = 不限"
              size="small"
            />
          </div>
        </div>
      </n-card>
    </div>

    <!-- Action -->
    <div class="migrate-action">
      <n-button
        type="primary"
        size="large"
        :disabled="!embyFile || embyMigrating"
        :loading="embyMigrating"
        @click="handleEmbyMigrate"
      >
        <template #icon><n-icon :component="SwapHorizontalOutline" /></template>
        开始迁移
      </n-button>
    </div>

    <!-- Step 3: Result -->
    <transition name="fade-slide">
      <n-card v-if="embyResult" class="glass-card migrate-card" :bordered="false">
        <template #header>
          <div class="card-header">
            <span class="step-badge step-badge--done">
              <n-icon :component="CheckmarkCircleOutline" :size="14" />
            </span>
            <span class="card-title">迁移结果</span>
          </div>
        </template>

        <div class="result-stats">
          <div class="result-stat">
            <span class="result-stat__value result-stat__value--primary">{{ embyResult.imported }}</span>
            <span class="result-stat__label">已导入</span>
          </div>
          <div class="result-stat">
            <span class="result-stat__value result-stat__value--muted">{{ embyResult.skipped }}</span>
            <span class="result-stat__label">已跳过</span>
          </div>
          <div class="result-stat">
            <span class="result-stat__value">{{ embyResult.total }}</span>
            <span class="result-stat__label">总计</span>
          </div>
        </div>

        <n-alert
          v-if="embyResult.errors?.length > 0"
          type="error"
          title="部分错误"
          style="margin-top: 16px"
        >
          <div v-for="(e, i) in embyResult.errors" :key="i" class="error-line">{{ e }}</div>
        </n-alert>
      </n-card>
    </transition>
  </div>
</template>

<style scoped>
.migrate-layout {
  max-width: 960px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--app-section-gap);
}

.migrate-top {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--app-section-gap);
  align-items: stretch;
}

.migrate-card {
  overflow: hidden;
}

/* Card header */
.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-title {
  font-size: 16px;
  font-weight: 600;
}

.step-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 700;
  color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.12);
  flex-shrink: 0;
}

.step-badge--done {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
}

.card-desc {
  font-size: 13px;
  color: var(--app-text-muted);
  line-height: 1.7;
  margin-bottom: 20px;
}

.card-desc code {
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(148, 163, 184, 0.1);
  font-size: 12px;
  color: var(--app-text);
}

/* Drop zone */
.drop-zone {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 36px 20px;
  border: 2px dashed var(--app-border);
  border-radius: var(--app-radius);
  cursor: pointer;
  transition: border-color 0.2s ease, background 0.2s ease;
}

.drop-zone:hover {
  border-color: var(--app-border-hover);
  background: rgba(148, 163, 184, 0.03);
}

.drop-zone--active {
  border-color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.04);
}

.drop-zone--has-file {
  border-style: solid;
  border-color: rgba(16, 185, 129, 0.3);
  background: rgba(16, 185, 129, 0.04);
  padding: 24px 20px;
}

.drop-zone__icon {
  color: var(--app-text-muted);
  opacity: 0.5;
}

.drop-zone__icon--ok {
  color: #10b981;
  opacity: 1;
}

.drop-zone__text {
  font-size: 14px;
  color: var(--app-text-muted);
}

.drop-zone__hint {
  font-size: 12px;
  color: var(--app-text-muted);
  opacity: 0.6;
}

.drop-zone__file {
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
}

.drop-zone__size {
  font-size: 12px;
  color: var(--app-text-muted);
}

.drop-zone__reset {
  margin-top: 4px;
  font-size: 12px;
}

/* Policy */
.policy-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px 28px;
}

.number-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px 28px;
  margin-top: 20px;
}

.number-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.number-field__label {
  font-size: 13px;
  color: var(--app-text-muted);
}

/* Action */
.migrate-action {
  display: flex;
  justify-content: center;
}

/* Result stats */
.result-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  text-align: center;
}

.result-stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 16px 0;
  border-radius: var(--app-radius);
  background: var(--app-surface-1);
}

.result-stat__value {
  font-size: 32px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--app-text);
}

.result-stat__value--primary {
  color: var(--app-primary);
}

.result-stat__value--muted {
  color: var(--app-text-muted);
}

.result-stat__label {
  font-size: 12px;
  color: var(--app-text-muted);
}

.error-line {
  font-size: 13px;
  line-height: 1.6;
}

/* Responsive */
@media (max-width: 768px) {
  .migrate-top {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .policy-grid,
  .number-grid {
    grid-template-columns: 1fr;
  }

  .result-stats {
    grid-template-columns: 1fr;
    gap: 8px;
  }

  .result-stat {
    flex-direction: row;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
  }

  .result-stat__value {
    font-size: 24px;
  }
}
</style>
