import { requestJson } from '@/api/client'

export interface S3ObjectItem {
  key: string
  size: number
  last_modified: string
  etag: string
}

export interface S3ListResponse {
  bucket: string
  prefix: string
  is_truncated: boolean
  next_token: string
  common_prefix: string[]
  objects: S3ObjectItem[]
}

export interface S3PresignResponse {
  url: string
  expiry: number
}

export function s3List(params: { backend_id: string; prefix?: string; limit?: number; continuation?: string }) {
  const search = new URLSearchParams()
  search.set('backend_id', params.backend_id)
  if (params.prefix) search.set('prefix', params.prefix)
  if (typeof params.limit === 'number') search.set('limit', String(params.limit))
  if (params.continuation) search.set('continuation', params.continuation)
  return requestJson<S3ListResponse>(`/Gateway/S3/List?${search.toString()}`)
}

export function s3Presign(params: { backend_id: string; key: string }) {
  const search = new URLSearchParams()
  search.set('backend_id', params.backend_id)
  search.set('key', params.key)
  return requestJson<S3PresignResponse>(`/Gateway/S3/Presign?${search.toString()}`)
}

export function s3Check(backend_id: string) {
  const search = new URLSearchParams()
  search.set('backend_id', backend_id)
  return requestJson<{ status: string; message: string }>(`/Gateway/S3/Check?${search.toString()}`, { method: 'POST' })
}
