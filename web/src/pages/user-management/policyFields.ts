// 用户权限策略字段的单一事实来源。
// 用户管理页、批量改策略弹窗、Emby 迁移统一策略都从这里取定义，
// 保证三处的字段、标签、默认值完全一致（避免迁移策略与用户策略"对不上"）。
//
// 注意：所有 key 必须与后端 models.PolicyUpdate 的 JSON tag 完全一致（PascalCase / Emby 风格），
// 这些 key 会原样作为 /Users/:id/Policy 和 /System/EmbyMigrate 的请求体字段名。

export interface PolicyState {
  IsAdministrator: boolean
  IsDisabled: boolean
  IsHidden: boolean
  EnableAllFolders: boolean
  EnableRemoteAccess: boolean
  EnableMediaPlayback: boolean
  EnableAudioPlaybackTranscoding: boolean
  EnableVideoPlaybackTranscoding: boolean
  EnablePlaybackRemuxing: boolean
  EnableContentDeletion: boolean
  EnableContentDownloading: boolean
  EnableSubtitleManagement: boolean
  EnableLiveTvAccess: boolean
  EnableLiveTvManagement: boolean
  EnableUserPreferenceAccess: boolean
  EnableRemoteControlOfOtherUsers: boolean
  EnableSharedDeviceControl: boolean
  RemoteClientBitrateLimit: number
  SimultaneousStreamLimit: number
  BlockedMediaFolders: string[]
  EnabledFolders: string[]
}

export type PolicyKey = keyof PolicyState

export interface ToggleDef {
  key: PolicyKey
  label: string
  desc?: string
}

export const adminToggles: ToggleDef[] = [
  { key: 'IsAdministrator', label: '管理员', desc: '拥有所有设置和内容的完全访问权限' },
  { key: 'IsDisabled', label: '禁用此用户', desc: '被禁用的用户无法登录' },
  { key: 'IsHidden', label: '在登录页面隐藏', desc: '隐藏的用户需要手动输入用户名' },
  { key: 'EnableUserPreferenceAccess', label: '管理个人偏好设置' },
]

export const playbackToggles: ToggleDef[] = [
  { key: 'EnableMediaPlayback', label: '允许媒体播放' },
  { key: 'EnableAudioPlaybackTranscoding', label: '允许音频转码播放' },
  { key: 'EnableVideoPlaybackTranscoding', label: '允许视频转码播放' },
  { key: 'EnablePlaybackRemuxing', label: '允许播放重新封装' },
]

export const featureToggles: ToggleDef[] = [
  { key: 'EnableContentDeletion', label: '允许删除媒体' },
  { key: 'EnableContentDownloading', label: '允许下载内容' },
  { key: 'EnableSubtitleManagement', label: '允许字幕管理' },
  { key: 'EnableLiveTvAccess', label: '允许访问电视直播' },
  { key: 'EnableLiveTvManagement', label: '允许管理电视直播' },
]

export const remoteToggles: ToggleDef[] = [
  { key: 'EnableRemoteAccess', label: '允许远程连接' },
  { key: 'EnableRemoteControlOfOtherUsers', label: '允许远程控制其他用户' },
  { key: 'EnableSharedDeviceControl', label: '允许远程控制共享设备' },
]

// 分组结构，供批量改策略弹窗 / 迁移统一策略表单遍历渲染。
export interface ToggleGroup {
  title: string
  toggles: ToggleDef[]
}

export const policyGroups: ToggleGroup[] = [
  { title: '播放权限', toggles: playbackToggles },
  { title: '功能权限', toggles: featureToggles },
  { title: '远程访问', toggles: remoteToggles },
]

export const streamLimitOptions = [0, 1, 2, 3, 4, 5, 6, 8, 10].map(n => ({
  label: n === 0 ? '不限制' : String(n),
  value: n,
}))

export const permissionTemplates = [
  { label: '标准用户', value: 'standard' },
  { label: '只读观影', value: 'readonly' },
  { label: '访客受限', value: 'guest' },
]

// 模板差异补丁：返回相对"标准用户"需要覆盖的字段。standard 返回 null（不覆盖）。
export function templatePatch(key: string): Partial<PolicyState> | null {
  if (key === 'readonly') {
    return {
      EnableContentDeletion: false,
      EnableContentDownloading: false,
      EnableSubtitleManagement: false,
      EnableLiveTvManagement: false,
      EnableRemoteControlOfOtherUsers: false,
      EnableSharedDeviceControl: false,
    }
  }
  if (key === 'guest') {
    return {
      IsAdministrator: false,
      EnableAllFolders: false,
      EnabledFolders: [],
      EnableRemoteAccess: false,
      EnableContentDeletion: false,
      EnableContentDownloading: false,
      EnableSubtitleManagement: false,
      EnableLiveTvManagement: false,
      EnableRemoteControlOfOtherUsers: false,
      EnableSharedDeviceControl: false,
      SimultaneousStreamLimit: 1,
    }
  }
  return null
}

// 迁移 / 新建场景的完整默认策略（全字段，可直接整体下发）。
export function defaultFullPolicy(): PolicyState {
  return {
    IsAdministrator: false,
    IsDisabled: false,
    IsHidden: false,
    EnableAllFolders: true,
    EnableRemoteAccess: true,
    EnableMediaPlayback: true,
    EnableAudioPlaybackTranscoding: true,
    EnableVideoPlaybackTranscoding: true,
    EnablePlaybackRemuxing: true,
    EnableContentDeletion: false,
    EnableContentDownloading: true,
    EnableSubtitleManagement: false,
    EnableLiveTvAccess: false,
    EnableLiveTvManagement: false,
    EnableUserPreferenceAccess: true,
    EnableRemoteControlOfOtherUsers: false,
    EnableSharedDeviceControl: false,
    RemoteClientBitrateLimit: 0,
    SimultaneousStreamLimit: 0,
    BlockedMediaFolders: [],
    EnabledFolders: [],
  }
}
