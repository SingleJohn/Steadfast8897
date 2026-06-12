<script setup lang="ts">
import { NButton, NCheckbox, NCheckboxGroup, NInput, NModal, NSpace, NSelect } from 'naive-ui'

defineProps<{
  showPlatformCover: boolean
  platformCoverTargetId: string | null
  platformCoverStyle: string
  coverStyleOptions: { label: string; value: string }[]
  coverStylesLoaded: boolean
  showcaseIconOptions: { label: string; value: string }[]
  platformShowcaseIcon: string
  platformShowcaseShowPosterTitles: boolean
  platformShowcaseShowCount: boolean
  generatingPlatformCover: boolean
  showRename: boolean
  renameValue: string
  showAlias: boolean
  aliasTarget: any
  aliasValues: string[]
  aliasSearch: string
  aliasResults: { Value: string; Count: number; AlreadyAdded: boolean }[]
  aliasSelected: string[]
  aliasLoading: boolean
  solidModalMenuProps: Record<string, any>
  forceSolidModalStyle: Record<string, string>
}>()

const emit = defineEmits<{
  updateShowPlatformCover: [value: boolean]
  updatePlatformCoverStyle: [value: string]
  updatePlatformShowcaseIcon: [value: string]
  updatePlatformShowcaseShowPosterTitles: [value: boolean]
  updatePlatformShowcaseShowCount: [value: boolean]
  confirmPlatformCover: []
  updateShowRename: [value: boolean]
  updateRenameValue: [value: string]
  confirmRename: []
  updateShowAlias: [value: boolean]
  removeAlias: [value: string]
  updateAliasSearch: [value: string]
  runAliasDiscover: []
  updateAliasSelected: [value: string[]]
  addAliasSelected: []
}>()

</script>

