<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NButton, NModal, NInput, NSpace, NTag, NSpin, NIcon, NEmpty,
  NSwitch, NSelect, NCheckbox, NScrollbar, NPagination,
} from 'naive-ui'
import { PersonAddOutline, ShieldCheckmarkOutline, TrashOutline } from '@vicons/ionicons5'
import {
  getAllUsers, createNewUser, getUserDetail, updateUserPolicy,
  changeUserPassword, deleteUserById, getLibraries, updateUserInfo,
} from '../api/client'
import { useAuth } from '../composables/useAuth'
import { useToast } from '../composables/useToast'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import UserBulkBar from './user-management/UserBulkBar.vue'
import UserBulkPolicyModal from './user-management/UserBulkPolicyModal.vue'
import UserSelectionBar from './user-management/UserSelectionBar.vue'
import UserManagementList from './user-management/UserManagementList.vue'
import UserManagementToolbar from './user-management/UserManagementToolbar.vue'
import {
  adminToggles, playbackToggles, featureToggles, remoteToggles,
  streamLimitOptions, permissionTemplates, templatePatch,
  POLICY_UNSUPPORTED_HINT,
  type PolicyState, type ToggleDef,
} from './user-management/policyFields'

const { auth } = useAuth()
const { showToast } = useToast()

const users = ref<any[]>([])
const libraries = ref<any[]>([])
const loading = ref(true)
const searchTerm = ref('')
const statusFilter = ref('all')
const groupFilter = ref('all')
const viewMode = ref<'card' | 'table'>('card')
const selectedUserIds = ref<string[]>([])

const showCreate = ref(false)
const newName = ref('')
const newPassword = ref('')
const newTemplate = ref('standard')
const createError = ref('')
const creating = ref(false)

const editUserId = ref<string | null>(null)
const editUser = ref<any>(null)
const editPolicy = ref<PolicyState | null>(null)
const editUsername = ref('')
const editLoading = ref(false)
const editSaving = ref(false)
const editLibraries = ref<any[]>([])
const editFolderChecks = reactive<Record<string, boolean>>({})
const editCurrentPw = ref('')
const editNewPw = ref('')
const editConfirmPw = ref('')
const showDeleteConfirm = ref(false)
const showBulkDeleteConfirm = ref(false)
const showBulkLibraryAccess = ref(false)
const showBulkPolicy = ref(false)
const bulkSaving = ref(false)

// 客户端分页：一次性拉全部用户，仅渲染当前页，跨页选择不受影响。
const currentPage = ref(1)
const pageSize = ref(24)
const pageSizeOptions = [
  { label: '24 / 页', value: 24 },
  { label: '48 / 页', value: 48 },
  { label: '96 / 页', value: 96 },
]
const bulkEnableAllFolders = ref(true)
const bulkFolderChecks = reactive<Record<string, boolean>>({})
const solidModalMenuProps = { class: 'solid-modal-menu' }
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

const showEditModal = computed(() => editUserId.value !== null)

const statusOptions = [
  { label: '记录状态', value: 'all' },
  { label: '正常', value: 'active' },
  { label: '已禁用', value: 'disabled' },
  { label: '登录页隐藏', value: 'hidden' },
  { label: '从未登录', value: 'never' },
]
const groupOptions = [
  { label: '按分组筛选', value: 'all' },
  { label: '管理员', value: 'admin' },
  { label: '普通用户', value: 'user' },
  { label: '受限媒体库', value: 'restricted' },
]
const libraryNameMap = computed(() => {
  const map: Record<string, string> = {}
  for (const lib of libraries.value) {
    const id = String(lib.ItemId || lib.Id || '')
    if (id) map[id] = lib.Name || id
  }
  return map
})

