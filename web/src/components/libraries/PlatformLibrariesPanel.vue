<script setup lang="ts">
import { NButton, NCheckbox, NCheckboxGroup, NIcon, NInput, NInputNumber, NProgress, NSelect, NSwitch } from 'naive-ui'

import { getPlatformIcon } from '@/icons/PlatformIcons'

defineProps<{
  platformsData: { GlobalEnabled: boolean; Platforms: any[] }
  platformTask: any
  platformScanning: boolean
  filenameScanning: boolean
  rescraping: boolean
  rescrapeStatus: any
  platformPosition: 'before' | 'after'
  showLibraryItemCount: boolean
  savingConfig: boolean
  dimensionOptions: { label: string; value: string }[]
  discoverDimension: 'studio' | 'num_prefix' | 'actor'
  discoverSearch: string
  discoverMinCount: number
  discoverLoading: boolean
  discoverResults: { Value: string; Count: number; AlreadyAdded: boolean }[]
  discoverSelected: string[]
  newPlatformName: string
}>()

const emit = defineEmits<{
  toggleGlobalPlatform: [enabled: boolean]
  updatePlatformPosition: [value: 'before' | 'after']
  updateShowLibraryItemCount: [value: boolean]
  saveSettings: []
  scanStudios: []
  scanFilename: []
  rescrape: []
  updateDiscoverDimension: [value: 'studio' | 'num_prefix' | 'actor']
  updateDiscoverSearch: [value: string]
  updateDiscoverMinCount: [value: number]
  updateDiscoverSelected: [value: string[]]
  runDiscover: []
  addSelectedDimension: []
  openPlatformCover: [id: string | null]
  openAlias: [platform: any]
  openRename: [platform: any]
  restoreCover: [id: string]
  movePlatform: [index: number, dir: number]
  togglePlatform: [id: string, enabled: boolean]
  deletePlatform: [id: string]
  updateNewPlatformName: [value: string]
  addPlatform: []
}>()
</script>

