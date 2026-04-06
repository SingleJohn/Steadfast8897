# FYMS Emby API 适配方案

**对照**: emby-api-compatibility-report.md
**目标**: 让 Sakura BOT (emby.py) 完整对接 FYMS

---

## P0: 必须修复

### 1. Users/New 允许空密码

**现状**: `CreateUser` 要求 Password 必填，bot 先创建再单独设密码
**文件**: `internal/handlers/users.go`

```go
// 修改 CreateUser 函数 (约 L251)
// 原:
if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" || body.Password == "" {

// 改为:
if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
    ...
}
// Password 为空时生成随机密码或空字符串
password := body.Password
if password == "" {
    password = uuid.New().String()[:12] // 随机密码，后续通过 Password 接口重置
}
u, err := models.CreateUser(ctx, st.DB, body.Name, password, false)
```

### 2. Policy 完整支持 IsHidden / BlockedMediaFolders / EnabledFolders

**现状**: POST Policy 只更新 user_policies 表的部分字段，不支持 IsHidden、BlockedMediaFolders、EnabledFolders
**涉及文件**:
- `migrations/021_policy_folders.sql` (新建)
- `internal/models/user.go`
- `internal/handlers/users.go`

#### 2a. 数据库迁移

```sql
-- migrations/021_policy_folders.sql
ALTER TABLE user_policies ADD COLUMN IF NOT EXISTS blocked_media_folders TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE user_policies ADD COLUMN IF NOT EXISTS enabled_folders TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE user_policies ADD COLUMN IF NOT EXISTS enable_all_folders BOOLEAN NOT NULL DEFAULT true;
```

#### 2b. models/user.go — UpsertUserPolicy 扩展

在 `UpsertUserPolicy` 中增加对以下字段的处理:
- `IsHidden` / `IsHiddenRemotely` → 写入 users 表的 `is_hidden` 字段
- `IsDisabled` → 写入 users 表的 `is_disabled` 字段
- `BlockedMediaFolders` → 写入 user_policies.blocked_media_folders
- `EnabledFolders` → 写入 user_policies.enabled_folders
- `EnableAllFolders` → 写入 user_policies.enable_all_folders

#### 2c. handlers/users.go — Policy POST

```go
// 在处理 Policy POST 时，额外处理这些字段:
if isHidden, ok := body["IsHidden"].(bool); ok {
    pool.Exec(ctx, "UPDATE users SET is_hidden = $1 WHERE id = $2::uuid", isHidden, userID)
}
if isDisabled, ok := body["IsDisabled"].(bool); ok {
    pool.Exec(ctx, "UPDATE users SET is_disabled = $1 WHERE id = $2::uuid", isDisabled, userID)
}
// BlockedMediaFolders, EnabledFolders, EnableAllFolders 写入 user_policies
```

#### 2d. GET 用户时返回完整 Policy

在 `buildUserResponse` / `FormatPolicyResponse` 中补充返回:
```json
{
  "Policy": {
    "EnableAllFolders": true,
    "BlockedMediaFolders": ["播放列表"],
    "EnabledFolders": ["guid1", "guid2"],
    "IsHidden": true,
    "IsHiddenRemotely": true,
    "IsDisabled": false,
    ...
  }
}
```

### 3. Users/Query — 按名称搜索用户

**现状**: 404
**文件**: `internal/handlers/compat.go`

```go
// RegisterCompatRoutes 中新增:
group.GET("/Users/Query", authMW, func(c *gin.Context) {
    nameFilter := c.Query("NameStartsWithOrGreater")
    if nameFilter == "" {
        nameFilter = c.Query("nameStartsWithOrGreater")
    }

    ctx := c.Request.Context()
    rows, err := state.DB.Query(ctx,
        "SELECT id, name, is_admin, is_disabled, is_hidden FROM users WHERE name >= $1 ORDER BY name LIMIT 50",
        nameFilter)
    // ... 遍历构建用户列表，每个用户调 buildUserResponse
    c.JSON(200, gin.H{"Items": items, "TotalRecordCount": len(items)})
})
```

---

## P1: 重要修复

### 4. Items 搜索返回字段补全

**现状**: `itemsSearch` (compat.go) 返回的 item 缺少多个 bot 需要的字段
**文件**:
- `internal/dto/types.go` — BaseItemDto 加字段
- `internal/dto/format.go` — FormatItemDto 补全输出
- `internal/handlers/compat.go` — itemsSearch 补充查询

#### 4a. dto/types.go 新增字段

```go
type BaseItemDto struct {
    // ... 现有字段 ...
    OriginalTitle       *string   `json:"OriginalTitle,omitempty"`
    Taglines            []string  `json:"Taglines,omitempty"`
    DateCreated         *string   `json:"DateCreated,omitempty"`
    Studios             []string  `json:"Studios,omitempty"`       // 从 items.studio 读取
    ProductionLocations []string  `json:"ProductionLocations,omitempty"` // 返回空数组
}
```

