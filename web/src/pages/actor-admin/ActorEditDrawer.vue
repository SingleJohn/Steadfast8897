<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  NButton,
  NDrawer,
  NDrawerContent,
  NDynamicInput,
  NDynamicTags,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NSpin,
  useMessage,
} from 'naive-ui'
import {
  deletePersonImage,
  getActor,
  getImageUrl,
  updateActor,
  uploadPersonImage,
} from '@/api/client'

const props = defineProps<{ show: boolean; actorId: string | null }>()
const emit = defineEmits<{
  'update:show': [boolean]
  saved: []        // 资料/图片有变更,父组件刷新该行
}>()

const message = useMessage()

const loading = ref(false)
const saving = ref(false)
const detail = ref<any>(null)

const form = reactive({
  overview: '',
  premiereDate: '',
  productionYear: null as number | null,
  locations: [] as string[],
  tags: [] as string[],
  taglines: [] as string[],
  providerPairs: [] as { key: string; value: string }[],
  imageLocked: false,
})

const show = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

function imgSrc(type: 'Primary' | 'Backdrop'): string {
  if (!detail.value) return ''
  return getImageUrl(detail.value.Id, type, { maxWidth: 320, tag: detail.value.ImageTag })
}

const hasImage = computed(() => !!detail.value?.HasImage)
const hasBackdrop = computed(() => !!detail.value?.HasBackdrop)

async function load() {
  if (!props.actorId) return
  loading.value = true
  detail.value = null
  try {
    const d = await getActor(props.actorId)
    detail.value = d
    form.overview = d.Overview || ''
    form.premiereDate = d.PremiereDate || ''
    form.productionYear = d.ProductionYear ?? null
    form.locations = [...(d.ProductionLocations || [])]
    form.tags = [...(d.Tags || [])]
    form.taglines = [...(d.Taglines || [])]
    form.providerPairs = Object.entries(d.ProviderIds || {}).map(([key, value]) => ({
      key,
      value: String(value ?? ''),
    }))
    form.imageLocked = !!d.ImageLocked
  } catch (e: any) {
    message.error(e?.message || '加载演员信息失败')
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.show, props.actorId],
  () => {
    if (props.show && props.actorId) void load()
  },
  { immediate: true },
)

function pairsToMap(pairs: { key: string; value: string }[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of pairs) {
    const k = (p.key || '').trim()
    if (k) out[k] = (p.value || '').trim()
  }
  return out
}

