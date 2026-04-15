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

`handlers.AppState` is injected into every Gin context via middleware and carries all shared services (DB pool, cache, session manager, etc.).

### Key Packages

- `internal/handlers/` — HTTP controllers. `compat.go` and `emby_compat.go` implement the Emby API surface that third-party clients depend on.
- `internal/models/` — raw SQL queries against PostgreSQL (pgx/v5). No ORM.
- `internal/services/` — background tasks: `scanner.go` (media scan), `tmdb.go` (metadata scraping), `file_watcher.go` (fsnotify), `session_manager.go`, `updater.go` (Docker self-update).
- `internal/gateway/` — 302 redirect engine. Rewrites local file paths to remote URLs (115, Alist, WebDAV, etc.) and records request stats.
- `migrations/` — plain SQL files run in order at startup by `database.RunMigrations`.

### Multi-Version Merging

Movies with the same `tmdb_id` + `studio` (platform) are merged: one becomes the `primary`, others get `merged_to_id` set. Key rules:
- Only `Movie` type is merged (Series merging creates orphan episodes).
- Merging is per-platform — no cross-platform merges.
- `POST /Library/MergeVersions` resets all merges and recalculates from scratch (idempotent).
- Detail/playback requests for a secondary auto-redirect to the primary.

### Platform Virtual Libraries

Libraries with `type = platform` group items by `studio` field. They reuse the same merge logic but are separate from user physical libraries.

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
- 文件名序号必须大于现有最大序号（当前为 `015`）
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