<template>
  <n-modal
    :show="showPlatformCover"
    preset="card"
    :title="platformCoverTargetId ? '生成虚拟库封面' : '一键生成虚拟库封面'"
    :style="[forceSolidModalStyle, { width: '480px', maxWidth: '92vw' }]"
    class="solid-modal-card force-solid-modal"
    @update:show="emit('updateShowPlatformCover', $event)"
  >
    <div class="form-group">
      <label class="form-label">封面风格</label>
      <n-select
        :value="platformCoverStyle"
        :options="coverStyleOptions"
        :loading="!coverStylesLoaded"
        :menu-props="solidModalMenuProps"
        placeholder="选择风格"
        @update:value="emit('updatePlatformCoverStyle', $event)"
      />
    </div>
    <div v-if="platformCoverStyle === 'showcase'" class="batch-cover-options">
      <div class="form-group">
        <label class="form-label">预制图标</label>
        <n-select
          :value="platformShowcaseIcon"
          :options="showcaseIconOptions"
          :menu-props="solidModalMenuProps"
          @update:value="emit('updatePlatformShowcaseIcon', $event)"
        />
      </div>
      <div class="batch-cover-checks">
        <n-checkbox :checked="platformShowcaseShowPosterTitles" @update:checked="emit('updatePlatformShowcaseShowPosterTitles', $event)">显示海报标题</n-checkbox>
        <n-checkbox :checked="platformShowcaseShowCount" @update:checked="emit('updatePlatformShowcaseShowCount', $event)">显示媒体数量</n-checkbox>
      </div>
    </div>
    <div v-if="!platformCoverTargetId" class="setting-desc">将为所有已启用的虚拟库生成封面，无海报素材的会自动跳过。</div>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShowPlatformCover', false)">取消</n-button>
        <n-button type="primary" :loading="generatingPlatformCover" :disabled="!platformCoverStyle || generatingPlatformCover" @click="emit('confirmPlatformCover')">
          {{ platformCoverTargetId ? '生成' : '生成全部' }}
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal
    :show="showRename"
    preset="card"
    title="自定义虚拟库名称"
    :style="[forceSolidModalStyle, { width: '440px', maxWidth: '92vw' }]"
    class="solid-modal-card force-solid-modal"
    @update:show="emit('updateShowRename', $event)"
  >
    <div class="form-group">
      <label class="form-label">显示名称</label>
      <n-input :value="renameValue" placeholder="留空则恢复默认名称" @update:value="emit('updateRenameValue', $event)" @keydown.enter.prevent="emit('confirmRename')" />
      <div class="setting-desc modal-desc">仅改变在播放器中显示的名称，不影响分组匹配与图标。</div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShowRename', false)">取消</n-button>
        <n-button type="primary" @click="emit('confirmRename')">保存</n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal
    :show="showAlias"
    preset="card"
    :title="`聚合匹配值 · ${aliasTarget?.DisplayName || aliasTarget?.PlatformName || ''}`"
    :style="[forceSolidModalStyle, { width: '560px', maxWidth: '92vw' }]"
    class="solid-modal-card force-solid-modal"
    @update:show="emit('updateShowAlias', $event)"
  >
    <div class="form-group">
      <label class="form-label">已绑定的值（{{ aliasTarget?.Dimension }} 维度）</label>
      <div class="alias-chips">
        <span v-for="v in aliasValues" :key="v" class="alias-chip" :class="{ 'is-primary': v === aliasTarget?.MatchValue }">
          {{ v }}
          <span v-if="v === aliasTarget?.MatchValue" class="alias-primary-tag">主</span>
          <button v-else class="alias-chip-remove" type="button" title="移除" @click="emit('removeAlias', v)">×</button>
        </span>
      </div>
      <div class="setting-desc modal-desc">将簡繁/译名等同一实体的不同写法合并到此库；主值不可移除。</div>
    </div>
    <div class="form-group">
      <label class="form-label">查找并合并更多值</label>
      <div class="alias-search-row">
        <n-input :value="aliasSearch" placeholder="搜索同维度的值（可选）" size="small" @update:value="emit('updateAliasSearch', $event)" @keydown.enter.prevent="emit('runAliasDiscover')" />
        <n-button secondary size="small" :loading="aliasLoading" @click="emit('runAliasDiscover')">扫描</n-button>
      </div>
      <div v-if="aliasResults.length > 0" class="alias-results">
        <n-checkbox-group :value="aliasSelected" @update:value="emit('updateAliasSelected', $event as string[])">
          <div class="discover-grid">
            <n-checkbox v-for="d in aliasResults" :key="d.Value" :value="d.Value">
              {{ d.Value }} <span class="platform-count">{{ d.Count }}</span>
              <span v-if="d.AlreadyAdded" class="already-added">(其他库已用)</span>
            </n-checkbox>
          </div>
        </n-checkbox-group>
        <div class="alias-actions">
          <n-button type="primary" size="small" :disabled="aliasSelected.length === 0" @click="emit('addAliasSelected')">
            合并所选 ({{ aliasSelected.length }})
          </n-button>
        </div>
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('updateShowAlias', false)">关闭</n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.form-group { margin-bottom: 20px; }
.form-label { display: block; font-size: 12px; color: var(--app-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500; }
.setting-desc { font-size: 12px; color: var(--app-text-muted); margin-top: 2px; }
.modal-desc { margin-top: 6px; }
.batch-cover-options { padding-top: 4px; }
.batch-cover-checks {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.alias-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.alias-chip { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 6px; font-size: 13px; color: var(--app-text); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04)); }
.alias-chip.is-primary { border-color: rgba(var(--app-primary-rgb), 0.5); }
.alias-primary-tag { font-size: 10px; color: var(--app-primary); }
.alias-chip-remove { border: 0; background: transparent; color: var(--app-text-muted); cursor: pointer; font-size: 15px; line-height: 1; padding: 0; }
.alias-chip-remove:hover { color: #d03050; }
.alias-search-row { display: flex; gap: 8px; align-items: center; }
.alias-results { margin-top: 10px; }
.alias-actions { margin-top: 8px; }
.discover-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 6px 12px; max-height: 320px; overflow-y: auto; padding: 4px 2px; }
.platform-count { font-size: 12px; color: var(--app-text-muted); margin-left: 8px; }
.already-added { color: var(--n-text-color-disabled); font-size: 11px; }
</style>
