const API_BASE = '';

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

type RequestOptions = RequestInit & { timeoutMs?: number }

export async function requestJson<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { timeoutMs = 12_000, ...init } = options
  const headers = new Headers(init.headers)
  const token = getToken()
  if (token) headers.set('X-Emby-Token', token)
  if (!headers.has('content-type') && !headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeoutMs)
  try {
    const res = await fetch(`${API_BASE}${path}`, { ...init, headers, signal: controller.signal })
    if (res.status === 401) {
      localStorage.removeItem('accessToken')
      localStorage.removeItem('userId')
      localStorage.removeItem('userName')
      localStorage.removeItem('isAdmin')
      window.location.href = '/#/login'
      throw new ApiError('未登录或会话过期', 401)
    }
    const contentType = res.headers.get('content-type') || ''
    const isJson = contentType.includes('application/json')
    const body = isJson ? await res.json().catch(() => null) : await res.text().catch(() => '')
    if (!res.ok) {
      const errMsg =
        typeof body === 'object' && body && 'error' in body && typeof body.error === 'string'
          ? body.error
          : `Request failed: ${res.status}`
      throw new ApiError(errMsg, res.status)
    }
    return body as T
  } catch (e) {
    if (e instanceof ApiError) throw e
    if (e instanceof DOMException && e.name === 'AbortError') throw new ApiError('请求超时', 0)
    if (e instanceof TypeError) throw new ApiError('网络错误', 0)
    throw new ApiError((e as Error).message || 'request failed', 0)
  } finally {
    window.clearTimeout(timer)
  }
}

function getToken(): string | null {
  return localStorage.getItem('accessToken');
}

function getUserId(): string | null {
  return localStorage.getItem('userId');
}

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  const token = getToken();
  if (token) {
    headers['X-Emby-Token'] = token;
  }
  return headers;
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      ...getAuthHeaders(),
      ...options.headers,
    },
  });
  if (res.status === 401) {
    localStorage.removeItem('accessToken');
    localStorage.removeItem('userId');
    localStorage.removeItem('userName');
    localStorage.removeItem('isAdmin');
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }
  if (res.status === 204) return undefined as T;
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// Auth
export async function getPublicUsers() {
  return request<any[]>('/Users/Public');
}

export async function login(username: string, password: string) {
  return request<any>('/Users/AuthenticateByName', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `MediaBrowser Client="Media Server Web", Device="Browser", DeviceId="${getBrowserId()}", Version="1.0.0"`,
    },
    body: JSON.stringify({ Username: username, Pw: password }),
  });
}

export async function logout() {
  return request('/Sessions/Logout', { method: 'POST' });
}

export async function getStartupConfig() {
  return request<any>('/Startup/Configuration');
}

export async function createStartupUser(name: string, password: string) {
  return request('/Startup/User', {
    method: 'POST',
    body: JSON.stringify({ Name: name, Password: password }),
  });
}

export async function completeStartup() {
  return request('/Startup/Complete', { method: 'POST' });
}

// System
export async function getSystemInfo() {
  return request<any>('/System/Info/Public');
}

export type UpdateStatus = {
  status: string
  message: string
  currentVersion: string
  latestVersion: string
  targetVersion: string
  channel: string
  hasUpdate: boolean
  currentImage?: string
  targetImage?: string
  releaseSource?: string
  releaseNotesUrl?: string
  githubReleaseUrl?: string
  helperContainer?: string
  lastCheckedAt?: string
  startedAt?: string
  completedAt?: string
  error?: string
  logs?: string[]
  needsDockerSocket?: boolean
  deploymentMode?: 'docker' | 'binary' | 'manual'
  downloadUrl?: string
}

export async function getUpdateStatus() {
  return requestJson<UpdateStatus>('/System/Update/Status')
}

export async function checkForUpdate() {
  return requestJson<UpdateStatus>('/System/Update/Check', { method: 'POST' })
}

export async function applyUpdate(categories: string[] = ['settings', 'users', 'libraries', 'media']) {
  return requestJson<UpdateStatus>('/System/Update/Apply', {
    method: 'POST',
    body: JSON.stringify({ categories }),
  })
}

