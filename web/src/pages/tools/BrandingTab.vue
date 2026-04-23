<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NIcon,
  NInput,
} from 'naive-ui'
import { ImageOutline, SaveOutline, TrashOutline } from '@vicons/ionicons5'

import { getSystemConfig, updateSystemConfig } from '@/api/client'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import { svgToDataUrl, useBranding } from '@/composables/useBranding'
import { useToast } from '@/composables/useToast'

const { showToast } = useToast()
const branding = useBranding()

const serverName = ref('')
const iconSvg = ref('')
const saving = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const previewName = computed(() => serverName.value.trim() || branding.serverName.value || 'FYMS')
const previewIconUrl = computed(() => {
  const raw = iconSvg.value.trim()
  if (raw) return svgToDataUrl(raw)
  return branding.iconUrl.value || ''
})

function isSVGDocument(raw: string) {
  return raw.trim().toLowerCase().includes('<svg')
}

function openFilePicker() {
  fileInput.value?.click()
}

async function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  if (!file) return

  if (!file.name.toLowerCase().endsWith('.svg') && file.type !== 'image/svg+xml') {
    showToast('只支持上传 SVG 图标', 'error')
    input.value = ''
    return
  }

  try {
    const raw = await file.text()
    if (!isSVGDocument(raw)) {
      showToast('SVG 文件内容无效', 'error')
      return
    }
    iconSvg.value = raw.trim()
  } catch {
    showToast('读取 SVG 文件失败', 'error')
  } finally {
    input.value = ''
  }
}

function clearIcon() {
  iconSvg.value = ''
}

async function loadConfig() {
  try {
    await branding.loadBranding()
    const cfg = await getSystemConfig()
    serverName.value = cfg.brand_server_name || branding.serverName.value || 'FYMS'
    iconSvg.value = cfg.brand_icon_svg || ''
  } catch {
    showToast('加载品牌配置失败', 'error')
  }
}

async function saveBranding() {
  saving.value = true
  try {
    await updateSystemConfig({
      brand_server_name: serverName.value.trim(),
      brand_icon_svg: iconSvg.value.trim(),
    })
    await branding.refreshBranding()
    serverName.value = branding.serverName.value
    showToast('品牌设置已保存', 'success')
  } catch (err: any) {
    showToast(err?.message || '保存品牌设置失败', 'error')
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
    title="系统品牌"
    description="配置服务器名称和统一 SVG 图标，前台品牌位、登录页和浏览器 favicon 会同步更新。"
    :icon="AppIcons.server"
    body-class="branding-layout"
  >
    <n-card :bordered="false" class="glass-card section-card branding-card">
      <n-alert type="info" :show-icon="false" class="branding-alert">
        图标仅支持 SVG。保存后会同步影响 Emby `/System/Info*` 返回的服务器名称。
      </n-alert>

      <div class="brand-preview">
        <div class="brand-preview__badge">
          <img v-if="previewIconUrl" :src="previewIconUrl" class="brand-preview__icon" alt="" />
          <n-icon v-else :size="22"><ImageOutline /></n-icon>
        </div>
        <div class="brand-preview__meta">
          <div class="brand-preview__title">{{ previewName }}</div>
          <div class="brand-preview__sub">前台品牌位与 favicon 预览</div>
        </div>
      </div>

      <n-form label-placement="top">
        <n-form-item label="服务器名称">
          <n-input v-model:value="serverName" placeholder="例如：家庭影院" maxlength="64" />
        </n-form-item>

        <n-form-item label="SVG 图标">
          <div class="icon-actions">
            <input
              ref="fileInput"
              type="file"
              accept=".svg,image/svg+xml"
              class="hidden-file-input"
              @change="onFileChange"
            />
            <n-button secondary @click="openFilePicker">
              <template #icon><n-icon><ImageOutline /></n-icon></template>
              选择 SVG
            </n-button>
            <n-button secondary type="error" :disabled="!iconSvg.trim()" @click="clearIcon">
              <template #icon><n-icon><TrashOutline /></n-icon></template>
              清空图标
            </n-button>
          </div>
          <div class="field-hint">建议使用正方形 SVG，前台和浏览器标签页都会复用这个图标。</div>
        </n-form-item>

        <n-form-item label="SVG 源码预览">
          <n-input
            :value="iconSvg"
            type="textarea"
            :autosize="{ minRows: 8, maxRows: 16 }"
            placeholder="上传后这里会显示 SVG 源码；也可以直接粘贴 SVG。"
            @update:value="iconSvg = $event"
          />
        </n-form-item>
      </n-form>

      <div class="card-actions">
        <n-button type="primary" :loading="saving" @click="saveBranding">
          <template #icon><n-icon><SaveOutline /></n-icon></template>
          保存设置
        </n-button>
      </div>
    </n-card>
  </page-shell>
</template>

<style scoped>
:deep(.branding-layout) {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.branding-card {
  min-height: 100%;
}

.branding-alert {
  margin-bottom: 16px;
}

.brand-preview {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 20px;
  padding: 18px 20px;
  border-radius: 18px;
  background: linear-gradient(135deg, rgba(var(--app-primary-rgb), 0.12), rgba(14, 165, 233, 0.08));
  border: 1px solid rgba(var(--app-primary-rgb), 0.16);
}

.brand-preview__badge {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.9);
  color: var(--app-primary);
  flex-shrink: 0;
}

.brand-preview__icon {
  width: 28px;
  height: 28px;
  display: block;
}

.brand-preview__title {
  font-size: 18px;
  font-weight: 700;
  color: var(--app-text);
}

.brand-preview__sub {
  margin-top: 4px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.icon-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.hidden-file-input {
  display: none;
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