async function save() {
  if (!detail.value) return
  saving.value = true
  try {
    const body = {
      Overview: form.overview,
      PremiereDate: form.premiereDate,
      ProductionYear: form.productionYear,
      ProductionLocations: form.locations,
      Tags: form.tags,
      Taglines: form.taglines,
      ProviderIds: pairsToMap(form.providerPairs),
      ImageLocked: form.imageLocked,
    }
    detail.value = await updateActor(detail.value.Id, body)
    message.success('已保存')
    emit('saved')
  } catch (e: any) {
    message.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const uploading = ref<'Primary' | 'Backdrop' | ''>('')

async function pickAndUpload(type: 'Primary' | 'Backdrop') {
  if (!detail.value) return
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const file = input.files?.[0]
    if (!file) return
    uploading.value = type
    try {
      await uploadPersonImage(detail.value.Id, type, file)
      await load()
      message.success(type === 'Primary' ? '头像已更新' : '背景图已更新')
      emit('saved')
    } catch (e: any) {
      message.error(e?.message || '上传失败')
    } finally {
      uploading.value = ''
    }
  }
  input.click()
}

async function removeImage(type: 'Primary' | 'Backdrop') {
  if (!detail.value) return
  try {
    await deletePersonImage(detail.value.Id, type)
    await load()
    message.success('已删除')
    emit('saved')
  } catch (e: any) {
    message.error(e?.message || '删除失败')
  }
}
</script>

<template>
  <n-drawer v-model:show="show" :width="520" placement="right">
    <n-drawer-content :title="detail?.Name || '编辑演员'" closable>
      <n-spin :show="loading">
        <div v-if="detail" class="edit-body">
          <!-- 图片 -->
          <div class="img-row">
            <div class="img-block">
              <div class="img-label">头像</div>
              <div class="img-box img-box--avatar">
                <img v-if="hasImage" :src="imgSrc('Primary')" alt="" />
                <span v-else class="img-empty">无</span>
              </div>
              <div class="img-actions">
                <n-button size="tiny" :loading="uploading === 'Primary'" @click="pickAndUpload('Primary')">上传/替换</n-button>
                <n-button v-if="hasImage" size="tiny" quaternary type="error" @click="removeImage('Primary')">删除</n-button>
              </div>
            </div>
            <div class="img-block">
              <div class="img-label">背景图</div>
              <div class="img-box img-box--backdrop">
                <img v-if="hasBackdrop" :src="imgSrc('Backdrop')" alt="" />
                <span v-else class="img-empty">无</span>
              </div>
              <div class="img-actions">
                <n-button size="tiny" :loading="uploading === 'Backdrop'" @click="pickAndUpload('Backdrop')">上传/替换</n-button>
                <n-button v-if="hasBackdrop" size="tiny" quaternary type="error" @click="removeImage('Backdrop')">删除</n-button>
              </div>
            </div>
          </div>

          <n-form label-placement="top" size="small" class="edit-form">
            <n-form-item label="锁定头像（锁定后刮削不覆盖）">
              <n-switch v-model:value="form.imageLocked" />
            </n-form-item>
            <n-form-item label="简介">
              <n-input v-model:value="form.overview" type="textarea" :autosize="{ minRows: 3, maxRows: 8 }" placeholder="演员简介" />
            </n-form-item>
            <div class="form-grid">
              <n-form-item label="出生日期">
                <n-input v-model:value="form.premiereDate" placeholder="YYYY-MM-DD" />
              </n-form-item>
              <n-form-item label="出生年">
                <n-input-number v-model:value="form.productionYear" :show-button="false" placeholder="如 1992" style="width: 100%" />
              </n-form-item>
            </div>
            <n-form-item label="出身地">
              <n-dynamic-tags v-model:value="form.locations" />
            </n-form-item>
            <n-form-item label="标签（罩杯 / 身高 / 三围 等）">
              <n-dynamic-tags v-model:value="form.tags" />
            </n-form-item>
            <n-form-item label="副标题 / Taglines">
              <n-dynamic-tags v-model:value="form.taglines" />
            </n-form-item>
            <n-form-item label="外部 ID（平台 → 账号/ID）">
              <n-dynamic-input
                v-model:value="form.providerPairs"
                preset="pair"
                key-placeholder="平台（如 Tmdb / Twitter）"
                value-placeholder="ID / 账号"
              />
            </n-form-item>
          </n-form>

          <div class="footer-meta">作品数：{{ detail.WorkCount }}</div>
        </div>
      </n-spin>

      <template #footer>
        <div class="drawer-footer">
          <n-button @click="show = false">取消</n-button>
          <n-button type="primary" :loading="saving" @click="save">保存</n-button>
        </div>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>

<style scoped>
.edit-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.img-row {
  display: flex;
  gap: 16px;
}

.img-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.img-label {
  font-size: 12px;
  color: var(--app-text-muted, #888);
  font-weight: 600;
}

.img-box {
  border-radius: 10px;
  overflow: hidden;
  background: rgba(128, 128, 128, 0.12);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--app-text-muted, #888);
}

.img-box--avatar {
  width: 120px;
  height: 120px;
  border-radius: 50%;
}

.img-box--backdrop {
  width: 200px;
  height: 120px;
}

.img-box img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.img-actions {
  display: flex;
  gap: 6px;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.footer-meta {
  font-size: 12px;
  color: var(--app-text-muted, #888);
}

.drawer-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
</style>