export async function setUpdateChannel(channel: 'stable' | 'nightly') {
  return requestJson<UpdateStatus>('/System/Update/Channel', {
    method: 'POST',
    body: JSON.stringify({ channel }),
  })
}

// Library
export async function getViews() {
  const userId = getUserId();
  return request<any>(`/Users/${userId}/Views`);
}

export async function getItems(params: Record<string, string> = {}) {
  const userId = getUserId();
  const qs = new URLSearchParams(params).toString();
  return request<any>(`/Users/${userId}/Items?${qs}`);
}

export async function getItem(itemId: string) {
  const userId = getUserId();
  return request<any>(`/Users/${userId}/Items/${itemId}`);
}

export async function getResumeItems(limit = 12) {
  const userId = getUserId();
  return request<any>(`/Users/${userId}/Items/Resume?Limit=${limit}`);
}

export async function getLatestItems(parentId: string, limit = 16) {
  const userId = getUserId();
  return request<any[]>(`/Users/${userId}/Items/Latest?ParentId=${parentId}&Limit=${limit}`);
}

export async function getLatestBatch(libraryIds: string[], limit = 16) {
  const userId = getUserId();
  return request<Record<string, any[]>>(`/Users/${userId}/Items/LatestBatch?LibraryIds=${libraryIds.join(',')}&Limit=${limit}`);
}

// Images
export function getImageUrl(itemId: string, type: string = 'Primary', maxWidth?: number): string {
  let url = `/Items/${itemId}/Images/${type}`;
  const params: string[] = [];
  if (maxWidth) params.push(`maxWidth=${maxWidth}`);
  params.push('format=jpg');
  params.push('quality=90');
  if (params.length) url += '?' + params.join('&');
  return url;
}

// Playback
export async function getPlaybackInfo(itemId: string) {
  return request<any>(`/Items/${itemId}/PlaybackInfo?UserId=${getUserId()}`);
}

export function getStreamUrl(itemId: string, mediaSourceId: string): string {
  const token = getToken();
  return `/Videos/${itemId}/stream?static=true&MediaSourceId=${mediaSourceId}&api_key=${token}`;
}

export async function reportPlaybackStart(itemId: string, positionTicks = 0) {
  return request('/Sessions/Playing', {
    method: 'POST',
    body: JSON.stringify({ ItemId: itemId, PositionTicks: positionTicks }),
  });
}

export async function reportPlaybackProgress(itemId: string, positionTicks: number, isPaused = false) {
  return request('/Sessions/Playing/Progress', {
    method: 'POST',
    body: JSON.stringify({ ItemId: itemId, PositionTicks: positionTicks, IsPaused: isPaused }),
  });
}

export async function reportPlaybackStopped(itemId: string, positionTicks: number) {
  return request('/Sessions/Playing/Stopped', {
    method: 'POST',
    body: JSON.stringify({ ItemId: itemId, PositionTicks: positionTicks }),
  });
}

// User Data
export async function toggleFavorite(itemId: string, isFavorite: boolean) {
  const userId = getUserId();
  return request(`/Users/${userId}/FavoriteItems/${itemId}`, {
    method: isFavorite ? 'POST' : 'DELETE',
  });
}

export async function togglePlayed(itemId: string, played: boolean) {
  const userId = getUserId();
  return request(`/Users/${userId}/PlayedItems/${itemId}`, {
    method: played ? 'POST' : 'DELETE',
  });
}

// Library management
export async function getLibraries() {
  return request<any[]>('/Library/VirtualFolders');
}

export type ItemCounts = {
  MovieCount: number
  SeriesCount: number
  EpisodeCount: number
  ArtistCount?: number
  ProgramCount?: number
  TrailerCount?: number
  SongCount?: number
  AlbumCount?: number
  MusicVideoCount?: number
  BoxSetCount?: number
  BookCount?: number
}

export async function getItemCounts() {
  return request<ItemCounts>('/Items/Counts');
}

