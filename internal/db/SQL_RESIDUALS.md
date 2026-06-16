# SQL residual audit

本文件记录 9 阶段 SQL 重构后的直接 SQL 边界。原则是：

- 普通业务固定 SQL 继续迁移到 `internal/db/queries` + `internal/repository`。
- 复杂动态查询可以保留，但必须集中在既有 builder/模型层，禁止在 handler/service 随手拼接。
- 基础设施 SQL 可以保留在基础设施模块内，例如 migration、备份恢复、批量导入、PG 系统统计和启动级设置。

## must_migrate

这些路径仍有普通业务 SQL，应按风险和改动面继续迁移。低风险项优先迁移，复杂主链路先保持行为不变。

| 路径 | 当前用途 | 后续建议 |
| --- | --- | --- |
| `internal/services/auto_scrape.go` | 自动刮削候选 item 查询 | 配置读取已改走 `SystemConfigRepository`；候选查询后续可收进 scrape/queue repository |
| `internal/services/file_watcher.go` | 启动时列出 libraries | 配置读取已改走 `SystemConfigRepository`；library 列表复用/补充 `LibraryRepository` |
| `internal/services/episode_fetch.go`、`internal/services/tmdb_storage.go` | episode 元数据批量更新、still 图回写、保存模式辅助 | 配置读取已改走 `SystemConfigRepository`；批量更新后续按 TMDB/episode repository 收口 |
| `internal/handlers/emby_compat.go` | 系统统计、season/episode 简单查询 | 后续迁到 compat/item helper repository |
| `internal/handlers/library_misc.go`、`internal/handlers/user_access.go`、`internal/handlers/compat_sessions.go` | 单点 lookup | 后续迁到对应 repository |
| `internal/models/user.go`、`internal/models/person_userdata.go` | 用户和人物 user data 固定写入 | 后续迁到 users/person repository |

## allowed_dynamic

这些查询包含动态过滤、排序、分页、递归、白名单维度或兼容协议特殊投影，短期允许保留。新增同类动态 SQL 应集中到这些区域或先抽 builder，不允许散落到新 handler/service 文件。

| 路径 | 保留原因 | 约束 |
| --- | --- | --- |
| `internal/models/item_query.go` | 主 item 查询 builder，包含筛选、排序、随机、统计估算 | 继续作为 item 动态查询集中点 |
| `internal/handlers/compat_items.go`、`internal/handlers/compat_show.go` | Emby `/Items`、剧集兼容查询，参数组合多 | 保持 Emby 语义；新增字段先核对 CTE 投影 |
| `internal/models/platform.go`、`internal/handlers/library_platform.go` | 虚拟库维度、别名、封面与平台重算 | 维度和排序必须走白名单 |
| `internal/models/person.go`、`internal/models/person_admin.go` | 演员搜索、清理和管理筛选 | 过滤和排序继续白名单化 |
| `internal/handlers/stats.go` | 统计页多条件聚合和排序 | 排序字段必须走白名单 |
| `internal/services/notify.go`、`internal/services/notify_sweeper.go` | 通知订阅筛选和过期清理 | 后续可按 notifier repository 收口 |
| `internal/gateway/store.go`、`internal/services/redirect_bitrate.go` | gateway 日志统计、重定向码率候选 | 保留在 gateway/redirect 边界内 |
| `internal/services/refresh_scheduler.go`、`internal/services/refresh_worker.go` | refresh queue 和 sidecar 变更调度 | 后续按 refresh repository 逐步迁移 |
| `internal/services/scanner_*.go`、`internal/services/ingest_match.go`、`internal/services/incremental_scan.go` | 扫描、rename/delete、NFO、mixed/tv/movie ingest 主链路 | 不为清零 SQL 破坏扫描/ingest 语义；优先迁固定 helper |
| `internal/services/backfill_*.go`、`internal/services/episode_fetch.go`、`internal/services/tmdb_identify.go`、`internal/services/unmatched.go` | 后台补全、候选识别、未匹配查询 | 先保留，后续按任务域迁 repository |
| `internal/handlers/library_detail.go`、`internal/handlers/videos.go`、`internal/handlers/compat_media.go` | 详情、播放、MediaSources fallback/多版本兼容 | 保持播放和 Emby 兼容语义优先 |

## allowed_infra

这些 SQL 属于基础设施或启动级例外，允许直接使用 pgx。

| 路径 | 保留原因 | 约束 |
| --- | --- | --- |
| `internal/database/database.go` | migration 表、checksum、事务执行迁移 | migration runner 自身不迁入 sqlc |
| `main.go` | 启动级 PG session/pool 设置 | 仅允许固定启动设置，不放业务 SQL |
| `internal/handlers/system.go` | 备份/恢复、动态表导出导入、truncate 白名单、系统日志/指标查询 | 只能使用表名 allowlist；不迁移备份恢复动态 SQL |
| `internal/gateway/store.go` | `CopyFrom` 批量写 gateway 日志 | `CopyFrom` 属于批量写入例外 |
| `internal/services/scanner_nfo.go` | `CopyFrom` 批量导入 cast members | `CopyFrom` 属于批量写入例外 |
| `internal/models/item_query.go` | `pg_class.reltuples` 估算计数 | PG 系统统计例外 |
| `internal/services/progress_buffer.go` | 播放进度缓冲落库 | 可后续迁移，但当前属于内部缓冲写入边界 |
| `internal/handlers/compat_query.go` | Emby 兼容的只读 SQLite 风格查询入口 | 仅保留兼容白名单/转换后的查询，不允许任意写入 |

## 新增 SQL 规则

新增直接 `Query` / `QueryRow` / `Exec` / `Begin` / `CopyFrom` / `SendBatch` 必须满足以下任一条件：

1. 已在本文件分类并进入检查脚本 allowlist。
2. 位于 `internal/db/gen` 生成代码。
3. 新增 sqlc query 和 repository 后由 repository 调用。

不满足条件的新增 SQL 应在提交前迁移或补充审计理由。
