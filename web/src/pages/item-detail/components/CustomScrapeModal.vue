<script setup lang="ts">
import { NButton, NInput, NInputNumber, NModal, NSpin } from 'naive-ui'

defineProps<{
  show: boolean
  customTmdbId: number | null
  customQuery: string
  customYear: number | null
  tmdbResults: any[]
  tmdbSearching: boolean
  tmdbApplying: number | null
  hasSearchedTmdb: boolean
  modalStyle: Record<string, string>
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  'update:customTmdbId': [value: number | null]
  'update:customQuery': [value: string]
  'update:customYear': [value: number | null]
  searchId: []
  search: []
  apply: [tmdbId: number]
}>()
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="自定义刮削 - 搜索 TMDB"
    :style="[modalStyle, { maxWidth: '680px', maxHeight: '85vh' }]"
    class="solid-modal-card force-solid-modal custom-scrape-modal"
    :bordered="false"
    @update:show="emit('update:show', $event)"
  >
    <div class="tmdb-id-search-bar">
      <n-input-number
        :value="customTmdbId"
        :min="1"
        :precision="0"
        placeholder="输入 TMDB ID 精确搜索"
        clearable
        style="flex: 1"
        @update:value="emit('update:customTmdbId', $event)"
        @keyup.enter="emit('searchId')"
      />
      <n-button type="primary" :loading="tmdbSearching" :disabled="tmdbSearching || !customTmdbId || customTmdbId <= 0" @click="emit('searchId')">搜索 ID</n-button>
    </div>
    <div class="tmdb-search-divider"><span>或通过名称搜索</span></div>
    <div class="tmdb-search-bar">
      <n-input :value="customQuery" placeholder="输入名称搜索 TMDB" clearable style="flex: 1" @update:value="emit('update:customQuery', $event)" @keyup.enter="emit('search')" />
      <n-input-number :value="customYear" :min="1900" :max="2030" placeholder="年份" clearable style="width: 110px" @update:value="emit('update:customYear', $event)" />
      <n-button type="primary" :loading="tmdbSearching" :disabled="tmdbSearching || !customQuery.trim()" @click="emit('search')">搜索</n-button>
    </div>
    <div v-if="tmdbSearching" class="tmdb-loading"><n-spin /></div>
    <div v-else-if="tmdbResults.length" class="tmdb-results">
      <div v-for="r in tmdbResults" :key="r.id" class="tmdb-result-card" @click="emit('apply', r.id)">
        <img v-if="r.poster_path" :src="'https://image.tmdb.org/t/p/w92' + r.poster_path" class="tmdb-poster" />
        <div v-else class="tmdb-poster tmdb-poster-empty">?</div>
        <div class="tmdb-info">
          <div class="tmdb-title">
            {{ r.title || r.name || '未知' }}
            <span v-if="(r.release_date || r.first_air_date)" class="tmdb-year">({{ (r.release_date || r.first_air_date || '').substring(0, 4) }})</span>
          </div>
          <div class="tmdb-meta">
            <span v-if="r.vote_average" class="tmdb-rating">TMDB {{ r.vote_average?.toFixed?.(1) || r.vote_average }}</span>
            <span class="tmdb-id">ID: {{ r.id }}</span>
          </div>
          <div v-if="r.overview" class="tmdb-overview">{{ r.overview.length > 120 ? r.overview.substring(0, 120) + '...' : r.overview }}</div>
        </div>
        <div v-if="tmdbApplying === r.id" class="tmdb-applying"><n-spin size="small" /></div>
      </div>
    </div>
    <div v-else-if="!tmdbSearching" class="tmdb-empty-state">{{ hasSearchedTmdb ? '未找到 TMDB 结果' : '输入 TMDB ID 或名称后点击搜索' }}</div>
  </n-modal>
</template>

