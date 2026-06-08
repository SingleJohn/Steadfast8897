<script setup lang="ts">
import type { CrewGroup } from '../types'

defineProps<{
  item: any
  crew: CrewGroup[]
}>()
</script>

<template>
  <div class="content-grid">
    <div class="content-main">
      <p v-if="item.Tagline" class="item-tagline">{{ item.Tagline }}</p>
      <template v-if="item.Overview">
        <h3 class="section-heading section-heading-light">简介</h3>
        <p class="item-overview">{{ item.Overview }}</p>
      </template>

      <div v-if="crew.length" class="crew-inline">
        <div v-for="group in crew" :key="group.label" class="crew-group">
          <span class="crew-label">{{ group.label }}</span>
          <span class="crew-names">{{ group.people.map(p => p.Name).join(', ') }}</span>
        </div>
      </div>
    </div>

    <div class="content-facts">
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