export async function addLibrary(name: string, collectionType: string, paths: string[]) {
  return request(`/Library/VirtualFolders/Add?name=${encodeURIComponent(name)}&collectionType=${collectionType}`, {
    method: 'POST',
    body: JSON.stringify({ Paths: paths }),
  });
}

export async function refreshLibrary() {
  return request('/Library/Refresh', { method: 'POST' });
}

export async function getLibraryDetail(id: string) {
  return request<any>(`/Library/VirtualFolders/${id}`);
}

export async function updateLibraryInfo(id: string, data: { Name?: string; CollectionType?: string; Paths?: string[] }) {
  return request('/Library/VirtualFolders/Update', {
    method: 'POST',
    body: JSON.stringify({ Id: id, ...data }),
  });
}

export async function deleteLibraryById(id: string) {
  return request(`/Library/VirtualFolders?id=${id}`, { method: 'DELETE' });
}

export async function addLibraryPath(id: string, path: string) {
  return request('/Library/VirtualFolders/Paths', {
    method: 'POST',
    body: JSON.stringify({ Id: id, Path: path }),
  });
}

export async function removeLibraryPath(id: string, path: string) {
  return request(`/Library/VirtualFolders/Paths?id=${id}&path=${encodeURIComponent(path)}`, {
    method: 'DELETE',
  });
}

export async function refreshSingleLibrary(id: string) {
  return request(`/Library/VirtualFolders/${id}/Refresh`, { method: 'POST' });
}

