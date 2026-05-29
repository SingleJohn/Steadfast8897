<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NIcon,
  NInputNumber,
  NSlider,
} from 'naive-ui'
import { SaveOutline } from '@vicons/ionicons5'

import { getSystemConfig, updateSystemConfig } from '@/api/client'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { useToast } from '@/composables/useToast'

const { showToast } = useToast()

const DEFAULT_THRESHOLD = 90
const playedThreshold = ref(DEFAULT_THRESHOLD)
const saving = ref(false)

async function loadConfig() {
  try {
    const cfg = await getSystemConfig()
    const raw = parseInt(cfg.playback_played_threshold ?? '', 10)
    playedThreshold.value = Number.isFinite(raw) && raw >= 1 && raw <= 100 ? raw : DEFAULT_THRESHOLD
  } catch {
    showToast('加载播放设置失败', 'error')
  }
}

async function savePlayback() {
  let v = Math.round(playedThreshold.value || DEFAULT_THRESHOLD)
  if (v < 1) v = 1
  if (v > 100) v = 100
  playedThreshold.value = v
  saving.value = true
  try {
    await updateSystemConfig({
      playback_played_threshold: String(v),
    })
    showToast('播放设置已保存', 'success')
  } catch (err: any) {
    showToast(err?.message || '保存播放设置失败', 'error')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadConfig()
})
</script>

<template>
  <page-shell
    title="播放设置"
    description="控制播放进度达到多少比例时判定为“已看完”。看完的剧集会离开“继续观看”，并通过“接下来观看”推送下一集。"
    :icon="AppIcons.play"
    body-class="playback-layout"
  >
    <n-card :bordered="false" class="glass-card section-card">
      <n-alert type="info" :show-icon="false" class="playback-alert">
        阈值越高越接近“看到片尾”才算看完；如果你的剧集片尾/预告较长，可适当调低（如 85）。修改后对之后的播放生效。
      </n-alert>

      <n-form label-placement="top">
        <n-form-item label="看完判定阈值 (%)">
          <div class="threshold-row">
            <n-slider
              v-model:value="playedThreshold"
              :min="1"
              :max="100"
              :step="1"
              :tooltip="true"
              style="flex: 1"
            />
            <n-input-number
              v-model:value="playedThreshold"
              :min="1"
              :max="100"
              :step="1"
              size="small"
              style="width: 110px"
            />
          </div>
          <div class="field-hint">播放进度超过该百分比即标记为已看完。推荐 90。</div>
        </n-form-item>
      </n-form>

      <div class="card-actions">
        <n-button type="primary" :loading="saving" @click="savePlayback">
          <template #icon><n-icon><SaveOutline /></n-icon></template>
          保存设置
        </n-button>
      </div>
    </n-card>
  </page-shell>
</template>

<style scoped>
:deep(.playback-layout) {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.playback-alert {
  margin-bottom: 16px;
}

.threshold-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.field-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.card-actions {
  display: flex;
  justify-content: flex-end;
}
</style>
