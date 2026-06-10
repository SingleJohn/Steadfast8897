> **实现状态总览（FYMS 调研，2026-06-10）**：23 条标准接口 **全部已实现**。原唯一缺口 `GET /emby/Items/{itemId}`（非 user 维度）已于本次补齐（`library.go:24`，见 §8.1）。剩 1 处行为差异：`GET /System/Info` 不校验 token（恒 200），不影响 EM 用其做"在线探活/版本读取"，但若 EM 依赖它来"验证 apiKey 有效性"则形同虚设。图例：✅ 已实现 ／ ⚠️ 已实现但有差异 ／ ❌ 未实现。

## 1. System（系统探活）

| Method | Path | 用途 | Auth | 状态 |
|---|---|---|---|---|
| GET | `/emby/System/Ping` | 探活；2xx 且返回正文 `Emby Server` 即在线 | 无 | ✅ `system.go:120`（GET+POST，返回 `Emby Server`） |
| GET | `/emby/System/Info` | 取服务器版本/架构等元信息，同时验证 apiKey 有效性 | `api_key` | ⚠️ `system.go:115`（路由无鉴权中间件，恒返回 200；版本号对 Emby 官方客户端伪装为 4.7.14。**不校验 apiKey 有效性**） |

## 2. Users（用户）

### 2.1 查询

| Method | Path | 用途 | Auth | 状态 |
|---|---|---|---|---|
| GET | `/emby/Users` | 列出所有用户（admin） | `api_key` | ✅ `users.go:196`（authMW） |
| GET | `/emby/Users/Public` | 列出公开用户（无需 admin 权限即可查） | `api_key`（仍带，用于代理） | ✅ `users.go:195`（无鉴权） |
| GET | `/emby/Users/{userId}` | 取单用户详情（姓名、最后登录、IsDisabled 等） | `api_key` | ✅ `users.go:210`（authMW） |
| GET | `/emby/Users/{templateUserId}` | 取模板用户，复制其 Policy 到新建账号 | `api_key` | ✅ 同上接口复用（含 Policy 字段） |


### 2.2 创建 / 删除

| Method | Path | 用途 | Body | 状态 |
|---|---|---|---|---|
| POST | `/emby/Users/New` | 创建用户 | `{ "Name": "<username>" }` | ✅ `users.go:199`（adminMW） |
| DELETE | `/emby/Users/{userId}` | 删除用户 | — | ✅ `users.go:212`（adminMW） |


> 创建用户的 Body 仅传 `Name`，密码通过随后的 `/Password` 接口单独设置（标准 Emby 行为）。

### 2.3 鉴权 / 密码

| Method | Path | 用途 | Body | 状态 |
|---|---|---|---|---|
| POST | `/emby/Users/{userId}/Authenticate` | 用密码换 AccessToken | `{ "Pw": "<password>" }` | ✅ `users.go:215`（optAuthMW） |
| POST | `/emby/Users/{userId}/Password` | 设置/重置密码 | `{ "NewPw": "<new>", "ResetPassword": <bool>, "CurrentPw"?: "<old>" }` | ✅ `users.go:213`（authMW） |


### 2.4 Policy（权限）

| Method | Path | 用途 | 状态 |
|---|---|---|---|
| POST | `/emby/Users/{userId}/Policy` | 整体覆盖用户 Policy（IsAdministrator / IsDisabled / EnableContentDownloading / EnabledFolders / 等） | ✅ `users.go:214`（adminMW，`UpdatePolicy`） |

**Body**：原样回填从 `GET /Users/{templateUserId}` 取来的 Policy，仅按需要覆盖少量字段。EM 主要改写的字段有：

- `IsDisabled` — 启用/停用
- `IsAdministrator` — admin 标记
- `EnableContentDownloading` — 是否允许下载
- `EnabledFolders` — 该用户可见的媒体库 Id 列表
- `EnableAllFolders` — true 时忽略 EnabledFolders
- `MaxActiveSessions` — 同时观看数限制
- `RemoteClientBitrateLimit` — 远程客户端码率上限