const visibleUsers = computed(() => {
  const sorted = [...users.value].sort((a, b) => {
    const aAdmin = a.Policy?.IsAdministrator ? 1 : 0
    const bAdmin = b.Policy?.IsAdministrator ? 1 : 0
    if (aAdmin !== bAdmin) return bAdmin - aAdmin
    return (a.Name || '').localeCompare(b.Name || '')
  })
  const q = searchTerm.value.trim().toLowerCase()
  return sorted.filter((u) => {
    const policy = u.Policy || {}
    const matchesSearch = !q || u.Name?.toLowerCase().includes(q)
    const matchesStatus = statusFilter.value === 'all'
      || (statusFilter.value === 'active' && !policy.IsDisabled)
      || (statusFilter.value === 'disabled' && policy.IsDisabled)
      || (statusFilter.value === 'hidden' && policy.IsHidden)
      || (statusFilter.value === 'never' && !u.LastLoginDate)
    const matchesGroup = groupFilter.value === 'all'
      || (groupFilter.value === 'admin' && policy.IsAdministrator)
      || (groupFilter.value === 'user' && !policy.IsAdministrator)
      || (groupFilter.value === 'restricted' && !policy.EnableAllFolders)
    return matchesSearch && matchesStatus && matchesGroup
  })
})

// selectableVisibleIds = 当前筛选条件下「全部页」可选用户（用于跨页全选 + 修剪失效选择）
const selectableVisibleIds = computed(() => visibleUsers.value.filter(u => u.Id !== auth.userId).map(u => u.Id))
const selectedUsers = computed(() => users.value.filter(u => selectedUserIds.value.includes(u.Id)))
const selectedCount = computed(() => selectedUsers.value.length)
const allFilteredSelected = computed(() => selectableVisibleIds.value.length > 0 && selectableVisibleIds.value.every(id => selectedUserIds.value.includes(id)))

const totalFiltered = computed(() => visibleUsers.value.length)
const pagedUsers = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return visibleUsers.value.slice(start, start + pageSize.value)
})
// 当前页可选 id（排除自己）
const pageSelectableIds = computed(() => pagedUsers.value.filter(u => u.Id !== auth.userId).map(u => u.Id))
const allPageSelected = computed(() => pageSelectableIds.value.length > 0 && pageSelectableIds.value.every(id => selectedUserIds.value.includes(id)))
const pageIndeterminate = computed(() => {
  const sel = pageSelectableIds.value.filter(id => selectedUserIds.value.includes(id)).length
  return sel > 0 && sel < pageSelectableIds.value.length
})

// 筛选/搜索变化时回到第 1 页；当前页超出总页数时收敛。
watch([searchTerm, statusFilter, groupFilter], () => { currentPage.value = 1 })
watch([totalFiltered, pageSize], () => {
  const maxPage = Math.max(1, Math.ceil(totalFiltered.value / pageSize.value))
  if (currentPage.value > maxPage) currentPage.value = maxPage
})

function loadUsers() {
  loading.value = true
  getAllUsers()
    .then((data) => { users.value = data })
    .catch(() => {})
    .finally(() => { loading.value = false })
}

function loadLibraries() {
  getLibraries()
    .then((data) => { libraries.value = data || [] })
    .catch(() => {})
}

async function ensureLibraries() {
  if (libraries.value.length > 0) return
  try {
    libraries.value = await getLibraries()
  } catch {
    libraries.value = []
  }
}

onMounted(() => {
  loadUsers()
  loadLibraries()
})

watch(visibleUsers, () => {
  const ids = new Set(selectableVisibleIds.value)
  selectedUserIds.value = selectedUserIds.value.filter(id => ids.has(id))
})

async function handleCreate() {
  if (!newName.value.trim()) { createError.value = '用户名不能为空'; return }
  creating.value = true
  createError.value = ''
  try {
    const created = await createNewUser(newName.value.trim(), newPassword.value)
    const patch = templatePatch(newTemplate.value)
    if (patch && created?.Id) await updateUserPolicy(created.Id, patch)
    showCreate.value = false
    newName.value = ''
    newPassword.value = ''
    newTemplate.value = 'standard'
    loadUsers()
  } catch {
    createError.value = '创建用户失败，用户名可能已存在。'
  } finally {
    creating.value = false
  }
}

