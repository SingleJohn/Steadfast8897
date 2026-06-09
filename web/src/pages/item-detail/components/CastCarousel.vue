<script setup lang="ts">
import { ref } from 'vue'
import { personImageUrl } from '../utils/images'

defineProps<{
  actors: any[]
}>()

const emit = defineEmits<{
  personClick: [person: any]
}>()

const brokenPeopleImages = ref<Record<string, boolean>>({})

function personImgSrc(person: any): string {
  return personImageUrl(person, brokenPeopleImages.value)
}

function handlePersonImageError(person: any) {
  const imageKey = String(person.PrimaryImageItemId || person.Id || person.Name || '')
  if (!imageKey) return
  brokenPeopleImages.value = {
    ...brokenPeopleImages.value,
    [imageKey]: true,
  }
}
</script>

<template>
  <div v-if="actors.length" class="cast-section">
    <h3 class="section-heading section-heading-light">演员</h3>
    <div class="cast-scroll">
      <button
        v-for="p in actors.slice(0, 20)"
        :key="p.Name + (p.Role || '')"
        type="button"
        class="cast-card"
        :aria-label="`查看${p.Name}的作品`"
        @click="emit('personClick', p)"
      >
        <div class="cast-img">
          <img v-if="personImgSrc(p)" :src="personImgSrc(p)" :alt="p.Name" width="120" height="120" loading="lazy" @error="handlePersonImageError(p)" />
          <svg v-else width="24" height="24" viewBox="0 0 24 24" fill="currentColor" opacity="0.3" aria-hidden="true"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>
        </div>
        <div class="cast-info">
          <span class="cast-name">{{ p.Name }}</span>
          <span v-if="p.Role" class="cast-role">{{ p.Role }}</span>
        </div>
      </button>
    </div>
  </div>
</template>