---

## 3. Library / Items（媒体库 & 媒体）

| Method | Path | 用途 | 关键 Query | 状态 |
|---|---|---|---|---|
| GET | `/emby/Library/VirtualFolders` | 列出所有媒体库（admin 视角，含 ItemId、Path、CollectionType） | `api_key` | ✅ `library.go:43` / `library_refresh.go:114` |
| GET | `/emby/Users/{userId}/Views` | 列出某用户可见的媒体库（受 EnabledFolders 影响） | `api_key` | ✅ `library.go:9`（含实际库+虚拟库混排） |
| GET | `/emby/Items` | 管理端查询条目（不受用户文件夹限制） | `ParentId`, `Recursive`, `Fields`, `SortBy`, `SortOrder`, `Limit`, `StartIndex`, `IncludeItemTypes` | ✅ `compat.go:42`（`itemsSearch`） |
| GET | `/emby/Users/{userId}/Items` | 用户视角查询条目（自动套 EnabledFolders） | 同上 + `searchTerm`, `IncludeMedia`, `ImageTypeLimit` | ✅ `library.go:10`（`getItems`） |
| GET | `/emby/Items/{itemId}` | 取单条目元数据 | `api_key` | ✅ `library.go:24`（复用 `getItemDetail`，`resolveUserID` 回退当前 token 用户）。详见 §8.1 |
| GET | `/emby/Items/{itemId}/Images/{type}` | 取条目图片（Primary / Backdrop / Logo / ...） | `maxHeight`（可选） | ✅ `images.go:53`（支持 `:imageIndex`、maxHeight、缓存） |
| GET | `/emby/Items/{itemId}/PlaybackInfo` | 取 MediaSources / 转码档位（播放前必经一步） | `api_key`、可选 `UserId` | ✅ `videos.go:30`（GET+POST） |

---

## 4. Sessions / Playback（会话 & 进度上报）

| Method | Path | 用途 | Body | 状态 |
|---|---|---|---|---|
| GET | `/emby/Sessions` | 列出当前活跃会话（轮询所有用户的播放状态） | `api_key` | ✅ `compat.go:11`（adminMW，`getSessions`） |
| POST | `/emby/Sessions/Playing` | 标记会话开始 | `{ ItemId, PositionTicks, IsPaused, PlaySessionId, MediaSourceId }` | ✅ `playback.go:183`（`OnPlaybackStart`） |
| POST | `/emby/Sessions/Playing/Progress` | 进度上报（默认 endpoint） | 同上 | ✅ `playback.go:184`（`OnPlaybackProgress`） |
| POST | `/emby/Sessions/Playing/Stopped` | 标记会话结束 | 同上 | ✅ `playback.go:185`（`OnPlaybackStopped`） |

> EM 在 `controllers/emby.ts:4949` 根据 `eventName ∈ {start, progress, stop}` 动态拼最后一段。

---

## 5. Videos / Streaming（视频流）

| Method | Path | 用途 | 关键 Query / Header | 状态 |
|---|---|---|---|---|
| GET | `/emby/Videos/{itemId}/stream.mp4` | 强制 MP4 容器流（远端 remux） | `static=true`、`MediaSourceId`、`PlaySessionId`、`DeviceId`；Header `X-Emby-Token` 或 query `api_key` | ✅ `videos.go:37`（`stream.:container` 通配，`streamVideo`）。注意：FYMS 铁律为**永远直出不转码/不 remux**，命中本地文件走 302 直链/直传 |
| GET | `/emby/Videos/{itemId}/stream` | 直传流（无转码） | 同上 | ✅ `videos.go:36`（`streamVideo`） |