function avatarColor(user: any): string {
  if (user.Policy?.IsDisabled) return 'var(--c-slate-600)'
  if (user.Policy?.IsAdministrator) return 'var(--app-primary)'
  const colors = ['#6366f1', '#8b5cf6', '#ec4899', '#f97316', '#14b8a6', '#06b6d4', '#3b82f6']
  const hash = (user.Name || '').split('').reduce((a: number, c: string) => a + c.charCodeAt(0), 0)
  return colors[hash % colors.length]
}

function editAvatarColor(): string {
  if (editPolicy.value?.IsDisabled) return 'var(--c-slate-600)'
  if (editPolicy.value?.IsAdministrator) return 'var(--app-primary)'
  const colors = ['#6366f1', '#8b5cf6', '#ec4899', '#f97316', '#14b8a6', '#06b6d4', '#3b82f6']
  const hash = (editUsername.value || '').split('').reduce((a: number, c: string) => a + c.charCodeAt(0), 0)
  return colors[hash % colors.length]
}

function openEdit(userId: string) {
  editUserId.value = userId
  editLoading.value = true
  editUser.value = null
  editPolicy.value = null
  editCurrentPw.value = ''
  editNewPw.value = ''
  editConfirmPw.value = ''

  Promise.all([getUserDetail(userId), getLibraries()])
    .then(([userData, libs]) => {
      editUser.value = userData
      editUsername.value = userData.Name
      editPolicy.value = normalizePolicy(userData.Policy)
      editLibraries.value = libs
      libraries.value = libs
      syncFolderChecks(editPolicy.value, libs, editFolderChecks)
    })
    .catch(() => { showToast('加载用户详情失败', 'error'); editUserId.value = null })
    .finally(() => { editLoading.value = false })
}

function closeEdit() {
  editUserId.value = null
  editUser.value = null
  editPolicy.value = null
}

function normalizePolicy(policy: any): PolicyState {
  return {
    ...policy,
    BlockedMediaFolders: policy?.BlockedMediaFolders || [],
    EnabledFolders: policy?.EnabledFolders || [],
  }
}

function syncFolderChecks(policy: PolicyState | null, libs: any[], target: Record<string, boolean>) {
  for (const key of Object.keys(target)) delete target[key]
  const enabled = new Set(policy?.EnabledFolders || [])
  for (const lib of libs) {
    const id = String(lib.ItemId || lib.Id || '')
    if (!id) continue
    target[id] = policy?.EnableAllFolders ? true : enabled.has(id)
  }
}

function togglePolicy(key: keyof PolicyState) {
  if (!editPolicy.value) return
  const cur = editPolicy.value[key]
  if (typeof cur === 'boolean') editPolicy.value = { ...editPolicy.value, [key]: !cur }
}

// 置灰开关的 tooltip 文案;effective 项返回空(不显示提示)。
function toggleHint(row: ToggleDef): string | undefined {
  if (row.effective) return undefined
  return row.disabledHint || POLICY_UNSUPPORTED_HINT
}

function buildPolicyPayload(policy: PolicyState, folderChecks: Record<string, boolean>) {
  const enabledFolders = policy.EnableAllFolders
    ? []
    : Object.entries(folderChecks).filter(([, checked]) => checked).map(([id]) => id)
  return {
    ...policy,
    BlockedMediaFolders: policy.BlockedMediaFolders || [],
    EnabledFolders: enabledFolders,
  }
}

