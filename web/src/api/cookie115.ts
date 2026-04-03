import { requestJson } from '@/api/client'

export interface Cookie115CredentialResponse {
  backend_id: string
  has_cookie: boolean
  cookie: string
  expires_at: number
  last_error: string
}

export interface Cookie115CredentialUpsertRequest {
  backend_id: string
  cookie: string
  expires_seconds?: number
}

export function getCookie115Credential(backendID: string) {
  const params = new URLSearchParams()
  params.set('backend_id', backendID)
  return requestJson<Cookie115CredentialResponse>(`/Gateway/115-Cookie/Credential?${params.toString()}`)
}

export function upsertCookie115Credential(payload: Cookie115CredentialUpsertRequest) {
  return requestJson<{ status: string }>(`/Gateway/115-Cookie/Credential/Upsert`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
}
