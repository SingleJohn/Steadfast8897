# SQL residual audit

本文件记录 9 阶段 SQL 重构后的直接 SQL 边界。原则是：

- 普通业务固定 SQL 继续迁移到 `internal/db/queries` + `internal/repository`。
- 复杂动态查询可以保留，但必须集中在既有 builder/模型层，禁止在 handler/service 随手拼接。
- 基础设施 SQL 可以保留在基础设施模块内，例如 migration、备份恢复、批量导入、PG 系统统计和启动级设置。

## must_migrate

当前为空。普通业务 SQL 已不应直接留在 handler/service/model/gateway 业务文件中。

## migrated_in_phase_12

这些路径在 Phase 12 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/services/auto_scrape.go` | 自动刮削单 item 资格检查、按库候选列表、全局缺失 identify 候选改走 `ScrapeQueueRepository` + `scrape_queue.sql` |
| `internal/services/episode_fetch.go` | Series season 列表、episode metadata 扫描、批量 name/overview 更新、still 回写改走 `ItemHelperRepository` + `item_helpers.sql` |
| `internal/services/tmdb_storage.go` | 保持无直接业务 SQL；保存模式辅助继续复用既有 repository |
| `internal/handlers/emby_compat.go` | media stats、season episode check、library check、Series 查找改走 `ItemHelperRepository` / `UserRepository` |

## migrated_in_phase_13

这些路径在 Phase 13 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/handlers/videos.go` | MediaVersions lookup/upsert、merged primary、stream/trailer/subtitle lookup、token lookup 改走 `PlaybackRepository` / `SessionRepository` |
| `internal/handlers/library_detail.go` | 详情 extras、MediaSources、merged sources、library count、trailer、similar lookup 改走 `PlaybackRepository` |
| `internal/handlers/compat_media.go` | Item/merged MediaSources 查询复用 `PlaybackRepository` |
| `internal/handlers/compat_show.go` | season/episode id 列表、season number、episode count/pagination 改走 `PlaybackRepository` |

## migrated_in_phase_14

这些路径在 Phase 14 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/services/incremental_scan.go` | webhook path mappings 改走 `SystemConfigRepository`；孤儿 Season/Series 清理改走 `ScanIngestRepository` |
| `internal/services/ingest_match.go` | library path index 刷新改走 `LibraryRepository.ListLibrariesForIngestMatch` |
| `internal/services/scanner_cleanup.go` | prune 候选、catalog number backfill、media_versions backfill 改走 `ScanIngestRepository` / `PlaybackRepository` |
| `internal/services/scanner_dir.go` | local trailer、extra backdrops、artwork sync 改走 `ScanIngestRepository` |
| `internal/services/scanner_mixed.go` | mixed folder upsert/lookup、parent 绑定改走 `ScanIngestRepository` |
| `internal/services/scanner_movie.go`、`internal/services/scanner_tv.go` | movie/episode `media_versions` upsert 改走 `PlaybackRepository.UpsertMediaVersion`；TMDB 补全资格查找改走 `ScanIngestRepository` |
| `internal/services/scanner_nfo.go` | provider_ids、genres/tags、cast image 查询/清理、平台错误标记改走 `ScanIngestRepository`；事务与 `CopyFrom` 保持原边界 |

## migrated_in_phase_15

这些路径在 Phase 15 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/services/backfill_actor_images.go`、`internal/services/backfill_episode_image.go`、`internal/services/backfill_episode_name.go`、`internal/services/backfill_media_quality.go` | backfill 候选查询、计数、回写改走 `BackgroundTaskRepository` |
| `internal/services/tmdb_identify.go`、`internal/services/tmdb_utils.go`、`internal/services/unmatched.go` | scrape item meta、external ids、identify candidates、unmatched 列表/候选 batch、统计计数改走 `BackgroundTaskRepository` |
| `internal/services/probe_on_play.go`、`internal/services/probe_task.go` | probe 目标查询、mediainfo/章节回写、统计计数、path mapping 读取改走 `BackgroundTaskRepository` / `SystemConfigRepository` |
| `internal/services/notify.go`、`internal/services/notify_sweeper.go` | 通知 item payload 查询、订阅查询、投递状态更新、library.new sweeper 候选查询改走 `NotifyRepository` |

## migrated_in_phase_16

