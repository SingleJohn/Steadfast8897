<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { NModal, NSpace, NButton, NCheckbox, NSwitch, NSelect, NInputNumber, NTag } from 'naive-ui'
import {
  adminToggles, policyGroups, streamLimitOptions, permissionTemplates,
  templatePatch, defaultFullPolicy, POLICY_UNSUPPORTED_HINT,
  type PolicyKey, type PolicyState, type ToggleDef,
} from './policyFields'

const props = defineProps<{
  show: boolean
  selectedCount: number
  loading: boolean
  modalStyle?: Record<string, any>
  menuProps?: Record<string, any>
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'apply', patch: Record<string, any>): void
}>()

// 仅批量管理"管理员 / 偏好设置"两项账户开关，禁用/隐藏由批量栏的专用按钮负责。
const accountToggles: ToggleDef[] = adminToggles.filter(
  t => t.key === 'IsAdministrator' || t.key === 'EnableUserPreferenceAccess',
)

const groups = [{ title: '账户', toggles: accountToggles }, ...policyGroups]

// 每个字段是否纳入本次批量修改（未勾选 = 保持各用户原值不变）。
const applyKeys = reactive<Record<string, boolean>>({})
// 字段取值。
const values = reactive<PolicyState>(defaultFullPolicy())
const applyStreamLimit = ref(false)
const template = ref<string | null>(null)

function resetState() {
  for (const key of Object.keys(applyKeys)) delete applyKeys[key]
  Object.assign(values, defaultFullPolicy())
  applyStreamLimit.value = false
  template.value = null
}

watch(() => props.show, (open) => { if (open) resetState() })

// 仅纳入本服务真正生效的开关:置灰项不参与套用模板/全选/下发,避免批量写入无效字段。
const flatToggleKeys: PolicyKey[] = groups.flatMap(g => g.toggles.filter(t => t.effective).map(t => t.key))

function toggleHint(row: ToggleDef): string | undefined {
  if (row.effective) return undefined
  return row.disabledHint || POLICY_UNSUPPORTED_HINT
}

// 套用模板：勾选受影响的开关项并填入模板值，用户仍可继续微调。
function applyTemplate(key: string | null) {
  if (!key) return
  const base = defaultFullPolicy()
  const patch = templatePatch(key) || {}
  const merged = { ...base, ...patch } as PolicyState
  for (const k of flatToggleKeys) {
    applyKeys[k] = true
    ;(values as any)[k] = (merged as any)[k]
  }
  applyStreamLimit.value = true
  values.SimultaneousStreamLimit = merged.SimultaneousStreamLimit
}

function selectAll(on: boolean) {
  for (const k of flatToggleKeys) applyKeys[k] = on
  applyStreamLimit.value = on
}

const checkedCount = () => flatToggleKeys.filter(k => applyKeys[k]).length
  + (applyStreamLimit.value ? 1 : 0)

function buildPatch(): Record<string, any> {
  const patch: Record<string, any> = {}
  for (const k of flatToggleKeys) {
    if (applyKeys[k]) patch[k] = !!(values as any)[k]
  }
  if (applyStreamLimit.value) patch.SimultaneousStreamLimit = values.SimultaneousStreamLimit
  return patch
}

function handleApply() {
  const patch = buildPatch()
  if (Object.keys(patch).length === 0) return
  emit('apply', patch)
}
</script>

