import { reactive, toRefs } from 'vue'

import { getBrandingConfig, type BrandingConfig } from '@/api/client'

const state = reactive({
  loaded: false,
  loading: false,
  serverName: 'FYMS',
  iconUrl: '',
})

function applyFavicon(iconUrl: string) {
  let link = document.querySelector("link[rel='icon']") as HTMLLinkElement | null
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.type = 'image/svg+xml'
  link.href = iconUrl || '/favicon.svg'
}

function applyBranding(branding: BrandingConfig) {
  state.serverName = branding.ServerName || 'FYMS'
  state.iconUrl = branding.IconUrl || ''
  applyFavicon(state.iconUrl)
}

async function loadBranding(force = false) {
  if (state.loading) return
  if (state.loaded && !force) return

  state.loading = true
  try {
    const branding = await getBrandingConfig()
    applyBranding(branding)
    state.loaded = true
  } catch {
    applyFavicon('')
  } finally {
    state.loading = false
  }
}

function applyDocumentTitle(pageTitle?: string) {
  const brandName = state.serverName || 'FYMS'
  document.title = pageTitle ? `${pageTitle} - ${brandName}` : brandName
}

export function svgToDataUrl(svg: string): string {
  const raw = String(svg || '').trim()
  if (!raw) return ''
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(raw)}`
}

export function useBranding() {
  return {
    ...toRefs(state),
    loadBranding,
    refreshBranding: () => loadBranding(true),
    applyDocumentTitle,
  }
}