export async function uploadLibraryImage(id: string, file: File) {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetch(`/Library/VirtualFolders/${id}/Image`, {
    method: 'POST',
    headers: { 'X-Emby-Token': getToken() || '' },
    body: formData,
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function setLibraryImageUrl(id: string, url: string) {
  return request<{ ImageTag: string }>(`/Library/VirtualFolders/${id}/ImageUrl`, {
    method: 'POST',
    body: JSON.stringify({ Url: url }),
  });
}

export async function deleteLibraryImage(id: string) {
  return request(`/Library/VirtualFolders/${id}/Image`, { method: 'DELETE' });
}

// User management
export async function getAllUsers() {
  return request<any[]>('/Users');
}

export async function createNewUser(name: string, password: string) {
  return request<any>('/Users/New', {
    method: 'POST',
    body: JSON.stringify({ Name: name, Password: password }),
  });
}

export async function updateUserInfo(userId: string, data: any) {
  return request<any>(`/Users/${userId}`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function deleteUserById(userId: string) {
  return request(`/Users/${userId}`, { method: 'DELETE' });
}

export async function updateUserPolicy(userId: string, policy: any) {
  return request(`/Users/${userId}/Policy`, {
    method: 'POST',
    body: JSON.stringify(policy),
  });
}

export async function changeUserPassword(userId: string, currentPw: string, newPw: string) {
  return request(`/Users/${userId}/Password`, {
    method: 'POST',
    body: JSON.stringify({ CurrentPw: currentPw, NewPw: newPw }),
  });
}

export async function getUserDetail(userId: string) {
  return request<any>(`/Users/${userId}`);
}

// Metadata scraping
export async function scrapeItemMetadata(itemId: string) {
  return request<any>(`/Items/${itemId}/Refresh`, { method: 'POST' });
}

export async function scrapeAllMetadata() {
  return request<any>('/Library/Refresh/Metadata', { method: 'POST' });
}

export async function searchTmdbForItem(itemId: string, query: string, year?: number) {
  return request<any>(`/Items/${itemId}/SearchTmdb`, {
    method: 'POST',
    body: JSON.stringify({ query, year: year || undefined }),
  });
}

export async function scrapeItemByTmdbId(itemId: string, tmdbId: number) {
  return request<any>(`/Items/${itemId}/ScrapeByTmdbId`, {
    method: 'POST',
    body: JSON.stringify({ tmdbId }),
  });
}

export async function listUnmatchedItems(params?: { type?: string; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.type) qs.set('type', params.type);
  if (params?.limit) qs.set('limit', String(params.limit));
  const suffix = qs.toString() ? `?${qs}` : '';
  return requestJson<{ items: any[]; count: number }>(`/Library/Scrape/Unmatched${suffix}`);
}

export async function batchApplyIdentifyCandidates(items: { item_id: string; candidate_id: string }[]) {
  return requestJson<{ results: { item_id: string; ok: boolean; message?: string }[] }>(
    '/Library/Scrape/Unmatched/Apply',
    { method: 'POST', body: JSON.stringify({ items }) },
  );
}

export async function getItemIdentifyCandidates(itemId: string) {
  return requestJson<{ items: any[] }>(`/Items/${itemId}/IdentifyCandidates`);
}

export async function applyItemIdentifyCandidate(itemId: string, candidateId: string) {
  return requestJson<{ ok: boolean }>(`/Items/${itemId}/IdentifyCandidates/${candidateId}/Apply`, {
    method: 'POST',
  });
}

// Genres
export async function getGenres() {
  return request<any>('/Genres');
}

// System config
export async function getSystemConfig() {
  return request<any>('/System/Configuration');
}

export async function updateSystemConfig(config: Record<string, string>) {
  return request('/System/Configuration', {
    method: 'POST',
    body: JSON.stringify(config),
  });
}

export async function restartServer() {
  return request('/System/Restart', { method: 'POST' });
}

// Scrape config ------------------------------------------------------------

export type FieldPriorityMap = Record<string, string[]>;

export interface ScrapeDefaults {
  providers: string[];
  field_names: string[];
  default_policy: FieldPriorityMap;
}

export interface ScrapeConfigOverride {
  providers_enabled?: string[];
  provider_priority?: Record<string, number>;
  field_priority?: FieldPriorityMap;
  confidence_threshold?: number;
  auto_apply?: boolean;
}

export interface LibraryScrapeConfigResponse {
  inherit: boolean;
  override: ScrapeConfigOverride | null;
  effective: {
    ProvidersEnabled?: string[];
    ProviderPriority?: Record<string, number>;
    FieldPriority?: FieldPriorityMap;
    ConfidenceThreshold?: number;
    AutoApply?: boolean;
  };
}

export async function getScrapeDefaults() {
  return request<ScrapeDefaults>('/System/Config/Scrape/Defaults');
}

export async function getLibraryScrapeConfig(id: string) {
  return request<LibraryScrapeConfigResponse>(`/Library/${encodeURIComponent(id)}/ScrapeConfig`);
}

export async function updateLibraryScrapeConfig(
  id: string,
  body: { inherit: boolean; override: ScrapeConfigOverride | null },
) {
  return request(`/Library/${encodeURIComponent(id)}/ScrapeConfig`, {
    method: 'PUT',
    body: JSON.stringify(body),
  });
}

export async function shutdownServer() {
  return request('/System/Shutdown', { method: 'POST' });
}

// Favorites
export async function getFavoriteItems(limit = 20) {
  const userId = getUserId();
  return request<any>(`/Users/${userId}/Items?Filters=IsFavorite&Recursive=true&Limit=${limit}&SortBy=SortName&SortOrder=Ascending&IncludeItemTypes=Movie,Series`);
}

// Sessions / Activity
export async function getActiveSessions() {
  return request<any[]>('/Sessions');
}

// Playback Stats
export async function getUserActivity(days = 30) {
  return request<any[]>(`/user_usage_stats/user_activity?days=${days}`);
}

export async function getPlayActivity(days = 30) {
  return request<any[]>(`/user_usage_stats/PlayActivity?days=${days}`);
}

export async function getHourlyReport(days = 30) {
  return request<any[]>(`/user_usage_stats/HourlyReport?days=${days}`);
}

export async function getBreakdownReport(type: string, days = 30) {
  return request<any[]>(`/user_usage_stats/${type}/BreakdownReport?days=${days}`);
}

export async function getRecentPlayback(limit = 50) {
  return request<any[]>(`/user_usage_stats/RecentPlayback?limit=${limit}`);
}

// Scrape Progress
export async function getScrapeProgress() {
  return request<any>('/Library/Scrape/Progress');
}

export async function stopScrape() {
  return request<any>('/Library/Scrape/Stop', { method: 'POST' });
}

// Browse directories
export async function browseDirectories(path?: string) {
  const qs = path ? `?path=${encodeURIComponent(path)}` : '';
  return request<{ Path: string; Directories: { Name: string; Path: string }[] }>(`/Library/BrowseDirectories${qs}`);
}

// Emby Migration
export async function embyMigrate(users: { name: string; password: string }[], policy?: Record<string, any>) {
  return request<any>('/System/EmbyMigrate', {
    method: 'POST',
    body: JSON.stringify({ users, policy }),
  });
}

// Scan Progress
export async function getScanProgress() {
  return request<any>('/Library/Scan/Progress');
}

// Probe
export async function startProbe(threads: number = 5) {
  return request<any>('/Library/Probe/Start', { method: 'POST', body: JSON.stringify({ Threads: threads }) });
}

export async function stopProbe() {
  return request<any>('/Library/Probe/Stop', { method: 'POST' });
}

export async function getProbeProgress() {
  return request<any>('/Library/Probe/Progress');
}

// API Keys
export async function getApiKeys() {
  return request<any>('/ApiKeys');
}

export async function createApiKey(name: string) {
  return request<any>('/ApiKeys', {
    method: 'POST',
    body: JSON.stringify({ Name: name }),
  });
}

export async function deleteApiKey(id: string) {
  return request(`/ApiKeys/${id}`, { method: 'DELETE' });
}

// Logs
export async function getSystemLogs(level?: string, limit = 500) {
  const params = new URLSearchParams();
  if (level && level !== 'ALL') params.set('level', level);
  params.set('limit', String(limit));
  return request<any>(`/System/Logs?${params}`);
}

// Utils
function getBrowserId(): string {
  let id = localStorage.getItem('deviceId');
  if (!id) {
    id = Math.random().toString(36).substring(2) + Date.now().toString(36);
    localStorage.setItem('deviceId', id);
  }
  return id;
}

// Backup
export async function createBackup(categories: string[]) {
  return request<any>('/System/Backup', { method: 'POST', body: JSON.stringify({ categories }) });
}
export async function listBackups() {
  return request<any[]>('/System/Backups');
}
export async function deleteBackup(filename: string) {
  return request(`/System/Backups/${encodeURIComponent(filename)}`, { method: 'DELETE' });
}
export async function restoreBackup(filename: string, categories: string[]) {
  return request<any>('/System/Restore', { method: 'POST', body: JSON.stringify({ filename, categories }) });
}
export function getBackupDownloadUrl(filename: string) {
  return `/System/Backups/${encodeURIComponent(filename)}`;
}

// Library Sort Order
export async function updateLibrarySortOrder(orders: { Id: string; SortOrder: number }[]) {
  return request('/Library/VirtualFolders/SortOrder', { method: 'POST', body: JSON.stringify(orders) });
}

// Platform Libraries
export async function getPlatforms() {
  return requestJson<any>('/Library/Platforms');
}
export async function addPlatformLibrary(name: string) {
  return request('/Library/Platforms', { method: 'POST', body: JSON.stringify({ PlatformName: name }) });
}
export async function setPlatformEnable(name: string, enabled: boolean) {
  return request(`/Library/Platforms/${encodeURIComponent(name)}/${enabled ? 'Enable' : 'Disable'}`, { method: 'POST' });
}
export async function deletePlatformLibrary(id: string) {
  return request(`/Library/Platforms/${id}`, { method: 'DELETE' });
}
export async function scanPlatformStudios() {
  return requestJson<any>('/Library/Platforms/Scan', { method: 'POST' });
}
export async function scanPlatformByFilename() {
  return requestJson<any>('/Library/Platforms/ScanFilename', { method: 'POST' });
}
export async function rescrapeMissingStudio() {
  return requestJson<any>('/Library/Platforms/Rescrape', { method: 'POST' });
}
export async function getRescrapeProgress() {
  return requestJson<any>('/Library/Platforms/Rescrape/Progress');
}

export async function getTaskSummary() {
  return requestJson<any>('/Library/Tasks/Summary');
}

// ---- M7 Backfill ----
export type BackfillStage = 'quality' | 'name' | 'image';

export async function startBackfill(stages?: BackfillStage[]) {
  return request<any>('/Library/Backfill/Start', {
    method: 'POST',
    body: JSON.stringify(stages && stages.length > 0 ? { stages } : {}),
  });
}
export async function stopBackfill() {
  return request<any>('/Library/Backfill/Stop', { method: 'POST' });
}
export async function getBackfillProgress() {
  return requestJson<any>('/Library/Backfill/Progress');
}
export async function getBackfillConfig() {
  return requestJson<any>('/Library/Backfill/Config');
}
export async function updateBackfillConfig(body: { enabled_on_startup?: boolean; episode_still_fetch?: boolean }) {
  return request<any>('/Library/Backfill/Config', { method: 'POST', body: JSON.stringify(body) });
}
export async function resetBackfillQuality() {
  return request<any>('/Library/Backfill/Reset/Quality', { method: 'POST' });
}
export async function resetBackfillEpisodeImage() {
  return request<any>('/Library/Backfill/Reset/EpisodeImage', { method: 'POST' });
}

// ---- Phase 5: 队列面板 / metrics / 动态 worker / 缓存 invalidate ----

export type ScrapeQueueStats = {
  pending: number;
  running: number;
  done: number;
  failed: number;
};

export type ScrapeQueueTask = {
  id: number;
  item_id: string;
  item_name: string;
  item_type: string;
  file_path?: string;
  series_name?: string;
  index_number?: number;
  parent_index_number?: number;
  task_type: string;
  status: string;
  priority: number;
  retry_count: number;
  last_error?: string;
  next_run_at: string;
  updated_at: string;
};

export type ScrapeQueueTaskDetail = ScrapeQueueTask & {
  request_url?: string;
  response_status?: number;
  response_sample?: string;
};

export type MetricsSnapshot = {
  ingest_channel_depth?: number;
  ingest_overflow_total?: number;
  ingest_worker_count?: number;
  scrape_pending?: number;
  scrape_running?: number;
  scrape_failed?: number;
  scrape_done?: number;
  scrape_worker_count?: number;
  tmdb_requests_total?: number;
};

export async function getScrapeQueueStats() {
  return requestJson<ScrapeQueueStats>('/Admin/ScrapeQueue/Stats');
}

export async function getScrapeQueueRecent(opts: { status?: 'failed' | 'running' | 'pending' | ''; limit?: number; offset?: number } = {}) {
  const params = new URLSearchParams();
  if (opts.status) params.set('status', opts.status);
  if (opts.limit != null) params.set('limit', String(opts.limit));
  if (opts.offset != null) params.set('offset', String(opts.offset));
  const qs = params.toString();
  return requestJson<{ tasks: ScrapeQueueTask[]; total: number }>(`/Admin/ScrapeQueue/Recent${qs ? '?' + qs : ''}`);
}

export async function getScrapeQueueTaskDetail(id: number) {
  return requestJson<ScrapeQueueTaskDetail>(`/Admin/ScrapeQueue/Task/${id}`);
}

export async function retryScrapeQueueTask(id: number) {
  return request<any>(`/Admin/ScrapeQueue/Retry/${id}`, { method: 'POST' });
}

export async function retryAllFailedScrapeQueueTasks() {
  return requestJson<{ reset: number }>('/Admin/ScrapeQueue/RetryAllFailed', { method: 'POST' });
}

export async function getMetricsSnapshot() {
  return requestJson<MetricsSnapshot>('/Admin/Metrics/Snapshot');
}

export async function invalidateScrapeCache() {
  return request<any>('/Admin/Scrape/Cache/Invalidate', { method: 'POST' });
}

export async function setIngestWorkerCount(count: number) {
  return requestJson<{ count: number }>('/Admin/Ingest/Workers', {
    method: 'POST',
    body: JSON.stringify({ count }),
  });
}

export async function setScrapeWorkerCount(count: number) {
  return requestJson<{ count: number }>('/Admin/Scrape/Workers', {
    method: 'POST',
    body: JSON.stringify({ count }),
  });
}

export { getToken, getUserId };
