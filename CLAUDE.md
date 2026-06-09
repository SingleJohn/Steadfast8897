# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

FYMS is an Emby-compatible media server written in Go + Vue 3. It serves Infuse, Emby, and other Emby-protocol clients. The Go binary embeds the Vue frontend via `//go:embed all:web/dist`.

## Development Rules

**DO NOT run any code, tests, or environment commands.** The user manages the environment manually. Your responsibility is writing code only:
- Do NOT execute `go run`, `go test`, `npm run`, or any other runtime commands
- Do NOT write test files or test code
- Do NOT run existing tests
- The user will run, test, and provide feedback

**所有回复使用中文。**

## Commands

### Backend

```bash
# Required env vars (or use .env file)
export DB_HOST=localhost DB_PORT=5432 DB_NAME=media_server
export DB_USER=postgres DB_PASSWORD=postgres
export REDIS_HOST=127.0.0.1 REDIS_PORT=6379

go run .          # start backend on :8961
go build -o fyms . # build binary (requires web/dist to be built first)
go test ./...     # run all tests
go test ./internal/services/ -run TestScanner  # run a single test
```

### Frontend

```bash
cd web
npm install
npm run dev    # dev server on :3001, proxies API to :8961
npm run build  # output to web/dist/ (embedded into Go binary at build time)
```

### Docker

```bash
docker build -t eianz/fyms:latest .
docker-compose up -d  # includes PostgreSQL + Redis
```

## Architecture

### Request Flow

All routes are registered twice — once on the root group and once under `/emby` — so Emby clients using either prefix work identically.

```
HTTP (Gin) → middleware (auth/admin/optAuth) → handlers/ → models/ → PostgreSQL + Redis
```

`handlers.AppState` is injected into every Gin context via middleware and carries all shared services (DB pool, cache, session manager, ingest worker, etc.).

### 扫描 / 监控 / 刮削 Pipeline

三条事件源共享同一条入库通道(ingest)、同一条刮削通道(scrape_queue):

```
FileWatcher(fsnotify) ─┐
ScanLibrary(手动/定时) ─┼──▶ IngestWorker (channel, 4 并发)
Webhook ──────────────┘        │
                               ▼
                         PostgreSQL items
                               │ enqueue on change
                               ▼
                         scrape_queue (PG 表)
                               │
                               ▼
                    ScrapeWorker (4 并发 + rate.Limiter)
                               │
                               ▼
                         TMDB / Bangumi / ...
```

- **IngestWorker**(`ingest_*.go`):把文件级事件翻译成 items/media_versions 写入。Rename 成对事件在 500ms 窗口合并,目录删除用 `LIKE '<path>/%'` 级联。
- **ScanLibrary**(`scanner.go`):不再直接落库。遍历 FS 产 Create 事件 → `IngestWorker.Barrier` 等 drain → 差集对比(DB 里 file_path 不在本次扫到的) → 产 Delete 事件。挂断路径(`os.Stat` 失败)自动跳过,不误删。
- **ScrapeWorker**(`scrape_worker.go`):消费 `scrape_queue`,按 task_type 分派到 identify / backfill_quality / backfill_episode_name / backfill_episode_image。失败按 2→4→8→16→32 分钟退避,5 次后 status=failed。
- **TMDB 限流**:`sharedTmdbLimiter`(3 rps / burst 5)所有调用路径共享,通过 `TmdbClient.tmdbGet` 自动 Wait。

### Key Packages

- `internal/handlers/` — HTTP controllers. `compat.go` and `emby_compat.go` implement the Emby API surface that third-party clients depend on.
- `internal/models/` — raw SQL queries against PostgreSQL (pgx/v5). No ORM.
- `internal/services/` — 后台任务,按职责拆分:
  - `scanner.go` / `scanner_*.go` — 全库扫描入口 + 单 item/show 处理 helper
  - `ingest_*.go` — fsnotify / webhook / scan 的统一消费通道
  - `scrape_queue.go` / `scrape_worker.go` — 刮削任务持久化队列 + 并发消费
  - `auto_scrape.go` / `backfill_*.go` — 任务产生端(扫表入队)
  - `tmdb.go` — TMDB 客户端 + Aggregator 缓存
  - `metrics.go` — 周期性观测打点
  - `session_manager.go` / `updater.go` — 其他背景任务
- `internal/gateway/` — 302 redirect engine. Rewrites local file paths to remote URLs (115, Alist, WebDAV, etc.) and records request stats.
- `migrations/` — plain SQL files run in order at startup by `database.RunMigrations`.

### Multi-Version Merging

