-- Phase 2: item-level refresh 持久化队列。
-- 与 scrape_queue 职责分离:
--   - refresh_queue 负责本地 metadata / images / subtree refresh 调度
--   - scrape_queue 继续负责远程 identify / backfill

CREATE TABLE IF NOT EXISTS refresh_queue (
    id           BIGSERIAL PRIMARY KEY,
    item_id      UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    scope        VARCHAR(16) NOT NULL,
    source       VARCHAR(16) NOT NULL DEFAULT 'manual',
    priority     SMALLINT NOT NULL DEFAULT 5,
    options_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status       VARCHAR(16) NOT NULL DEFAULT 'pending',
    retry_count  SMALLINT NOT NULL DEFAULT 0,
    next_run_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(item_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_refresh_queue_ready
    ON refresh_queue (priority, next_run_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_refresh_queue_status
    ON refresh_queue (status, updated_at);
