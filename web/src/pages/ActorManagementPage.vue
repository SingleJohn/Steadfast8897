<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  NButton,
  NCheckbox,
  NEmpty,
  NPagination,
  NPopconfirm,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import PageShell from '@/components/PageShell.vue'
import { AppIcons } from '@/icons/appIcons'
import {
  backfillAllActorImages,
  bulkDeleteActors,
  deleteActor,
  deleteAllJunkActors,
  getActorImageSummary,
  getImageUrl,
  listActors,
  type ActorAdminRow,
  type ActorImageSummary,
} from '@/api/client'
import ActorEditDrawer from './actor-admin/ActorEditDrawer.vue'

const message = useMessage()
const dialog = useDialog()

const PAGE_SIZE = 50

const rows = ref<ActorAdminRow[]>([])
const total = ref(0)
const loading = ref(false)
const summary = ref<ActorImageSummary | null>(null)

const q = ref('')
const filter = ref('')
const sort = ref('works')
const order = ref('desc')
const page = ref(1)

const selected = ref<Set<string>>(new Set())

const drawerShow = ref(false)
const editingId = ref<string | null>(null)

const filterOptions = [
  { label: '全部', value: '' },
  { label: '缺头像', value: 'missing_image' },
  { label: '有头像', value: 'has_image' },
  { label: '已锁定', value: 'locked' },
  { label: '有作品', value: 'with_works' },
  { label: '垃圾名', value: 'junk' },
]
const sortOptions = [
  { label: '作品数', value: 'works' },
  { label: '姓名', value: 'name' },
  { label: '更新时间', value: 'updated' },
]

const selectedCount = computed(() => selected.value.size)
const pageAllSelected = computed(() => rows.value.length > 0 && rows.value.every((r) => selected.value.has(r.Id)))
const pageIndeterminate = computed(() => !pageAllSelected.value && rows.value.some((r) => selected.value.has(r.Id)))

async function load() {
  loading.value = true
  try {
    const res = await listActors({
      q: q.value.trim(),
      filter: filter.value,
      sort: sort.value,
      order: order.value,
      limit: PAGE_SIZE,
      offset: (page.value - 1) * PAGE_SIZE,
    })
    rows.value = res.Items || []
    total.value = res.TotalRecordCount || 0
  } catch (e: any) {
    message.error(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function loadSummary() {
  try {
    summary.value = await getActorImageSummary()
  } catch {
    /* ignore */
  }
}

onMounted(() => {
  void load()
  void loadSummary()
})

// 过滤/排序变化 → 回到第 1 页并清空选择
watch([filter, sort, order], () => {
  page.value = 1
  selected.value = new Set()
  void load()
})
watch(page, () => void load())

// 搜索防抖
let searchTimer: number | undefined
watch(q, () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    page.value = 1
    selected.value = new Set()
    void load()
  }, 350)
})

function toggleOrder() {
  order.value = order.value === 'asc' ? 'desc' : 'asc'
}

function thumb(row: ActorAdminRow): string {
  if (!row.HasImage) return ''
  return getImageUrl(row.Id, 'Primary', { maxWidth: 96, tag: row.ImageTag })
}