Movies with the same `tmdb_id` + `studio` (platform) are merged: one becomes the `primary`, others get `merged_to_id` set. Key rules:
- Only `Movie` type is merged (Series merging creates orphan episodes).
- Merging is per-platform — no cross-platform merges.
- `POST /Library/MergeVersions` resets all merges and recalculates from scratch (idempotent).
- Detail/playback requests for a secondary auto-redirect to the primary.

### Platform Virtual Libraries (平台 / 虚拟库)

虚拟库存于 `platform_libraries` 表，按某个维度的值聚合 `items`，与用户的物理媒体库相互独立。`POST /Library/Platforms/*` 路由族管理它们。

- **多维度** (`dimension`)：`studio`(片商) / `num_prefix`(番号字母前缀，依赖 049 函数索引) / `actor`(演员，走 `cast_members`)。维度→SQL 由 `models.virtualDimensionCondition` 统一产出，占位符 `$1` 为 `text[]`。
- **多值聚合** (`match_values TEXT[]`，迁移 054)：一个虚拟库可绑定多个匹配值（`= ANY($1)`），用来把簡繁/译名等同一实体的不同写法合并进一个库。`match_value` 保留为「主值」——唯一键 `(dimension, match_value)` 与 `PlatformVirtualID(dimension, match_value)` 都基于它，保持客户端缓存/封面稳定。`PlatformLibrary.Values()` 兜底退化为 `[match_value]`。`POST/DELETE /Library/Platforms/:id/Values` 增删别名。
- **自定义显示名** (`display_name`，迁移 052)：优先级 `display_name`(用户自定义) > `PlatformDisplayName(platform_name)`(内置本地化) > `platform_name`，统一经 `PlatformLibrary.EffectiveDisplayName()`。logo/渐变仍按 `platform_name` 匹配，改名不影响图标。`POST /Library/Platforms/:id/Rename`。
- **封面生成**：复用 `internal/services/coverart` 的多风格 registry（`/Library/CoverArt/Styles` 列出 ninegrid/showcase…），`POST /Library/Platforms/:id/Image/Generate` 与 `.../CoverArt/GenerateAll` 接收 `{Style, Options}`。新增风格只需 `coverart.Register`，前后端下拉自动出现。
- **统一展示顺序** (`library_display_order` 表，迁移 053)：实际库 + 虚拟库混排。`getUserViews` 有此表记录时按 `sort_order` 合并排序，无记录回退旧的 `platform_libraries_position`(before/after)。`POST /Library/DisplayOrder` 整体重写。
- 仍复用 Multi-Version 合并结果（按 `studio` 分组的 primary），与物理库共用聚合能力。

### Image Serving (`handlers/images.go`)

Fallback chain: Episode → Season → Series → merged primary. Resized images are cached in `data/cache/`. WebP decoding is registered at init. Resize failures fall back to the original image rather than returning 500.

### Authentication

Tokens are passed via `X-Emby-Token` or `Authorization` header. Passwords are stored as bcrypt; legacy Emby SHA1 hashes are auto-upgraded on first login. Login failures are rate-limited per IP.

### Database Migrations (数据库迁移)

迁移系统在每次服务启动时自动执行（`database.RunMigrations`）：

1. 读取 `migrations/` 目录下所有 `.sql` 文件，按文件名字母序排序
2. 查询 `migrations` 表中已执行的记录
3. 未执行的文件在事务中执行，成功后写入 `migrations` 表
4. 已执行的文件跳过（幂等）

**如何新增字段或修改表结构：**

创建新的迁移文件，命名为 `NNN_描述.sql`（序号递增）：

```sql
-- migrations/016_add_new_field.sql
ALTER TABLE items ADD COLUMN IF NOT EXISTS new_field VARCHAR(255);
CREATE INDEX IF NOT EXISTS idx_items_new_field ON items(new_field);
```

**注意：**
- 文件名序号必须大于现有最大序号（新增时 `ls migrations/` 看最大号 +1）
- 已执行的迁移文件不能修改，只能新增
- 使用 `IF NOT EXISTS` 保证幂等性
- 迁移失败会回滚并阻止服务启动

## Data Directory Layout

```
data/
├── cache/      # resized image cache
├── metadata/   # TMDB-downloaded images
├── logs/       # fyms-YYYY-MM-DD.log (7-day retention)
└── backups/    # database backups
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8961` | HTTP listen port |
| `DB_HOST/PORT/NAME/USER/PASSWORD` | see README | PostgreSQL connection |
| `REDIS_HOST/PORT/PASSWORD` | see README | Redis connection |
| `DB_POOL_MAX` | `400` | pgxpool max connections |
| `FYMS_UPDATE_IMAGE_REPO` | `eianz/fyms` | Docker image for self-update |
| `FYMS_UPDATE_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker socket path |
