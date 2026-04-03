import { requestJson } from '@/api/client'

export interface ErrorLogResponse {
  path: string
  size: number
  from_offset: number
  next_offset: number
  text: string
  truncated: boolean
}

const EMPTY_RESPONSE: ErrorLogResponse = {
  path: '',
  size: 0,
  from_offset: 0,
  next_offset: 0,
  text: '',
  truncated: false,
}

export async function getErrorLogTail(tailLines = 200): Promise<ErrorLogResponse> {
  try {
    const search = new URLSearchParams()
    search.set('tail_lines', String(tailLines))
    return await requestJson<ErrorLogResponse>(`/Gateway/Console/ErrorLog?${search.toString()}`)
  } catch {
    return EMPTY_RESPONSE
  }
}

export async function readErrorLog(offset: number, limitBytes = 64 * 1024): Promise<ErrorLogResponse> {
  try {
    const search = new URLSearchParams()
    search.set('offset', String(offset))
    search.set('limit_bytes', String(limitBytes))
    return await requestJson<ErrorLogResponse>(`/Gateway/Console/ErrorLog?${search.toString()}`)
  } catch {
    return EMPTY_RESPONSE
  }
}