function toggleRow(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function togglePage(checked: boolean) {
  const next = new Set(selected.value)
  for (const r of rows.value) {
    if (checked) next.add(r.Id)
    else next.delete(r.Id)
  }
  selected.value = next
}

function openEdit(row: ActorAdminRow) {
  editingId.value = row.Id
  drawerShow.value = true
}

function onSaved() {
  void load()
  void loadSummary()
}

async function removeOne(row: ActorAdminRow) {
  try {
    await deleteActor(row.Id)
    message.success(`已删除「${row.Name}」`)
    const next = new Set(selected.value)
    next.delete(row.Id)
    selected.value = next
    void load()
    void loadSummary()
  } catch (e: any) {
    message.error(e?.message || '删除失败')
  }
}

function confirmBulkDelete() {
  const ids = [...selected.value]
  if (!ids.length) return
  dialog.warning({
    title: '批量删除演员',
    content: `确认删除选中的 ${ids.length} 个演员?将解除其与影片的关联并删除头像文件,不可撤销。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const res = await bulkDeleteActors(ids)
        message.success(`已删除 ${res.Deleted} 个`)
        selected.value = new Set()
        void load()
        void loadSummary()
      } catch (e: any) {
        message.error(e?.message || '批量删除失败')
      }
    },
  })
}

function confirmCleanupJunk() {
  dialog.warning({
    title: '清理垃圾演员',
    content: '删除所有「垃圾名」演员(HTML 实体 / 尖括号残留,如 &lt;i)。解除关联并删除,不可撤销。',
    positiveText: '清理',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const res = await deleteAllJunkActors()
        message.success(`已清理 ${res.Deleted} 个垃圾演员`)
        selected.value = new Set()
        void load()
        void loadSummary()
      } catch (e: any) {
        message.error(e?.message || '清理失败')
      }
    },
  })
}

const backfilling = ref(false)
async function runBackfill() {
  backfilling.value = true
  try {
    const res = await backfillAllActorImages()
    message.success(`已补 ${res.persons_filled} 个头像,入队 ${res.tmdb_items_queued} 个 TMDB 任务`)
    void load()
    void loadSummary()
  } catch (e: any) {
    message.error(e?.message || '批量补图失败')
  } finally {
    backfilling.value = false
  }
}
</script>

<template>
  <page-shell title="演员管理" :icon="AppIcons.users" description="人工核对与管理演员资料、头像、清理垃圾条目。">
    <!-- 统计条 -->
    <div v-if="summary" class="stat-bar">
      <span class="stat"><b>{{ summary.total }}</b> 总数</span>
      <span class="stat stat--ok"><b>{{ summary.with_image }}</b> 有头像</span>
      <span class="stat stat--warn"><b>{{ summary.missing }}</b> 缺头像</span>
      <span class="stat"><b>{{ summary.locked }}</b> 已锁定</span>
    </div>

    <!-- 工具栏 -->
    <div class="toolbar">
      <input v-model="q" class="search-input" type="text" placeholder="搜索演员名…" />
      <n-select v-model:value="filter" :options="filterOptions" size="small" class="tb-select" />
      <n-select v-model:value="sort" :options="sortOptions" size="small" class="tb-select tb-select--sort" />
      <n-button size="small" @click="toggleOrder">{{ order === 'asc' ? '升序' : '降序' }}</n-button>
      <div class="toolbar-spacer" />
      <n-button size="small" :loading="backfilling" @click="runBackfill">批量补图</n-button>
      <n-button size="small" type="warning" ghost @click="confirmCleanupJunk">清理垃圾</n-button>
      <n-button size="small" type="error" :disabled="!selectedCount" @click="confirmBulkDelete">
        删除选中{{ selectedCount ? `(${selectedCount})` : '' }}
      </n-button>
    </div>

    <!-- 表格 -->
    <n-spin :show="loading">
      <div class="actor-table">
        <div class="actor-row actor-row--head">
          <div class="col-check">
            <n-checkbox :checked="pageAllSelected" :indeterminate="pageIndeterminate" @update:checked="togglePage" />
          </div>
          <div class="col-avatar" />
          <div class="col-name">姓名</div>
          <div class="col-works">作品</div>
          <div class="col-status">状态</div>
          <div class="col-ops">操作</div>
        </div>

        <n-empty v-if="!loading && rows.length === 0" description="没有匹配的演员" class="table-empty" />

        <div v-for="row in rows" :key="row.Id" class="actor-row" :class="{ 'is-selected': selected.has(row.Id) }">
          <div class="col-check">
            <n-checkbox :checked="selected.has(row.Id)" @update:checked="() => toggleRow(row.Id)" />
          </div>
          <div class="col-avatar">
            <div class="avatar" @click="openEdit(row)">
              <img v-if="thumb(row)" :src="thumb(row)" alt="" />
              <svg v-else width="20" height="20" viewBox="0 0 24 24" fill="currentColor" opacity="0.3"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>
            </div>
          </div>
          <div class="col-name">
            <button class="name-btn" @click="openEdit(row)">{{ row.Name }}</button>
          </div>
          <div class="col-works">{{ row.WorkCount }}</div>
          <div class="col-status">
            <n-tag v-if="row.IsJunk" size="tiny" type="error" :bordered="false">垃圾</n-tag>
            <n-tag v-if="row.HasImage" size="tiny" type="success" :bordered="false">图</n-tag>
            <n-tag v-if="row.ImageLocked" size="tiny" :bordered="false">锁</n-tag>
            <n-tag v-if="row.HasBackdrop" size="tiny" :bordered="false">背</n-tag>
            <n-tag v-if="row.HasOverview" size="tiny" :bordered="false">简介</n-tag>
            <n-tag v-if="row.ProviderCount > 0" size="tiny" type="info" :bordered="false">{{ row.ProviderCount }}链接</n-tag>
          </div>
          <div class="col-ops">
            <n-button size="tiny" @click="openEdit(row)">编辑</n-button>
            <n-popconfirm @positive-click="() => removeOne(row)">
              <template #trigger>
                <n-button size="tiny" quaternary type="error">删除</n-button>
              </template>
              删除「{{ row.Name }}」?将解除关联并删头像。
            </n-popconfirm>
          </div>
        </div>
      </div>
    </n-spin>

    <div class="pager">
      <n-pagination
        v-model:page="page"
        :page-count="Math.max(1, Math.ceil(total / PAGE_SIZE))"
        :page-slot="7"
      />
      <span class="pager-total">共 {{ total }} 人</span>
    </div>

    <actor-edit-drawer v-model:show="drawerShow" :actor-id="editingId" @saved="onSaved" />
  </page-shell>
</template>

<style scoped>
.stat-bar {
  display: flex;
  gap: 18px;
  padding: 10px 14px;
  margin-bottom: 12px;
  border-radius: 10px;
  background: var(--app-surface-2, rgba(128, 128, 128, 0.08));
  font-size: 13px;
}
.stat b { font-size: 15px; }
.stat--ok b { color: #18a058; }
.stat--warn b { color: #f0a020; }

.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.search-input {
  width: 220px;
  height: 30px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--app-border, rgba(128, 128, 128, 0.3));
  background: transparent;
  color: var(--app-text, inherit);
  font-size: 13px;
  outline: none;
}
.search-input:focus { border-color: var(--app-primary, #4098fc); }
.tb-select { width: 120px; }
.tb-select--sort { width: 110px; }
.toolbar-spacer { flex: 1; }

.actor-table {
  border: 1px solid var(--app-border, rgba(128, 128, 128, 0.18));
  border-radius: 12px;
  overflow: hidden;
}
.actor-row {
  display: grid;
  grid-template-columns: 44px 56px 1fr 70px minmax(160px, 2fr) 150px;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border-top: 1px solid var(--app-border, rgba(128, 128, 128, 0.12));
}
.actor-row:first-child { border-top: 0; }
.actor-row--head {
  font-size: 12px;
  font-weight: 700;
  color: var(--app-text-muted, #888);
  background: var(--app-surface-2, rgba(128, 128, 128, 0.06));
}
.actor-row.is-selected { background: rgba(64, 152, 252, 0.08); }

.avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  overflow: hidden;
  background: rgba(128, 128, 128, 0.15);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--app-text-muted, #888);
  cursor: pointer;
}
.avatar img { width: 100%; height: 100%; object-fit: cover; }

.name-btn {
  border: 0;
  background: transparent;
  color: var(--app-text, inherit);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
  padding: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.name-btn:hover { color: var(--app-primary, #4098fc); }

.col-status { display: flex; flex-wrap: wrap; gap: 4px; }
.col-ops { display: flex; gap: 6px; justify-content: flex-end; }
.col-works { color: var(--app-text-muted, #888); }

.table-empty { padding: 60px 0; }

.pager {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-top: 16px;
}
.pager-total { font-size: 12px; color: var(--app-text-muted, #888); }

@media (max-width: 900px) {
  .actor-row {
    grid-template-columns: 36px 48px 1fr 100px;
  }
  .col-works, .col-status { display: none; }
}
</style>