#### 4b. dto/format.go 补全

```go
func FormatItemDto(row *ItemRow, serverID string, ud *UserDataRow) BaseItemDto {
    // ... 现有逻辑 ...

    // 补充字段
    if row.Tagline != nil && *row.Tagline != "" {
        d.Taglines = []string{*row.Tagline}
    }
    if row.CreatedAt != nil {
        t := row.CreatedAt.UTC().Format("2006-01-02T15:04:05.0000000Z")
        d.DateCreated = &t
    }
    if row.Studio != nil && *row.Studio != "" {
        d.Studios = []string{*row.Studio}
    }
    d.ProductionLocations = []string{}
}
```

#### 4c. ItemRow 扩展

需在 `dto/types.go` 的 `ItemRow` 中添加:
```go
type ItemRow struct {
    // ... 现有字段 ...
    Tagline   *string
    Studio    *string
    CreatedAt *time.Time
}
```

然后在 `models/item.go` 的 `MapColsToItemRow` 中读取:
```go
item.Tagline = getStringPtr(m, "tagline")
item.Studio = getStringPtr(m, "studio")
item.CreatedAt = getTimePtr(m, "created_at")
```

#### 4d. Genres 字段

`itemsSearch` 返回时，如果 Fields 包含 Genres，需额外查 item_genres + genres 表:
```go
if strings.Contains(fields, "Genres") {
    genres, _ := models.GetItemGenres(ctx, state.DB, itemID)
    for _, g := range genres {
        result["Genres"] = append(result["Genres"].([]string), g[1]) // g[1] = genre name
    }
}
```

### 5. People 字段返回数组

**现状**: Items 查询中 People 字段返回字符串而非数组
**文件**: `internal/handlers/compat.go` — `itemsSearch`

```go
// 在 itemsSearch 结果构建中:
if strings.Contains(fields, "People") {
    cast, _ := models.GetItemCast(ctx, state.DB, itemID)
    if cast != nil {
        result["People"] = cast  // 已经是 []map[string]interface{} 格式
    } else {
        result["People"] = []interface{}{}
    }
}
```

### 6. submit_custom_query SQL 转换补全

**现状**: 已有部分 SQLite→PostgreSQL 转换，缺少 `substr/instr` 和 `UserList`
**文件**: `internal/handlers/compat.go` — `submitCustomQuery`

#### 6a. 新增转换规则

```go
// 在现有 rewrite 规则后添加:

// substr(x, 0, instr(x, 'sep')) → split_part(x, 'sep', 1)
rewriteSubstrInstr = regexp.MustCompile(`(?i)substr\s*\(\s*(\w+)\s*,\s*0\s*,\s*instr\s*\(\s*\w+\s*,\s*'([^']+)'\s*\)\s*\)`)
sql = rewriteSubstrInstr.ReplaceAllString(sql, "split_part($1, '$2', 1)")

// || 字符串拼接在 PG 中已兼容，无需转换

// UserList 子查询替换 — 过滤管理员
sql = strings.ReplaceAll(sql, "select UserId from UserList",
    "SELECT id::text FROM users WHERE is_admin = true")
```

#### 6b. PlaybackActivity 字段映射

检查 FYMS 的 `playback_activity` 表是否有以下字段:
- `PlayDuration` — 可能对应 `progress_ms` 或 `duration_ms`
- `PauseDuration` — FYMS 可能没有此字段
- `DeviceName`, `ClientName`, `RemoteAddress` — 对应 `device_name`, `client_name`, `client_ip`

如字段名不同，在 SQL 转换中增加列名映射:
```go
// 创建视图兼容 Emby 字段名
CREATE VIEW "PlaybackActivity" AS
SELECT
    id,
    playback_start AS "DateCreated",
    user_id::text AS "UserId",
    item_id::text AS "ItemId",
    item_type AS "ItemType",
    ... AS "ItemName",
    device_name AS "DeviceName",
    client_name AS "ClientName",
    client_ip AS "RemoteAddress",
    duration_ms / 1000 AS "PlayDuration",
    0 AS "PauseDuration"
FROM playback_activity;
```

---

## P2: 低优先级

### 7. ProductionLocations

直接在 FormatItemDto 中返回空数组即可:
```go
d.ProductionLocations = []string{}
```

### 8. Sessions/Playing/Stop + Sessions/Message

已实现且测试通过，无需修改。

---

## 实施顺序建议

```
Step 1: Users/New 允许空密码         → 1 个文件，5 分钟
Step 2: Users/Query 搜索用户         → 1 个文件，15 分钟
Step 3: Policy 完整支持              → 迁移 + 2 个文件，1 小时
Step 4: Items 字段补全               → 4 个文件，30 分钟
Step 5: People 数组修复              → 1 个文件，10 分钟
Step 6: SQL 转换 + PlaybackActivity  → 1 个文件 + 迁移，30 分钟
```

总计约 2.5 小时工作量。