async function handleSaveProfile() {
  if (!editUserId.value || !editPolicy.value) return
  if (!editUsername.value.trim()) { showToast('用户名不能为空', 'error'); return }
  editSaving.value = true
  try {
    await updateUserInfo(editUserId.value, { Name: editUsername.value.trim() })
    await updateUserPolicy(editUserId.value, buildPolicyPayload(editPolicy.value, editFolderChecks))
    showToast('用户设置已保存', 'success')
    loadUsers()
    const updated = await getUserDetail(editUserId.value)
    editUser.value = updated
    editPolicy.value = normalizePolicy(updated.Policy)
    syncFolderChecks(editPolicy.value, editLibraries.value, editFolderChecks)
  } catch {
    showToast('保存设置失败', 'error')
  } finally {
    editSaving.value = false
  }
}

async function handleChangePassword(e: Event) {
  e.preventDefault()
  if (editNewPw.value !== editConfirmPw.value) { showToast('两次输入的密码不一致', 'error'); return }
  if (!editUserId.value) return
  try {
    await changeUserPassword(editUserId.value, editCurrentPw.value, editNewPw.value)
    showToast('密码已修改', 'success')
    editCurrentPw.value = ''; editNewPw.value = ''; editConfirmPw.value = ''
  } catch {
    showToast('修改密码失败', 'error')
  }
}

async function handleDelete() {
  if (!editUserId.value) return
  try {
    await deleteUserById(editUserId.value)
    showToast('用户已删除', 'success')
    closeEdit()
    loadUsers()
  } catch {
    showToast('删除用户失败', 'error')
  }
}

function toggleUserSelection(userId: string, checked: boolean) {
  if (userId === auth.userId) return
  selectedUserIds.value = checked
    ? Array.from(new Set([...selectedUserIds.value, userId]))
    : selectedUserIds.value.filter(id => id !== userId)
}

// 仅勾选/取消当前页
function toggleAllPage(checked: boolean) {
  if (checked) selectedUserIds.value = Array.from(new Set([...selectedUserIds.value, ...pageSelectableIds.value]))
  else selectedUserIds.value = selectedUserIds.value.filter(id => !pageSelectableIds.value.includes(id))
}

// 跨页选择全部筛选结果
function selectAllFiltered() {
  selectedUserIds.value = Array.from(new Set([...selectedUserIds.value, ...selectableVisibleIds.value]))
}

function clearSelection() {
  selectedUserIds.value = []
}

async function applyBulkPolicy(patch: Record<string, any>, successText: string) {
  if (selectedCount.value === 0) return false
  bulkSaving.value = true
  try {
    await Promise.all(selectedUserIds.value.map(id => updateUserPolicy(id, patch)))
    showToast(successText, 'success')
    selectedUserIds.value = []
    loadUsers()
    return true
  } catch {
    showToast('批量操作失败', 'error')
    return false
  } finally {
    bulkSaving.value = false
  }
}

async function openBulkLibraryAccess() {
  await ensureLibraries()
  bulkEnableAllFolders.value = true
  for (const key of Object.keys(bulkFolderChecks)) delete bulkFolderChecks[key]
  for (const lib of libraries.value) {
    const id = String(lib.ItemId || lib.Id || '')
    if (id) bulkFolderChecks[id] = true
  }
  showBulkLibraryAccess.value = true
}

async function applyBulkLibraryAccess() {
  const enabledFolders = bulkEnableAllFolders.value
    ? []
    : Object.entries(bulkFolderChecks).filter(([, checked]) => checked).map(([id]) => id)
  const ok = await applyBulkPolicy({ EnableAllFolders: bulkEnableAllFolders.value, EnabledFolders: enabledFolders }, '媒体库访问已批量更新')
  if (ok) showBulkLibraryAccess.value = false
}

async function applyBulkPolicyEdit(patch: Record<string, any>) {
  const ok = await applyBulkPolicy(patch, '权限策略已批量更新')
  if (ok) showBulkPolicy.value = false
}

async function handleBulkDelete() {
  if (selectedCount.value === 0) return
  bulkSaving.value = true
  try {
    await Promise.all(selectedUserIds.value.map(id => deleteUserById(id)))
    showToast('选中用户已删除', 'success')
    selectedUserIds.value = []
    showBulkDeleteConfirm.value = false
    loadUsers()
  } catch {
    showToast('批量删除失败', 'error')
  } finally {
    bulkSaving.value = false
  }
}

