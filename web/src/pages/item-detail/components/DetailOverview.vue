<script setup lang="ts">
import { computed } from 'vue'
import { formatOverviewDetail } from '@/utils/overviewText'
import type { CrewGroup } from '../types'

const props = defineProps<{
  item: any
  crew: CrewGroup[]
}>()

const emit = defineEmits<{
  tagClick: [tag: string]
}>()

const overviewText = computed(() => {
  return formatOverviewDetail(props.item?.Overview)
})
</script>

<template>
  <div class="content-grid">
    <div class="content-main">
      <p v-if="item.Tagline" class="item-tagline">{{ item.Tagline }}</p>
      <template v-if="overviewText">
        <h3 class="section-heading section-heading-light">简介</h3>
        <p class="item-overview">{{ overviewText }}</p>
      </template>

      <div v-if="crew.length" class="crew-inline">
        <div v-for="group in crew" :key="group.label" class="crew-group">
          <span class="crew-label">{{ group.label }}</span>
          <span class="crew-names">{{ group.people.map(p => p.Name).join(', ') }}</span>
        </div>
      </div>
    </div>

    <div class="content-facts">
      <div v-if="item.Tags?.length" class="facts-block">
        <h3 class="section-heading">标签</h3>
        <div class="metadata-chip-row">
          <button
            v-for="tag in item.Tags"
            :key="tag"
            type="button"
            class="metadata-chip"
            :aria-label="`按标签筛选:${tag}`"
            @click="emit('tagClick', tag)"
          >
            {{ tag }}
          </button>
        </div>
      </div>

      <div v-if="item.ProviderIds?.Tmdb || item.ProviderIds?.Imdb" class="facts-block">
        <h3 class="section-heading">外部链接</h3>
        <div class="ext-links">
          <a v-if="item.ProviderIds?.Tmdb" :href="`https://www.themoviedb.org/${item.Type === 'Movie' ? 'movie' : 'tv'}/${item.ProviderIds.Tmdb}`" target="_blank" rel="noopener noreferrer" class="ext-link ext-tmdb">TMDB ↗</a>
          <a v-if="item.ProviderIds?.Imdb" :href="`https://www.imdb.com/title/${item.ProviderIds.Imdb}`" target="_blank" rel="noopener noreferrer" class="ext-link ext-imdb">IMDb ↗</a>
        </div>
      </div>
    </div>
  </div>
</template>
