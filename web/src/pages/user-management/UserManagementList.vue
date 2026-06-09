<script setup lang="ts">
import { NButton, NCheckbox, NTag } from 'naive-ui'

defineProps<{
  users: any[]
  viewMode: 'card' | 'table'
  selectedUserIds: string[]
  authUserId?: string | null
  allVisibleSelected: boolean
  selectableCount: number
  avatarColor: (user: any) => string
  accessSummary: (user: any) => string
}>()

const emit = defineEmits<{
  (e: 'open-edit', userId: string): void
  (e: 'toggle-selection', userId: string, checked: boolean): void
  (e: 'toggle-all', checked: boolean): void
}>()
</script>

<template>
  <div v-if="viewMode === 'card'" class="user-grid">
    <div
      v-for="user in users"
      :key="user.Id"
      class="user-card glass-card interactive"
      :class="{ selected: selectedUserIds.includes(user.Id) }"
      @click="emit('open-edit', user.Id)"
    >
      <n-checkbox
        v-if="user.Id !== authUserId"
        class="card-check"
        :checked="selectedUserIds.includes(user.Id)"
        @click.stop
        @update:checked="(checked: boolean) => emit('toggle-selection', user.Id, checked)"
      />
      <div class="user-avatar" :style="{ background: avatarColor(user), opacity: user.Policy?.IsDisabled ? 0.45 : 1 }">
        {{ user.Name?.[0]?.toUpperCase() || '?' }}
      </div>
      <div class="user-name" :style="{ opacity: user.Policy?.IsDisabled ? 0.5 : 1 }">{{ user.Name }}</div>
      <div class="user-tags">
        <n-tag v-if="user.Policy?.IsAdministrator" size="tiny" :bordered="false" round type="success">管理员</n-tag>
        <n-tag v-if="user.Policy?.IsDisabled" size="tiny" :bordered="false" round type="error">已禁用</n-tag>
        <n-tag v-if="user.Policy?.IsHidden" size="tiny" :bordered="false" round type="warning">已隐藏</n-tag>
      </div>
      <div class="user-login">
        {{ user.LastLoginDate ? new Date(user.LastLoginDate).toLocaleDateString() : '从未登录' }}
      </div>
      <div class="user-access">{{ accessSummary(user) }}</div>
    </div>
  </div>

  <div v-else class="table-wrap">
    <table class="user-table">
      <thead>
        <tr>
          <th class="check-col">
            <n-checkbox :checked="allVisibleSelected" :disabled="selectableCount === 0" @update:checked="emit('toggle-all', $event)" />
          </th>
          <th>用户</th>
          <th>状态</th>
          <th>媒体库访问</th>
          <th>播放限制</th>
          <th>远程访问</th>
          <th>上次登录</th>
          <th class="action-col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="user in users"
          :key="user.Id"
          :class="{ disabled: user.Policy?.IsDisabled, selected: selectedUserIds.includes(user.Id) }"
          @click="emit('open-edit', user.Id)"
        >
          <td class="check-col" @click.stop>
            <n-checkbox
              v-if="user.Id !== authUserId"
              :checked="selectedUserIds.includes(user.Id)"
              @update:checked="(checked: boolean) => emit('toggle-selection', user.Id, checked)"
            />
          </td>
          <td>
            <div class="table-user">
              <div class="table-avatar" :style="{ background: avatarColor(user) }">{{ user.Name?.[0]?.toUpperCase() || '?' }}</div>
              <div>
                <div class="table-name">{{ user.Name }}</div>
                <div class="table-sub">{{ user.Id === authUserId ? '当前用户' : user.Id }}</div>
              </div>
            </div>
          </td>
          <td>
            <div class="table-tags">
              <n-tag v-if="user.Policy?.IsAdministrator" size="tiny" :bordered="false" round type="success">管理员</n-tag>
              <n-tag v-if="user.Policy?.IsDisabled" size="tiny" :bordered="false" round type="error">禁用</n-tag>
              <n-tag v-if="user.Policy?.IsHidden" size="tiny" :bordered="false" round type="warning">隐藏</n-tag>
              <n-tag v-if="!user.Policy?.IsAdministrator && !user.Policy?.IsDisabled && !user.Policy?.IsHidden" size="tiny" :bordered="false" round>正常</n-tag>
            </div>
          </td>
          <td>{{ accessSummary(user) }}</td>
          <td>{{ user.Policy?.SimultaneousStreamLimit ? `${user.Policy.SimultaneousStreamLimit} 路` : '不限制' }}</td>
          <td>{{ user.Policy?.EnableRemoteAccess ? '允许' : '禁止' }}</td>
          <td>{{ user.LastLoginDate ? new Date(user.LastLoginDate).toLocaleString() : '从未登录' }}</td>
          <td class="action-col" @click.stop>
            <n-button size="tiny" secondary @click="emit('open-edit', user.Id)">编辑</n-button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.user-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px;
}

.user-card {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 24px 16px 20px;
  cursor: pointer;
  text-align: center;
}

.user-card.selected {
  border-color: var(--app-primary);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--app-primary) 42%, transparent);
}

.card-check {
  position: absolute;
  top: 10px;
  left: 10px;
}

.user-avatar {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  font-weight: 600;
  color: #fff;
  flex-shrink: 0;
  letter-spacing: 0.5px;
}

.user-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

.user-tags {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
  justify-content: center;
  min-height: 20px;
}

.user-login,
.user-access {
  max-width: 100%;
  font-size: 11px;
  color: var(--app-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.table-wrap {
  overflow: hidden;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
}

.user-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}

.user-table th,
.user-table td {
  padding: 12px 14px;
  border-bottom: 1px solid var(--app-border);
  font-size: 13px;
  color: var(--app-text);
  text-align: left;
  vertical-align: middle;
}

.user-table th {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text-muted);
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.04));
}

.user-table tbody tr {
  cursor: pointer;
  transition: background 0.15s ease;
}

.user-table tbody tr:hover,
.user-table tbody tr.selected {
  background: color-mix(in srgb, var(--app-primary) 8%, transparent);
}

.user-table tbody tr.disabled {
  opacity: 0.58;
}

.user-table tbody tr:last-child td {
  border-bottom: 0;
}

.check-col {
  width: 44px;
}

.action-col {
  width: 86px;
  text-align: right;
}

.table-user {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.table-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: #fff;
  font-size: 13px;
  font-weight: 700;
}

.table-name,
.table-sub {
  max-width: 190px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.table-name {
  font-weight: 600;
}

.table-sub {
  margin-top: 2px;
  font-size: 11px;
  color: var(--app-text-muted);
}

.table-tags {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

@media (max-width: 640px) {
  .user-grid {
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 8px;
  }

  .user-card {
    padding: 20px 12px 16px;
  }

  .user-avatar {
    width: 44px;
    height: 44px;
    font-size: 18px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .user-table {
    min-width: 780px;
  }
}
</style>
