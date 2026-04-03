import { requestJson } from '@/api/client'

export interface Sub115DriveItem {
  backend_id: string
  drive_id: string
  name: string
  owner_type: string
  owner_id: string
  root_folder_id: string
  credential_mode: 'inline' | 'backend_ref' | string
  credential_backend_id: string
  enabled: boolean
  weight: number
  priority: number
  has_cookie: boolean
  drive_last_error: string
  credential_last_error: string
}

export interface Sub115DriveListResponse {
  items: Sub115DriveItem[]
}

export interface Sub115DriveUpsertRequest {
  backend_id: string
  drive_id: string
  name?: string
  owner_type?: string
  owner_id?: string
  root_folder_id?: string
  credential_mode?: 'inline' | 'backend_ref' | string
  credential_backend_id?: string
  enabled?: boolean
  weight?: number
  priority?: number
  cookie?: string
}

export function listSub115Drives(backendID: string, includeDisabled = false) {
  const params = new URLSearchParams()
  params.set('backend_id', backendID)
  if (includeDisabled) params.set('include_disabled', '1')
  return requestJson<Sub115DriveListResponse>(`/Gateway/115-Sub/Drives?${params.toString()}`)
}

export function upsertSub115Drive(payload: Sub115DriveUpsertRequest) {
  return requestJson<{ status: string }>(`/Gateway/115-Sub/Drives/Upsert`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
}

export function deleteSub115Drive(backendID: string, driveID: string) {
  return requestJson<{ status: string }>(`/Gateway/115-Sub/Drives/Delete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ backend_id: backendID, drive_id: driveID }),
  })
}