function accessSummary(user: any) {
  const policy = user.Policy || {}
  if (policy.EnableAllFolders) return '全部媒体库'
  const ids = policy.EnabledFolders || []
  if (ids.length === 0) return '未授权'
  if (ids.length <= 2) return ids.map((id: string) => libraryNameMap.value[id] || id).join('、')
  return `${ids.slice(0, 2).map((id: string) => libraryNameMap.value[id] || id).join('、')} 等 ${ids.length} 个`
}

const isSelf = computed(() => auth.userId === editUserId.value)
</script>

<template>
  <page-shell title="用户管理" :icon="AppIcons.users" :description="`共 ${users.length} 个用户`">
    <template #actions>
      <n-button type="primary" @click="showCreate = true">
        <template #icon><n-icon><PersonAddOutline /></n-icon></template>
        添加用户
      </n-button>
    </template>

    <user-management-toolbar
      v-model:search-term="searchTerm"
      v-model:status-filter="statusFilter"
      v-model:group-filter="groupFilter"
      v-model:view-mode="viewMode"
      :status-options="statusOptions"
      :group-options="groupOptions"
      :menu-props="solidModalMenuProps"
    />

    <user-bulk-bar
      :selected-count="selectedCount"
      :loading="bulkSaving"
      @enable="applyBulkPolicy({ IsDisabled: false }, '选中用户已启用')"
      @disable="applyBulkPolicy({ IsDisabled: true }, '选中用户已禁用')"
      @show="applyBulkPolicy({ IsHidden: false }, '选中用户已显示')"
      @hide="applyBulkPolicy({ IsHidden: true }, '选中用户已隐藏')"
      @library="openBulkLibraryAccess"
      @policy="showBulkPolicy = true"
      @delete="showBulkDeleteConfirm = true"
    />

    <!-- Create User Modal -->
    <n-modal v-model:show="showCreate" preset="card" title="添加用户" :style="[forceSolidModalStyle, { maxWidth: '440px' }]" class="glass-modal solid-modal-card force-solid-modal">
      <n-space vertical :size="16">
        <div>
          <label class="form-label">用户名</label>
          <n-input v-model:value="newName" autofocus @keydown.enter="handleCreate" />
        </div>
        <div>
          <label class="form-label">密码</label>
          <n-input v-model:value="newPassword" type="password" show-password-on="click" placeholder="留空将自动生成临时密码" />
        </div>
        <div>
          <label class="form-label">权限模板</label>
          <n-select v-model:value="newTemplate" :options="permissionTemplates" :menu-props="solidModalMenuProps" />
        </div>
        <div v-if="createError" style="color: var(--app-error); font-size: 13px">{{ createError }}</div>
      </n-space>
      <template #action>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" :loading="creating" @click="handleCreate">创建</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Loading -->
    <div v-if="loading" style="padding: 60px; text-align: center">
      <n-spin size="medium" />
    </div>

    <template v-else>
      <n-empty v-if="visibleUsers.length === 0" description="没有匹配的用户" style="padding: 40px 0" />

      <template v-else>
        <user-selection-bar
          :filtered-count="totalFiltered"
          :page-selectable-count="pageSelectableIds.length"
          :selected-count="selectedCount"
          :all-page-selected="allPageSelected"
          :page-indeterminate="pageIndeterminate"
          :all-filtered-selected="allFilteredSelected"
          @toggle-page="toggleAllPage"
          @select-all-filtered="selectAllFiltered"
          @clear="clearSelection"
        />

        <user-management-list
          :users="pagedUsers"
          :view-mode="viewMode"
          :selected-user-ids="selectedUserIds"
          :auth-user-id="auth.userId"
          :all-visible-selected="allPageSelected"
          :selectable-count="pageSelectableIds.length"
          :avatar-color="avatarColor"
          :access-summary="accessSummary"
          @open-edit="openEdit"
          @toggle-selection="toggleUserSelection"
          @toggle-all="toggleAllPage"
        />

        <div v-if="totalFiltered > pageSize" class="pagination-row">
          <n-pagination
            v-model:page="currentPage"
            v-model:page-size="pageSize"
            :item-count="totalFiltered"
            :page-sizes="pageSizeOptions"
            show-size-picker
          />
        </div>
      </template>
    </template>

    <!-- Edit User Modal -->
    <n-modal
      :show="showEditModal"
      @update:show="(v: boolean) => { if (!v) closeEdit() }"
      preset="card"
      class="glass-modal force-solid-modal"
      :style="[forceSolidModalStyle, { width: '640px', maxWidth: '94vw' }]"
      :title="editUser ? `编辑 · ${editUser.Name}` : '加载中...'"
    >
      <div v-if="editLoading" style="padding: 40px; text-align: center">
        <n-spin size="medium" />
      </div>

      <n-scrollbar v-else-if="editUser && editPolicy" style="max-height: 70vh">
        <div class="edit-inner">
          <!-- Profile banner -->
          <div class="profile-banner">
            <div class="profile-avatar" :style="{ background: editAvatarColor() }">
              {{ editUsername?.[0]?.toUpperCase() || '?' }}
            </div>
            <div class="profile-meta">
              <n-input v-model:value="editUsername" style="max-width: 280px; font-size: 16px" />
              <div class="profile-status">
                <n-tag v-if="editPolicy.IsAdministrator" size="small" :bordered="false" round type="success">管理员</n-tag>
                <n-tag v-if="editPolicy.IsDisabled" size="small" :bordered="false" round type="error">已禁用</n-tag>
                <n-tag v-if="isSelf" size="small" :bordered="false" round type="info">当前用户</n-tag>
                <span v-if="editUser.LastLoginDate" class="login-time">
                  上次登录 {{ new Date(editUser.LastLoginDate).toLocaleString() }}
                </span>
              </div>
            </div>
          </div>

          <!-- Account & Security -->
          <div class="section-card">
            <h3 class="section-title">
              <n-icon :size="16"><ShieldCheckmarkOutline /></n-icon>
              账户与安全
            </h3>
            <div v-for="row in adminToggles" :key="row.key" class="toggle-row" :class="{ 'toggle-row--disabled': !row.effective }" :title="toggleHint(row)">
              <div class="toggle-label">
                <span>{{ row.label }}</span>
                <span v-if="row.desc" class="toggle-desc">{{ row.desc }}</span>
                <span v-else-if="!row.effective" class="toggle-desc">{{ toggleHint(row) }}</span>
              </div>
              <n-switch :value="!!editPolicy[row.key]" :disabled="!row.effective" @update:value="togglePolicy(row.key)" />
            </div>
          </div>

          <!-- Password -->
          <div class="section-card">
            <h3 class="section-title">修改密码</h3>
            <form class="pw-form" @submit="handleChangePassword">
              <n-input v-if="!auth.isAdmin" v-model:value="editCurrentPw" type="password" show-password-on="click" placeholder="当前密码" style="margin-bottom: 10px" />
              <n-input v-model:value="editNewPw" type="password" show-password-on="click" placeholder="新密码" style="margin-bottom: 10px" />
              <n-input v-model:value="editConfirmPw" type="password" show-password-on="click" placeholder="确认新密码" style="margin-bottom: 14px" />
              <n-button secondary attr-type="submit" size="small">修改密码</n-button>
            </form>
          </div>

          <!-- Library Access -->
          <div class="section-card">
            <h3 class="section-title">媒体库访问</h3>
            <div class="toggle-row">
              <span class="toggle-label">允许访问所有媒体库</span>
              <n-switch :value="editPolicy.EnableAllFolders" @update:value="togglePolicy('EnableAllFolders')" />
            </div>
            <div v-if="!editPolicy.EnableAllFolders && editLibraries.length > 0" class="folder-list">
              <div v-for="lib in editLibraries" :key="lib.ItemId" class="folder-item">
                <n-checkbox v-model:checked="editFolderChecks[lib.ItemId]">
                  {{ lib.Name }}
                  <span class="folder-type">{{ lib.CollectionType === 'movies' ? '电影' : lib.CollectionType === 'tvshows' ? '电视剧' : lib.CollectionType === 'mixed' ? '混合' : lib.CollectionType }}</span>
                </n-checkbox>
              </div>
            </div>
          </div>

          <!-- Playback -->
          <div class="section-card">
            <h3 class="section-title">播放权限</h3>
            <div v-for="row in playbackToggles" :key="row.key" class="toggle-row" :class="{ 'toggle-row--disabled': !row.effective }" :title="toggleHint(row)">
              <div class="toggle-label">
                <span>{{ row.label }}</span>
                <span v-if="!row.effective" class="toggle-desc">{{ toggleHint(row) }}</span>
              </div>
              <n-switch :value="!!editPolicy[row.key]" :disabled="!row.effective" @update:value="togglePolicy(row.key)" />
            </div>
            <div class="toggle-row">
              <div class="toggle-label">
                <span>最大同时播放数</span>
                <span class="toggle-desc">0 表示不限制</span>
              </div>
              <n-select v-model:value="editPolicy.SimultaneousStreamLimit" :options="streamLimitOptions" size="small" style="width: 100px" :menu-props="solidModalMenuProps" />
            </div>
          </div>

          <!-- Features -->
          <div class="section-card">
            <h3 class="section-title">功能权限</h3>
            <div v-for="row in featureToggles" :key="row.key" class="toggle-row" :class="{ 'toggle-row--disabled': !row.effective }" :title="toggleHint(row)">
              <div class="toggle-label">
                <span>{{ row.label }}</span>
                <span v-if="!row.effective" class="toggle-desc">{{ toggleHint(row) }}</span>
              </div>
              <n-switch :value="!!editPolicy[row.key]" :disabled="!row.effective" @update:value="togglePolicy(row.key)" />
            </div>
          </div>

          <!-- Remote -->
          <div class="section-card">
            <h3 class="section-title">远程访问</h3>
            <div v-for="row in remoteToggles" :key="row.key" class="toggle-row" :class="{ 'toggle-row--disabled': !row.effective }" :title="toggleHint(row)">
              <div class="toggle-label">
                <span>{{ row.label }}</span>
                <span v-if="!row.effective" class="toggle-desc">{{ toggleHint(row) }}</span>
              </div>
              <n-switch :value="!!editPolicy[row.key]" :disabled="!row.effective" @update:value="togglePolicy(row.key)" />
            </div>
          </div>
        </div>
      </n-scrollbar>

      <template #action>
        <div class="modal-actions">
          <n-button v-if="editUser && !isSelf" type="error" ghost size="small" @click="showDeleteConfirm = true">
            <template #icon><n-icon><TrashOutline /></n-icon></template>
            删除
          </n-button>
          <div style="flex: 1" />
          <n-button @click="closeEdit">取消</n-button>
          <n-button type="primary" :loading="editSaving" @click="handleSaveProfile">保存</n-button>
        </div>
      </template>
    </n-modal>

    <!-- Delete Confirm -->
    <n-modal v-model:show="showDeleteConfirm" preset="dialog" type="error" title="删除用户" positive-text="删除" negative-text="取消" @positive-click="handleDelete">
      <p style="color: var(--app-text-muted); font-size: 14px">
        确定要删除用户 <strong style="color: var(--app-text)">{{ editUser?.Name }}</strong> 吗？此操作不可撤销。
      </p>
    </n-modal>

    <n-modal v-model:show="showBulkLibraryAccess" preset="card" title="批量设置媒体库访问" :style="[forceSolidModalStyle, { maxWidth: '520px' }]" class="glass-modal force-solid-modal">
      <n-space vertical :size="14">
        <div class="toggle-row">
          <span class="toggle-label">允许访问所有媒体库</span>
          <n-switch v-model:value="bulkEnableAllFolders" />
        </div>
        <div v-if="!bulkEnableAllFolders" class="folder-list">
          <div v-for="lib in libraries" :key="lib.ItemId || lib.Id" class="folder-item">
            <n-checkbox v-model:checked="bulkFolderChecks[lib.ItemId || lib.Id]">
              {{ lib.Name }}
              <span class="folder-type">{{ lib.CollectionType === 'movies' ? '电影' : lib.CollectionType === 'tvshows' ? '电视剧' : lib.CollectionType === 'mixed' ? '混合' : lib.CollectionType }}</span>
            </n-checkbox>
          </div>
        </div>
      </n-space>
      <template #action>
        <n-space justify="end">
          <n-button @click="showBulkLibraryAccess = false">取消</n-button>
          <n-button type="primary" :loading="bulkSaving" @click="applyBulkLibraryAccess">应用到选中用户</n-button>
        </n-space>
      </template>
    </n-modal>

    <user-bulk-policy-modal
      v-model:show="showBulkPolicy"
      :selected-count="selectedCount"
      :loading="bulkSaving"
      :modal-style="forceSolidModalStyle"
      :menu-props="solidModalMenuProps"
      @apply="applyBulkPolicyEdit"
    />

    <n-modal v-model:show="showBulkDeleteConfirm" preset="dialog" type="error" title="批量删除用户" positive-text="删除" negative-text="取消" @positive-click="handleBulkDelete">
      <p style="color: var(--app-text-muted); font-size: 14px">
        确定要删除选中的 <strong style="color: var(--app-text)">{{ selectedCount }}</strong> 个用户吗？当前用户不会出现在批量选择中。
      </p>
    </n-modal>
  </page-shell>
