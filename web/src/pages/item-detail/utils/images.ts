import { getImageUrl } from '@/api/client'

export function backdropSourceIdFor(data: any): string {
  if (!data) return ''
  if (data.BackdropImageTags?.length > 0) return data.Id
  return data.ParentBackdropItemId || data.Id || ''
}

export function backdropTagsFor(data: any): string[] {
  if (!data) return []
  if (data.BackdropImageTags?.length > 0) return data.BackdropImageTags
  if (data.ParentBackdropImageTags?.length > 0) return data.ParentBackdropImageTags
  if (data.ParentBackdropItemId) return ['']
  return []
}

export function backdropUrl(sourceId: string, index: number, tag: string | undefined, maxWidth: number): string {
  return getImageUrl(sourceId, 'Backdrop', {
    maxWidth,
    imageIndex: index,
    tag,
  })
}

export function primaryUrl(sourceId: string, tag: string | undefined, maxWidth: number): string {
  return getImageUrl(sourceId, 'Primary', {
    maxWidth,
    tag,
  })
}

export function personImageUrl(person: any, brokenImages: Record<string, boolean>): string {
  const imageId = person.PrimaryImageItemId || person.Id
  const imageKey = String(imageId || person.Name || '')
  if (brokenImages[imageKey]) return ''
  if (person.ImageUrl) return person.ImageUrl
  if (person.PrimaryImageTag || person.ImageTags?.Primary) return getImageUrl(imageId, 'Primary', 200)
  // 无图标记则不请求,直接交给占位 SVG,避免对无头像演员产生 404。
  return ''
}
