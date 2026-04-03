import { requestJson } from '@/api/client'

export function clearIPInfos() {
  return requestJson<{ status: string }>('/Gateway/IPInfos/Clear', { method: 'POST' })
}