<template>
  <n-modal
    :show="show"
    @update:show="(v: boolean) => emit('update:show', v)"
    preset="card"
    title="批量修改权限策略"
    class="glass-modal force-solid-modal"
    :style="[modalStyle, { width: '600px', maxWidth: '94vw' }]"
  >
    <div class="bulk-policy">
      <div class="hint-row">
        <span>
          将应用于选中的 <strong>{{ selectedCount }}</strong> 个用户。
          仅<strong>勾选「应用」</strong>的项会被覆盖，其余保持各用户原值。
        </span>
      </div>

      <div class="quick-row">
        <span class="quick-label">套用模板</span>
        <n-select
          v-model:value="template"
          :options="permissionTemplates"
          placeholder="选择模板自动填充"
          size="small"
          clearable
          style="width: 180px"
          :menu-props="menuProps"
          @update:value="applyTemplate"
        />
        <div style="flex: 1" />
        <n-button text size="small" @click="selectAll(true)">全部应用</n-button>
        <n-button text size="small" @click="selectAll(false)">全部不改</n-button>
      </div>

      <div v-for="group in groups" :key="group.title" class="section-card">
        <h3 class="section-title">{{ group.title }}</h3>
        <div v-for="row in group.toggles" :key="row.key" class="policy-row" :class="{ inactive: !applyKeys[row.key] || !row.effective }" :title="toggleHint(row)">
          <n-checkbox :checked="!!applyKeys[row.key]" :disabled="!row.effective" @update:checked="(v: boolean) => applyKeys[row.key] = v">
            <span class="apply-label">应用</span>
          </n-checkbox>
          <div class="policy-label">
            <span>{{ row.label }}</span>
            <span v-if="row.desc" class="policy-desc">{{ row.desc }}</span>
            <span v-else-if="!row.effective" class="policy-desc">{{ toggleHint(row) }}</span>
          </div>
          <n-switch
            :value="!!(values as any)[row.key]"
            :disabled="!applyKeys[row.key] || !row.effective"
            size="small"
            @update:value="(v: boolean) => (values as any)[row.key] = v"
          />
        </div>
      </div>

      <div class="section-card">
        <h3 class="section-title">播放限制</h3>
        <div class="policy-row" :class="{ inactive: !applyStreamLimit }">
          <n-checkbox v-model:checked="applyStreamLimit"><span class="apply-label">应用</span></n-checkbox>
          <div class="policy-label">
            <span>最大同时播放数</span>
            <span class="policy-desc">0 表示不限制</span>
          </div>
          <n-select
            v-model:value="values.SimultaneousStreamLimit"
            :options="streamLimitOptions"
            :disabled="!applyStreamLimit"
            size="small"
            style="width: 100px"
            :menu-props="menuProps"
          />
        </div>
        <div class="policy-row inactive" :title="POLICY_UNSUPPORTED_HINT">
          <n-checkbox :checked="false" disabled><span class="apply-label">应用</span></n-checkbox>
          <div class="policy-label">
            <span>远程码率限制（bps）</span>
            <span class="policy-desc">{{ POLICY_UNSUPPORTED_HINT }}</span>
          </div>
          <n-input-number
            :value="values.RemoteClientBitrateLimit"
            :min="0"
            disabled
            size="small"
            style="width: 160px"
          />
        </div>
      </div>
    </div>

    <template #action>
      <div class="modal-actions">
        <n-tag v-if="checkedCount() > 0" size="small" :bordered="false" round type="info">
          将修改 {{ checkedCount() }} 项
        </n-tag>
        <div style="flex: 1" />
        <n-button @click="emit('update:show', false)">取消</n-button>
        <n-button type="primary" :loading="loading" :disabled="checkedCount() === 0" @click="handleApply">
          应用到 {{ selectedCount }} 个用户
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.bulk-policy { display: flex; flex-direction: column; gap: 12px; max-height: 64vh; overflow-y: auto; padding-right: 4px; }

.hint-row {
  font-size: 13px; color: var(--app-text-muted); line-height: 1.6;
}
.hint-row strong { color: var(--app-text); }

.quick-row {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 12px;
  background: var(--app-modal-panel-bg, var(--app-surface-1));
  border: 1px solid var(--app-border); border-radius: 8px;
}
.quick-label { font-size: 13px; color: var(--app-text-muted); }

.section-card {
  background: var(--app-modal-panel-bg, var(--app-surface-1));
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  padding: 12px 16px;
}

.section-title {
  font-size: 13px; font-weight: 600; color: var(--app-text);
  margin: 0 0 8px; padding-bottom: 8px;
  border-bottom: 1px solid var(--app-border);
}

.policy-row {
  display: flex; align-items: center; gap: 14px;
  padding: 7px 0; min-height: 34px;
}
.policy-row + .policy-row { border-top: 1px solid rgba(128,128,128,0.08); }
.policy-row.inactive .policy-label { opacity: 0.5; }

.apply-label { font-size: 12px; color: var(--app-text-muted); }

.policy-label {
  flex: 1; font-size: 13px; color: var(--app-text);
  display: flex; flex-direction: column; gap: 2px;
}
.policy-desc { font-size: 11px; color: var(--app-text-muted); }

.modal-actions { display: flex; align-items: center; gap: 8px; width: 100%; }
</style>