<template>
  <div class="settings-card">
    <div class="settings-card-header">
      <div>
        <h3 class="settings-card-title">平台媒体库</h3>
        <div class="settings-card-desc">根据 TMDB 的出品平台信息（Netflix、HBO 等）自动生成虚拟媒体库，在播放器中可见。</div>
      </div>
    </div>

    <div class="setting-row">
      <div>
        <div class="setting-label">启用平台库</div>
        <div class="setting-desc">开启后，已启用的平台将作为虚拟媒体库显示在播放器中</div>
      </div>
      <n-switch :value="platformsData.GlobalEnabled" @update:value="emit('toggleGlobalPlatform', $event)" />
    </div>

    <div class="setting-row">
      <div>
        <div class="setting-label">显示偏好</div>
        <div class="setting-desc">控制虚拟库在播放器中的位置，以及媒体库卡片是否显示总数。</div>
      </div>
      <div class="platform-display-controls">
        <n-select
          :value="platformPosition"
          :options="[{ label: '媒体库前面', value: 'before' }, { label: '媒体库后面', value: 'after' }]"
          class="platform-position-select"
          @update:value="emit('updatePlatformPosition', $event)"
        />
        <n-checkbox :checked="showLibraryItemCount" @update:checked="emit('updateShowLibraryItemCount', $event)">显示媒体总数</n-checkbox>
        <n-button size="small" type="primary" :loading="savingConfig" @click="emit('saveSettings')">保存</n-button>
      </div>
    </div>

    <div class="setting-row platform-action-row">
      <div class="platform-action-copy">
        <div class="setting-label">从 TMDB 获取平台</div>
        <div class="setting-desc">通过 TMDB API 获取 networks/出品公司（需已刮削，速度较慢）</div>
        <div v-if="platformTask" class="setting-desc task-hint">
          当前待平台识别 {{ platformTask.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，其中可直接扫描 {{ platformTask.pending_tmdb_ready_total || 0 }} 项
        </div>
      </div>
      <n-button secondary :loading="platformScanning" :disabled="platformScanning || ((platformTask?.pending_tmdb_ready_total || 0) === 0 && !platformTask?.scan_running)" @click="emit('scanStudios')">
        {{ platformScanning ? '扫描中...' : 'TMDB 扫描' }}
      </n-button>
    </div>
    <div class="setting-row platform-action-row">
      <div class="platform-action-copy">
        <div class="setting-label">从文件名识别平台</div>
        <div class="setting-desc">分析文件名中的平台标识（NF/DSNP/ATVP/AMZN/HMAX 等），速度快覆盖广</div>
      </div>
      <n-button secondary :loading="filenameScanning" :disabled="filenameScanning" @click="emit('scanFilename')">
        {{ filenameScanning ? '扫描中...' : '文件名扫描' }}
      </n-button>
    </div>
    <div class="setting-row platform-action-row">
      <div class="platform-action-copy">
        <div class="setting-label">重新刮削无平台项目</div>
        <div class="setting-desc">对仍无平台信息的 Movie/Series 重新执行完整 TMDB 刮削（耗时较长）</div>
        <div v-if="platformTask && !rescrapeStatus?.running" class="setting-desc task-hint">
          当前待重新刮削 {{ rescrapeStatus?.pending_total || 0 }} / {{ platformTask.items_total || 0 }} 项，仍缺少 TMDB 的有 {{ platformTask.pending_metadata_total || 0 }} 项
        </div>
      </div>
      <n-button secondary type="warning" :loading="rescraping" :disabled="rescraping || (!!rescrapeStatus && !rescrapeStatus.running && (rescrapeStatus.pending_total || 0) === 0)" @click="emit('rescrape')">
        {{ rescraping ? '刮削中...' : '重新刮削' }}
      </n-button>
    </div>
    <div v-if="rescrapeStatus && rescrapeStatus.running" class="rescrape-progress">
      <n-progress type="line" :percentage="rescrapeStatus.percentage" :show-indicator="true" status="info" />
      <div class="rescrape-stats">
        已处理 {{ rescrapeStatus.processed }} / {{ rescrapeStatus.total }}
        <span class="stat-success">成功 {{ rescrapeStatus.success }}</span>
        <span class="stat-warn">未找到 {{ rescrapeStatus.not_found }}</span>
        <span class="stat-error">请求失败 {{ rescrapeStatus.fetch_error }}</span>
      </div>
    </div>
    <div v-else-if="rescrapeStatus && !rescrapeStatus.running && rescrapeStatus.total > 0" class="rescrape-progress">
      <div class="rescrape-stats">
        刮削完成: 共 {{ rescrapeStatus.total }} 项
        <span class="stat-success">成功 {{ rescrapeStatus.success }}</span>
        <span class="stat-warn">TMDB未收录 {{ rescrapeStatus.not_found }}</span>
        <span class="stat-error">网络错误 {{ rescrapeStatus.fetch_error }}</span>
      </div>
    </div>

    <div class="platform-section">
      <div class="setting-label platform-section-title">扫描分类（按维度发现，勾选后添加）</div>
      <div class="discover-toolbar">
        <n-select :value="discoverDimension" :options="dimensionOptions" size="small" class="discover-dimension" @update:value="emit('updateDiscoverDimension', $event)" />
        <n-input :value="discoverSearch" placeholder="搜索（可选）" size="small" class="discover-search" @update:value="emit('updateDiscoverSearch', $event)" @keydown.enter.prevent="emit('runDiscover')" />
        <n-input-number :value="discoverMinCount" :min="1" size="small" class="discover-min" title="最少影片数" @update:value="emit('updateDiscoverMinCount', $event || 1)" />
        <n-button secondary size="small" :loading="discoverLoading" @click="emit('runDiscover')">扫描</n-button>
      </div>
      <div v-if="discoverResults.length > 0" class="discover-results">
        <n-checkbox-group :value="discoverSelected" @update:value="emit('updateDiscoverSelected', $event as string[])">
          <div class="discover-grid">
            <n-checkbox v-for="d in discoverResults" :key="d.Value" :value="d.Value" :disabled="d.AlreadyAdded">
              {{ d.Value }} <span class="platform-count">{{ d.Count }}</span>
              <span v-if="d.AlreadyAdded" class="already-added">(已加)</span>
            </n-checkbox>
          </div>
        </n-checkbox-group>
        <div class="discover-actions">
          <n-button type="primary" size="small" :disabled="discoverSelected.length === 0" @click="emit('addSelectedDimension')">
            添加所选 ({{ discoverSelected.length }})
          </n-button>
          <span class="setting-desc">共 {{ discoverResults.length }} 项 · 添加后默认关闭，需在下方启用</span>
        </div>
      </div>
    </div>

    <div class="platform-section">
      <div class="platform-list-head">
        <div class="setting-label">平台/虚拟库列表</div>
        <n-button text size="tiny" @click="emit('openPlatformCover', null)">一键生成封面</n-button>
      </div>
      <div v-for="(p, idx) in platformsData.Platforms" :key="p.Id" class="platform-row">
        <img v-if="p.CoverUrl" :src="p.CoverUrl" class="platform-cover-thumb" alt="" />
        <img v-else-if="p.LogoUrl" :src="p.LogoUrl" class="platform-logo-icon" alt="" />
        <n-icon v-else size="28" class="platform-fallback-icon"><component :is="getPlatformIcon(p.PlatformName)" /></n-icon>
        <div class="platform-row-main">
          <span class="platform-name">{{ p.DisplayName || p.PlatformName }}</span>
          <span class="platform-dim-badge">{{ p.Dimension }}</span>
          <span v-if="(p.MatchValues?.length || 1) > 1" class="platform-dim-badge" title="已聚合的匹配值数量">聚合 {{ p.MatchValues.length }}</span>
          <span class="platform-count">{{ p.ItemCount }} 部</span>
        </div>
        <div class="platform-row-actions">
          <n-button text size="tiny" title="聚合多个匹配值" @click="emit('openAlias', p)">聚合</n-button>
          <n-button text size="tiny" title="重命名" @click="emit('openRename', p)">重命名</n-button>
          <n-button text size="tiny" title="生成封面" @click="emit('openPlatformCover', p.Id)">封面</n-button>
          <n-button v-if="p.HasCover" text size="tiny" title="恢复默认封面" @click="emit('restoreCover', p.Id)">恢复默认</n-button>
          <n-button text size="tiny" :disabled="idx === 0" title="上移" @click="emit('movePlatform', idx, -1)">上移</n-button>
          <n-button text size="tiny" :disabled="idx === platformsData.Platforms.length - 1" title="下移" @click="emit('movePlatform', idx, 1)">下移</n-button>
          <n-switch :value="p.Enabled" size="small" @update:value="emit('togglePlatform', p.Id, $event)" />
          <n-button text type="error" size="tiny" @click="emit('deletePlatform', p.Id)">删除</n-button>
        </div>
      </div>
    </div>

    <div class="platform-add-row">
      <n-input :value="newPlatformName" placeholder="自定义片商名称(studio 维度)" size="small" @update:value="emit('updateNewPlatformName', $event)" @keydown.enter.prevent="emit('addPlatform')" />
      <n-button secondary size="small" @click="emit('addPlatform')">添加</n-button>
    </div>
  </div>
</template>

<style scoped>
.settings-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 20px 24px;
}
.settings-card-header { margin-bottom: 10px; }
.settings-card-title { font-size: 15px; font-weight: 600; color: var(--app-text); margin: 0 0 6px; }
.settings-card-desc { font-size: 13px; color: var(--app-text-muted); }
.setting-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 0; }
.setting-row + .setting-row { border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.setting-label { font-size: 14px; color: var(--app-text); }
.setting-desc { font-size: 12px; color: var(--app-text-muted); margin-top: 2px; }

.platform-display-controls { display: flex; align-items: center; justify-content: flex-end; gap: 12px; flex-wrap: wrap; }
.platform-position-select { width: 140px; }
.platform-action-row { flex-wrap: wrap; gap: 8px; }
.platform-action-copy { flex: 1; min-width: 220px; }
.task-hint { margin-top: 6px; }

.rescrape-progress { margin-top: 12px; padding: 12px 0; border-top: 1px solid var(--app-border, rgba(255,255,255,0.04)); }
.rescrape-stats { font-size: 13px; color: var(--app-text-muted); margin-top: 6px; }
.stat-success { color: #18a058; margin-left: 12px; }
.stat-warn { color: #f0a020; margin-left: 12px; }
.stat-error { color: #d03050; margin-left: 12px; }

.platform-section {
  margin-top: 16px;
  border-top: 1px solid var(--app-border, rgba(255,255,255,0.04));
  padding-top: 16px;
}
.platform-section-title { margin-bottom: 8px; }
.discover-toolbar { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.discover-dimension { width: 180px; }
.discover-search { flex: 1; min-width: 120px; }
.discover-min { width: 110px; }
.discover-results { margin-top: 10px; }
.discover-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 6px 12px; max-height: 320px; overflow-y: auto; padding: 4px 2px; }
.discover-actions { margin-top: 8px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.already-added { color: var(--n-text-color-disabled); font-size: 11px; }

.platform-list-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.platform-row { display: flex; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.04)); gap: 8px; }
.platform-row:last-child { border-bottom: none; }
.platform-row-main { flex: 1; min-width: 0; }
.platform-row-actions { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; justify-content: flex-end; }
.platform-name { font-size: 14px; color: var(--app-text); font-weight: 500; }
.platform-count { font-size: 12px; color: var(--app-text-muted); margin-left: 8px; }
.platform-logo-icon { width: 28px; height: 28px; border-radius: 6px; object-fit: cover; flex-shrink: 0; }
.platform-cover-thumb { width: 48px; height: 27px; border-radius: 4px; object-fit: cover; flex-shrink: 0; }
.platform-fallback-icon { flex-shrink: 0; }
.platform-dim-badge { font-size: 10px; color: var(--app-text-muted); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); border-radius: 4px; padding: 0 5px; margin-left: 8px; vertical-align: middle; }
.platform-add-row { margin-top: 16px; display: flex; gap: 8px; }

@media (max-width: 640px) {
  .setting-row {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }

  .platform-display-controls,
  .platform-row-actions {
    justify-content: flex-start;
  }

  .platform-row {
    align-items: flex-start;
    flex-wrap: wrap;
  }
}
</style>
