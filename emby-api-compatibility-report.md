# FYMS Go v1.3.6-1 Emby API 兼容性测试报告

**测试日期**: 2026-04-06
**测试地址**: http://43.248.133.226:8988
**对照文件**: emby.py (Sakura BOT)

---

## 测试结果总览

| 状态 | 数量 |
|------|------|
| 完全通过 | 10 |
| 部分通过（需修复） | 5 |
| 未实现 | 1 |

---

## 完全通过的接口

### 1. `GET /emby/Users` — 获取所有用户
- **结果**: OK, 返回 3 个用户，字段齐全（Id, Name, Policy 等）

### 2. `POST /emby/Users/New` — 创建用户
- **结果**: OK（需带 Password 字段）
- **注意**: FYMS 要求 Password 必填，Emby 原版只需 Name。**bot 侧需加 Password 参数**

### 3. `POST /emby/Users/{id}/Password` — 设置/重置密码
- **结果**: OK (HTTP 204)
- ResetPassword=true 和 NewPw 两种模式均通过

### 4. `DELETE /emby/Users/{id}` — 删除用户
- **结果**: OK (HTTP 204)

### 5. `POST /emby/Users/AuthenticateByName` — 登录验证
- **结果**: OK, 返回 AccessToken + User 对象

### 6. `GET /emby/Sessions` — 获取活跃会话
- **结果**: OK, 返回 2 个会话，NowPlayingItem 字段正常

### 7. `GET /emby/Items/Counts` — 媒体数量统计
- **结果**: OK — Movies=13364, Series=5960, Episodes=210667

### 8. `GET /emby/Devices/Info?Id=` — 设备信息
- **结果**: OK, 无活跃设备时返回 Unknown

### 9. `POST /emby/Users/{id}/FavoriteItems/{itemId}` — 添加收藏
- **结果**: OK (HTTP 200)

### 10. `POST /emby/user_usage_stats/submit_custom_query` — 自定义SQL查询
- **结果**: OK, 返回 columns + results
- **注意**: emby.py 使用 SQLite 语法（substr/instr），FYMS 已做部分转换，但 `substr(x, 0, instr(x, ' - '))` 等复杂语法可能不兼容

---

## 部分通过（需修复）

### 11. `POST /emby/Users/{id}/Policy` — 设置用户策略
- **HTTP**: 200 OK
- **问题**: `IsHidden` 设为 true 但查询返回 false，`BlockedMediaFolders` 和 `EnabledFolders` 字段未生效
- **影响**: bot 的 `emby_create`（设置隐藏）、`emby_block`（屏蔽媒体库）、`hide_folders_by_names` / `show_folders_by_names` 全部受影响
- **适配方案**:
  - Policy POST 处理中需支持 `IsHidden`、`IsHiddenRemotely` 写入 users 表
  - 支持 `BlockedMediaFolders`（媒体库名称数组）和 `EnabledFolders`（媒体库 ID 数组）写入 user_policies
  - GET 用户时返回完整 Policy 含这些字段

### 12. `GET /emby/Users/{id}` — 获取用户信息
- **HTTP**: 200 OK
- **问题**: Policy 中缺少 `EnableAllFolders`、`BlockedMediaFolders`、`EnabledFolders` 字段
- **影响**: bot 的 `get_current_enabled_folder_ids` 读取这些字段管理权限
- **适配方案**: 同上 Policy 修复

### 13. `GET /emby/Library/VirtualFolders` — 媒体库列表
- **HTTP**: 200 OK
- **Guid 字段**: **已有** (与 ItemId 相同)
- **问题**: bot 使用 `lib['Guid']` 和 `lib['Name']` 匹配，已兼容
- **状态**: 基本通过，无需修改

### 14. `GET /emby/Items?SearchTerm=&Fields=...` — 搜索含详细字段
- **HTTP**: 200 OK, 搜索功能正常
- **缺失字段**:
  - `OriginalTitle` — 未返回（数据库有但 dto 未输出）
  - `Taglines` — 未返回（数据库有 tagline 字段）
  - `Genres` — 未返回（需 JOIN genres 表）
  - `DateCreated` — 未返回（数据库有 created_at）
  - `Studios` — 未返回（新增的 studio 字段）
  - `People` — 返回类型错误（字符串而非数组）
  - `ProductionLocations` — 可忽略，返回空数组即可
- **影响**: bot 的 `get_movies` 搜索结果展示不完整
- **适配方案**: 在 `itemsSearch` 和 `FormatItemDto` 中补全这些字段的输出

### 15. `GET /emby/Items?Ids=&Fields=People` — 获取演员信息
- **HTTP**: 200 OK
- **问题**: People 字段返回了字符串而非数组对象
- **影响**: bot 的 `item_id_people` 无法正确解析演员列表
- **适配方案**: `itemsSearch` 中当 Fields 含 People 时，查 cast_members 表返回 `[{Name, Role, Type, Id, PrimaryImageTag}]` 数组

---

## 未实现

### 16. `GET /emby/Users/Query?NameStartsWithOrGreater=` — 按名称搜索用户
- **HTTP**: 404
- **影响**: bot 的 `get_emby_user_by_name` 无法按用户名查找用户
- **适配方案**: 在 compat.go 新增路由：
  ```
  GET /Users/Query?NameStartsWithOrGreater={name}
  ```
  查 users 表 `WHERE name >= $1 ORDER BY name`，返回 `{Items: [...], TotalRecordCount: N}`

---

## 特殊兼容性问题

### submit_custom_query SQL 语法差异

emby.py 的 SQL 使用 SQLite 语法，FYMS 是 PostgreSQL。已有转换：
- `strftime('%Y-%m-%d', x)` → `TO_CHAR(x, 'YYYY-MM-DD')`
- `datetime('now', '-N days')` → `(NOW() - INTERVAL 'N days')`
- `rowid` → `id`

**尚未转换**:
- `substr(x, 0, instr(x, ' - '))` → 需转为 `split_part(x, ' - ', 1)`
- `PlaybackActivity` 表名大小写 — 已处理（加引号）
- `UserList` 子查询 — FYMS 无此表，需创建视图或过滤管理员
- `PlayDuration - PauseDuration` — 需确认 FYMS 的 playback_activity 表是否有对应字段

### Users/New Password 要求

FYMS 创建用户必须提供密码，Emby 原版可不提供。bot 的 `emby_create` 流程是先创建再设密码。

**适配方案**: FYMS 的 `CreateUser` 允许 Password 为空时设置随机密码或空密码

---

## 适配优先级

### P0 (必须修复 — 核心功能不可用)
1. **Users/New 允许空密码** — bot 创建用户流程依赖
2. **Policy 完整支持** — IsHidden, BlockedMediaFolders, EnabledFolders
3. **Users/Query** — bot 按名称查用户

### P1 (重要 — 功能残缺)
4. **Items 搜索字段补全** — OriginalTitle, Taglines, Genres, DateCreated, Studios
5. **People 字段返回数组** — 演员信息展示
6. **submit_custom_query SQL 转换补全** — 统计报表

### P2 (低优 — 不影响主流程)
7. **ProductionLocations** — 返回空数组即可
8. **Sessions/Playing/Stop + Message** — 已实现，正常工作
