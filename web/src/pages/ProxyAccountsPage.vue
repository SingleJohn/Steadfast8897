<script setup lang="ts">
import { h, onMounted, ref, reactive } from 'vue'
import { NButton, NDataTable, NModal, NInput, NSelect, NTag, NSwitch, NSpace, NPopconfirm, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { listProxyAccounts, createProxyAccount, updateProxyAccount, deleteProxyAccount } from '../api/client'

const message = useMessage()
const loading = ref(false)
const accounts = ref<any[]>([])
const showModal = ref(false)
const editing = ref<any>(null)

const form = reactive({
  alias: '',
  type: '115_open',
  enabled: true,
  // 115_open
  access_token: '',
  refresh_token: '',
  root_folder_id: '0',
  // pan123
  client_id: '',
  client_secret: '',
})

const typeOptions = [
  { label: '115 网盘 (Open API)', value: '115_open' },
  { label: '123 云盘', value: 'pan123' },
]

async function load() {
  loading.value = true
  try {
    accounts.value = await listProxyAccounts() || []
  } catch (e: any) {
    message.error('加载失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  form.alias = ''
  form.type = '115_open'
  form.enabled = true
  form.access_token = ''
  form.refresh_token = ''
  form.root_folder_id = '0'
  form.client_id = ''
  form.client_secret = ''
  showModal.value = true
}

function openEdit(row: any) {
  editing.value = row
  form.alias = row.alias
  form.type = row.type
  form.enabled = row.enabled
  const cfg = row.config || {}
  form.access_token = cfg.access_token || ''
  form.refresh_token = cfg.refresh_token || ''
  form.root_folder_id = cfg.root_folder_id || '0'
  form.client_id = cfg.client_id || ''
  form.client_secret = cfg.client_secret || ''
  showModal.value = true
}

async function save() {
  if (!form.alias.trim()) {
    message.error('请填写账号别名')
    return
  }
  const config: Record<string, any> = {}
  if (form.type === '115_open') {
    if (!form.access_token || !form.refresh_token) {
      message.error('请填写 access_token 和 refresh_token')
      return
    }
    config.access_token = form.access_token
    config.refresh_token = form.refresh_token
    config.root_folder_id = form.root_folder_id || '0'
  } else {
    if (!form.client_id || !form.client_secret) {
      message.error('请填写 client_id 和 client_secret')
      return
    }
    config.client_id = form.client_id
    config.client_secret = form.client_secret
  }
  try {
    if (editing.value) {
      await updateProxyAccount(editing.value.id, form.alias, form.type, config, form.enabled)
      message.success('已更新')
    } else {
      await createProxyAccount(form.alias, form.type, config)
      message.success('已创建')
    }
    showModal.value = false
    await load()
  } catch (e: any) {
    message.error('保存失败: ' + (e?.message || e))
  }
}

async function remove(row: any) {
  try {
    await deleteProxyAccount(row.id)
    message.success('已删除')
    await load()
  } catch (e: any) {
    message.error('删除失败: ' + (e?.message || e))
  }
}

function copyLinkExample(row: any) {
  const base = window.location.origin
  const example = `${base}/d/${row.alias}/你的/文件路径.mkv`
  navigator.clipboard.writeText(example).then(() => message.success('示例链接已复制'))
}

const columns: DataTableColumns = [
  { title: '别名', key: 'alias', width: 160 },
  {
    title: '类型',
    key: 'type',
    width: 140,
    render: (row: any) => row.type === '115_open' ? '115 网盘' : '123 云盘',
  },
  {
    title: '状态',
    key: 'enabled',
    width: 100,
    render: (row: any) => h(NTag, { type: row.enabled ? 'success' : 'default', size: 'small' }, { default: () => row.enabled ? '启用' : '禁用' }),
  },
  {
    title: '操作',
    key: 'actions',
    render: (row: any) => h(NSpace, { size: 'small' }, {
      default: () => [
        h(NButton, { size: 'small', onClick: () => copyLinkExample(row) }, { default: () => '复制示例' }),
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(NPopconfirm, { onPositiveClick: () => remove(row) }, {
          trigger: () => h(NButton, { size: 'small', type: 'error' }, { default: () => '删除' }),
          default: () => `确定删除「${row.alias}」？`,
        }),
      ],
    }),
  },
]

onMounted(load)
</script>

<template>
  <div class="proxy-page">
    <div class="page-header">
      <h2>网盘直链代理</h2>
      <n-button type="primary" @click="openNew">新增账号</n-button>
    </div>

    <p class="hint">
      通过 <code>GET /d/{别名}/{文件路径}</code> 获取 302 直链。支持 <code>?json=1</code> 返回 JSON。
    </p>

    <n-data-table
      :columns="columns"
      :data="accounts"
      :loading="loading"
      :bordered="false"
      :pagination="{ pageSize: 20 }"
    />

    <n-modal v-model:show="showModal" preset="card" :title="editing ? '编辑账号' : '新增账号'" style="max-width: 560px">
      <div class="form">
        <div class="form-row">
          <label>别名</label>
          <n-input v-model:value="form.alias" placeholder="例如 my115、my123" />
        </div>
        <div class="form-row">
          <label>类型</label>
          <n-select v-model:value="form.type" :options="typeOptions" />
        </div>

        <template v-if="form.type === '115_open'">
          <div class="form-row">
            <label>Access Token</label>
            <n-input v-model:value="form.access_token" type="password" show-password-on="click" />
          </div>
          <div class="form-row">
            <label>Refresh Token</label>
            <n-input v-model:value="form.refresh_token" type="password" show-password-on="click" />
          </div>
          <div class="form-row">
            <label>根目录 ID</label>
            <n-input v-model:value="form.root_folder_id" placeholder="默认 0（根目录）" />
          </div>
        </template>

        <template v-else>
          <div class="form-row">
            <label>Client ID</label>
            <n-input v-model:value="form.client_id" />
          </div>
          <div class="form-row">
            <label>Client Secret</label>
            <n-input v-model:value="form.client_secret" type="password" show-password-on="click" />
          </div>
        </template>

        <div v-if="editing" class="form-row">
          <label>启用</label>
          <n-switch v-model:value="form.enabled" />
        </div>

        <div class="form-actions">
          <n-button @click="showModal = false">取消</n-button>
          <n-button type="primary" @click="save">保存</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<style scoped>
.proxy-page { padding: 24px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.hint { color: #888; margin-bottom: 16px; font-size: 13px; }
.hint code { background: rgba(255,255,255,0.08); padding: 2px 6px; border-radius: 3px; }
.form { display: flex; flex-direction: column; gap: 12px; }
.form-row { display: flex; flex-direction: column; gap: 6px; }
.form-row label { font-size: 13px; color: #aaa; }
.form-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
</style>