</template>

<style scoped>
.form-label {
  display: block; font-size: 12px; color: var(--app-text-muted);
  margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; font-weight: 500;
}

/* Edit modal inner */
.edit-inner {
  padding: 0 4px 4px;
}

.profile-banner {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: var(--app-modal-panel-bg, var(--app-surface-1));
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  margin-bottom: 16px;
}

.profile-avatar {
  width: 56px; height: 56px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  font-size: 22px; font-weight: 600; color: #fff; flex-shrink: 0;
}

.profile-meta {
  flex: 1; display: flex; flex-direction: column; gap: 8px;
}

.profile-status {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
}

.login-time {
  font-size: 12px; color: var(--app-text-muted);
}

.section-card {
  background: var(--app-modal-panel-bg, var(--app-surface-1));
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  padding: 16px 20px;
  margin-bottom: 12px;
}

.section-title {
  font-size: 13px; font-weight: 600; color: var(--app-text);
  margin: 0 0 12px; padding-bottom: 10px;
  border-bottom: 1px solid var(--app-border);
  display: flex; align-items: center; gap: 8px;
}

.toggle-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 0; min-height: 36px;
}
.toggle-row + .toggle-row {
  border-top: 1px solid rgba(128,128,128,0.08);
}

.toggle-label {
  font-size: 13px; color: var(--app-text);
  display: flex; flex-direction: column; gap: 2px;
}
.toggle-row--disabled .toggle-label { opacity: 0.5; }
.toggle-desc {
  font-size: 11px; color: var(--app-text-muted); font-weight: 400;
}

.pw-form { max-width: 320px; }

.folder-list {
  margin-top: 10px; padding: 10px 14px;
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.04)); border-radius: 8px;
}
.folder-item { padding: 3px 0; }
.folder-type { font-size: 12px; color: var(--app-text-muted); margin-left: 4px; }

.modal-actions {
  display: flex; align-items: center; gap: 8px; width: 100%;
}

.pagination-row {
  display: flex;
  justify-content: center;
  margin-top: 18px;
}

</style>