> 这两条路径用于"EM 充当流代理"的场景；前端浏览器实际命中的是 EM 暴露的 `/api/emby/stream/{serverId}/{itemId}`，EM 再用上面这两条 URL 回源到 Emby。

---

## 6. 一些重要约定

1. **鉴权方式**：标准 Emby 同时接受 `?api_key=` 与 `X-Emby-Token` Header。EM 几乎所有调用走 `?api_key=`；只有 `/emby/System/Info` (`utils/embyPing.ts:22`) 和流媒体回源用 Header，以便在被反代/CDN 缓存时保持 URL 一致。
2. **路径前缀必须带 `/emby/`**：EM 全部硬编码 `/emby/...`，不依赖服务器侧的 `/` 重定向。如果用户配置的是非标准前缀（极少见），需要在 `server.url` 中包含。
3. **超时**：所有 fetch 由 `proxiedFetch` 包装；后台轮询/探活默认 3 s，用户写操作默认 30 s（部分到 60 s）。
4. **404 语义**：
   - `GET /Users/{id}` 返回 404 → 直接视为账号已不在 Emby 侧。
   - `DELETE /Users/{id}` 返回 404 → 视为成功（已删过）。
5. **创建账号的固定流程**（EM 多处复用）：
   1. `GET /emby/Users/{templateUserId}` 取模板 Policy。
   2. `GET /emby/Users` 或 `GET /emby/Users/Public` 查重名。
   3. `POST /emby/Users/New` 仅传 Name。
   4. `POST /emby/Users/{newId}/Password` 设密码。
   5. `POST /emby/Users/{newId}/Policy` 套模板 Policy（叠加本用户的 EnabledFolders / IsDisabled 等覆盖）。
   失败任意一步会 `DELETE /emby/Users/{newId}` 回滚。
6. **区分 EA vs 标准 Emby**：EM 在 `server.serverType === 'EMBY_API'` 时走 EA 自有接口（如 `/api/em/permission`、`/api/em/sync-config`、`/health` 等，本文件不收录）；其他类型走本文件所列标准接口。少数老接口（GET/POST `/emby/Users/*`）EA 也兼容实现，因此 EM 偶尔同一段代码对两类服务器都生效——这种情况在本文件按"标准 Emby 接口"语义列出即可。


## 7. 标准 Emby HTTP 接口汇总速查表

| # | Method | Path | 模块 | 状态 |
|---|---|---|---|---|
| 1 | GET | `/emby/System/Ping` | System | ✅ |
| 2 | GET | `/emby/System/Info` | System | ⚠️ 不校验 apiKey |
| 3 | GET | `/emby/Users` | Users | ✅ |
| 4 | GET | `/emby/Users/Public` | Users | ✅ |
| 5 | GET | `/emby/Users/{userId}` | Users | ✅ |
| 6 | POST | `/emby/Users/New` | Users | ✅ |
| 7 | DELETE | `/emby/Users/{userId}` | Users | ✅ |
| 8 | POST | `/emby/Users/{userId}/Authenticate` | Users | ✅ |
| 9 | POST | `/emby/Users/{userId}/Password` | Users | ✅ |
| 10 | POST | `/emby/Users/{userId}/Policy` | Users | ✅ |
| 11 | GET | `/emby/Users/{userId}/Views` | Library | ✅ |
| 12 | GET | `/emby/Library/VirtualFolders` | Library | ✅ |
| 13 | GET | `/emby/Items` | Library | ✅ |
| 14 | GET | `/emby/Users/{userId}/Items` | Library | ✅ |
| 15 | GET | `/emby/Items/{itemId}` | Library | ✅ |
| 16 | GET | `/emby/Items/{itemId}/Images/{type}` | Library | ✅ |
| 17 | GET | `/emby/Items/{itemId}/PlaybackInfo` | Library | ✅ |
| 18 | GET | `/emby/Sessions` | Sessions | ✅ |
| 19 | POST | `/emby/Sessions/Playing` | Sessions | ✅ |
| 20 | POST | `/emby/Sessions/Playing/Progress` | Sessions | ✅ |
| 21 | POST | `/emby/Sessions/Playing/Stopped` | Sessions | ✅ |
| 22 | GET | `/emby/Videos/{itemId}/stream.mp4` | Streaming | ✅ |
| 23 | GET | `/emby/Videos/{itemId}/stream` | Streaming | ✅ |

