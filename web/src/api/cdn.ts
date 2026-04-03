import { requestJson } from '@/api/client'

export interface CdnSignResponse {
  uri: string
  url: string
  expiry: number
  timestamp: number
  rand: string
  uid: string
  md5hash: string
  auth_key: string
}

export function cdnSign(params: { backend_id: string; key: string; uid?: string }) {
  return requestJson<CdnSignResponse>('/Gateway/CDN/Sign', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      backend_id: params.backend_id,
      key: params.key,
      uid: params.uid || '',
    }),
  })
}
