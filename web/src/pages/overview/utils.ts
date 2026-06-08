export function formatVersion(info: any): string {
  const ver = info.Version || 'dev'
  const commit = info.BuildCommit
  if (commit) return `${ver} (${commit.substring(0, 7)})`
  return ver
}

export function formatServerId(id: string | undefined): string {
  if (!id) return '-'
  if (id.length === 32 && !id.includes('-')) {
    return `${id.slice(0, 8)}-${id.slice(8, 12)}-${id.slice(12, 16)}-${id.slice(16, 20)}-${id.slice(20)}`
  }
  return id
}

export function isUpdateBusy(status?: string) {
  return ['checking', 'backing_up', 'pulling', 'recreating', 'restarting'].includes(status || '')
}

export function formatUpdateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