这些路径在 Phase 16 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/handlers/stats.go` | 统计聚合、排行筛选/排序、近期播放查询改走 `StatsRepository`；handler 仅保留参数解析和响应组装 |

## migrated_in_phase_11

这些路径在 Phase 11 已迁出直接 SQL，并已从边界脚本 allowlist 移除。

| 路径 | 迁移方式 |
| --- | --- |
| `internal/services/file_watcher.go` | 启动列库改走 `LibraryRepository.ListLibrariesForWatcher` |
| `internal/handlers/library_misc.go` | merge 诊断计数改走 `ItemHelperRepository.CountMergedVersionPrimaries/Secondaries`；genres/tags 继续经既有 item helper repository 包装 |
| `internal/handlers/user_access.go` | item library access lookup 改走 `UserRepository.GetItemLibraryIDForAccess` |
| `internal/handlers/compat_sessions.go` | primary media version container/bitrate lookup 改走 `ItemHelperRepository.GetPrimaryMediaVersionInfo` |
| `internal/models/person_userdata.go` | person favorite 读写改走 `ItemHelperRepository` 的 user-person-data 方法 |
| `internal/models/user.go` | policy 字段更新、admin/hidden/disabled 更新改走 `UserRepository` 方法 |

## allowed_dynamic

这些查询包含动态过滤、排序、分页、递归、白名单维度或兼容协议特殊投影，短期允许保留。新增同类动态 SQL 应集中到这些区域或先抽 builder，不允许散落到新 handler/service 文件。

| 路径 | 保留原因 | 约束 |
| --- | --- | --- |
| `internal/models/item_query.go` | 主 item 查询 builder，包含筛选、排序、随机、统计估算 | 继续作为 item 动态查询集中点 |
| `internal/handlers/compat_items.go` | Emby `/Items` 查询，参数组合多 | 保持 Emby 语义；新增字段先核对 CTE 投影 |
| `internal/models/platform.go`、`internal/handlers/library_platform.go` | 虚拟库维度、别名、封面与平台重算 | 维度和排序必须走白名单 |
| `internal/models/person.go`、`internal/models/person_admin.go` | 演员搜索、清理和管理筛选 | 过滤和排序继续白名单化 |
| `internal/gateway/store.go`、`internal/services/redirect_bitrate.go` | gateway 日志统计、重定向码率候选 | 保留在 gateway/redirect 边界内 |
| `internal/services/refresh_scheduler.go`、`internal/services/refresh_worker.go` | refresh queue 和 sidecar 变更调度 | 后续按 refresh repository 逐步迁移 |
| `internal/services/scanner_movie.go`、`internal/services/scanner_tv.go`、`internal/services/scanner_nfo.go` | 扫描、NFO、tv/movie ingest 主链路仍含 item 创建、查重、动态 NFO update、episode canonical merge 等高耦合路径 | 不为清零 SQL 破坏扫描/ingest 语义；继续按固定 helper 和集中 builder 迁移 |

## allowed_infra

这些 SQL 属于基础设施或启动级例外，允许直接使用 pgx。

| 路径 | 保留原因 | 约束 |
| --- | --- | --- |
| `internal/database/database.go` | migration 表、checksum、事务执行迁移 | migration runner 自身不迁入 sqlc |
| `main.go` | 启动级 PG session/pool 设置 | 仅允许固定启动设置，不放业务 SQL |
| `internal/handlers/system.go` | 备份/恢复、动态表导出导入、truncate 白名单、系统日志/指标查询 | 只能使用表名 allowlist；不迁移备份恢复动态 SQL |
| `internal/gateway/store.go` | `CopyFrom` 批量写 gateway 日志 | `CopyFrom` 属于批量写入例外 |
| `internal/services/scanner_nfo.go` | `CopyFrom` 批量导入 cast members | `CopyFrom` 属于批量写入例外；动态 `UPDATE items SET ...` 后续应收敛为 NFO repository builder |
| `internal/models/item_query.go` | `pg_class.reltuples` 估算计数 | PG 系统统计例外 |
| `internal/services/progress_buffer.go` | 播放进度缓冲落库 | 可后续迁移，但当前属于内部缓冲写入边界 |
| `internal/handlers/compat_query.go` | Emby 兼容的只读 SQLite 风格查询入口 | 仅保留兼容白名单/转换后的查询，不允许任意写入 |

## 新增 SQL 规则

新增直接 `Query` / `QueryRow` / `Exec` / `Begin` / `CopyFrom` / `SendBatch` 必须满足以下任一条件：

1. 已在本文件分类并进入检查脚本 allowlist。
2. 位于 `internal/db/gen` 生成代码。
3. 新增 sqlc query 和 repository 后由 repository 调用。

不满足条件的新增 SQL 应在提交前迁移或补充审计理由。