---

## 8. 缺口与方案

### 8.1 ✅ `GET /emby/Items/{itemId}`（标准非 user 维度单条目）— 已落地

**变更**：已在 `library.go:24` 注册 `u.GET("/Items/:itemId", authMW, getItemDetail)`，复用现有 handler。

**改动前**：FYMS 仅注册了 `GET /Users/:userId/Items/:itemId`（→ `getItemDetail`）。标准 Emby 的 `GET /Items/{itemId}`（不带 userId、仅凭 token）没有路由，EM 调用会落到前端 SPA fallback 返回 HTML，而非条目 JSON。

**为何此前没暴露问题**：Infuse / Yamby / 多数 Emby 协议客户端取详情走的是 user 维度的 `/Users/{userId}/Items/{itemId}`，所以一直够用；EM 这类管理/代理工具才会直接打非 user 维度的 `/Items/{itemId}`。

**关键利好**：`getItemDetail` 内部用 `resolveUserID(c)`（`library_auth.go:22`）——当 path 上没有 `:userId` 时会**自动回退到当前已认证用户**。因此现有 handler 无需改造即可服务无 userId 的请求。

**方案（最小改动，一行路由）**：在 `RegisterLibraryRoutes`（`library.go`）的 `/Items/:itemId/...` 路由附近，追加终端路由：

```go
// 标准 Emby 单条目（无 user 维度）：EM 等管理端依赖。
// getItemDetail 内部 resolveUserID 已回退到当前 token 用户。
u.GET("/Items/:itemId", authMW, getItemDetail)
```

**注意点**：
1. **路由冲突**：同层级已存在静态 `/Items/Counts`、`/Items`（无参）与参数 `/Items/:itemId/Images/...`、`/Items/:itemId/PlaybackInfo` 并存且未 panic，说明本工程 Gin 版本支持静态+参数混用，新增终端 `/Items/:itemId` 安全。
2. **鉴权语义**：标准 Emby 该接口凭 token 即可（非 admin）。挂 `authMW`、复用 `getItemDetail` 内的 `matchUserOrAdmin` 即与现有 user 维度行为一致。
3. **隐藏路径中间件**：`RegisterLibraryRoutes` 注册在 `browse` 组（`HideMediaPaths`），非 admin 会隐藏物理路径——与现有详情接口一致，符合预期。
4. **可选对齐**：若要 100% 贴近 Emby，可后续补 `POST /Items/{itemId}`（部分客户端用 POST 提交字段更新），但 EM 抓包仅用 GET，本期不做。

### 8.2 ⚠️ `GET /System/Info` 不校验 apiKey（行为差异，非阻塞）

**现状**：路由 `system.go:115` 未挂任何鉴权中间件，无论 token 是否有效都返回 200 + 完整 Info。

**影响**：EM 注释称用此接口"同时验证 apiKey 有效性"。FYMS 下该校验恒为通过——只要服务器在线，EM 永远认为 token 有效。探活与版本读取不受影响。

**是否需要改**：
- **保持现状（推荐）**：标准 Emby 的 `/System/Info`（非 `/System/Info/Public`）本身也是面向已登录会话返回更详细信息；很多三方客户端用它做无 token 探活。贸然加鉴权可能破坏其他客户端。
- **若确需校验**：可改为挂 `optAuthMW` 并在带了 `api_key`/`X-Emby-Token` 但无效时返回 401（不带 token 仍放行降级为 public 信息）。属于增强，建议确认 EM 真实依赖后再做，避免回归。
