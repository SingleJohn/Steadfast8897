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
  NSwitch,
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

// strm 条目 item.Path 返回模式：true=返回 .strm 文件路径(对齐 Emby)；false=返回解析后的内层真实路径。
const strmPathAsStrm = ref(true)
const savingStrmMode = ref(false)

async function loadConfig() {
  try {
    const cfg = await getSystemConfig()
    const raw = parseInt(cfg.playback_played_threshold ?? '', 10)
    playedThreshold.value = Number.isFinite(raw) && raw >= 1 && raw <= 100 ? raw : DEFAULT_THRESHOLD
    // 默认 'strm'(对齐 Emby)；仅显式设为 'resolved' 时关闭。
    strmPathAsStrm.value = (cfg.strm_item_path_mode || 'strm') !== 'resolved'
  } catch {
    showToast('加载播放设置失败', 'error')
  }
}

async function saveStrmMode(next: boolean) {
  savingStrmMode.value = true
  try {
    await updateSystemConfig({ strm_item_path_mode: next ? 'strm' : 'resolved' })
    strmPathAsStrm.value = next
    showToast('strm 路径模式已保存', 'success')
  } catch (err: any) {
    showToast(err?.message || '保存 strm 路径模式失败', 'error')
  } finally {
    savingStrmMode.value = false
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

    <n-card :bordered="false" class="glass-card section-card">
      <template #header>
        <div class="strm-header">
          <div class="strm-header-title">strm 路径返回模式</div>
          <div class="strm-header-desc">控制 `.strm` 媒体在接口与通知里 item 级 `Path` 字段的取值。</div>
        </div>
      </template>

      <div class="strm-toggle-row">
        <n-switch
          :value="strmPathAsStrm"
          :loading="savingStrmMode"
          :disabled="savingStrmMode"
          @update:value="saveStrmMode"
        />
        <div class="strm-toggle-copy">
          <div class="strm-toggle-label">
            {{ strmPathAsStrm ? '返回 .strm 文件路径（对齐 Emby）' : '返回解析后的真实路径' }}
          </div>
          <div class="strm-toggle-state">
            当前值：<code>{{ strmPathAsStrm ? 'strm' : 'resolved' }}</code>
          </div>
        </div>
      </div>

      <n-alert type="info" :show-icon="false" class="strm-explain">
        <div class="strm-explain-title">说明</div>
        <ul class="strm-explain-list">
          <li><strong>开启（strm，默认）</strong>：item 级 <code>Path</code> 返回 `.strm` 文件本身的路径，与 Emby 行为一致。</li>
          <li><strong>关闭（resolved）</strong>：item 级 <code>Path</code> 返回读取 `.strm` 后解析出的内层真实地址（FYMS 旧行为）。</li>
          <li>此开关<strong>全局生效</strong>：同时影响详情 <code>/Items</code>、列表 <code>/Items/Latest</code>、以及出站通知的 <code>Path</code> 字段。</li>
          <li><strong>不影响播放</strong>：实际出流走 <code>MediaSources.Path</code>，始终为解析后的真实地址，与本开关无关。</li>
          <li>两种模式下 <code>Container</code> 都返回真实内层容器（如 <code>mp4</code>），不会暴露为 `strm`。</li>
        </ul>
      </n-alert>
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

.strm-header-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--app-text);
}

.strm-header-desc {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.strm-toggle-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 4px 0 14px;
}

.strm-toggle-copy {
  min-width: 0;
}

.strm-toggle-label {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}

.strm-toggle-state {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.strm-explain-title {
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 6px;
}

.strm-explain-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.6;
}

.strm-explain-list code,
.strm-toggle-state code {
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 11px;
  padding: 1px 4px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.2);
}
</style>
