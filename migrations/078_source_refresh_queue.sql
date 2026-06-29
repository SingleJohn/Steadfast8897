-- migrations/078_source_refresh_queue.sql
-- 在线源内容刷新队列：填充虚拟库(catalog_fetch) + 连载剧集追更(detail_refresh)。
-- 复用 scrape_queue 的「持久化队列 + 退避重试」模式，但目标是 source_providers / source_items。

-- detail_refreshed_at：记录最后一次成功重拉剧集 detail 的时间，用于 TTL 判断是否需要追更。
-- 不能用 updated_at —— 搜索/分类入库也会刷新 updated_at，会让 TTL 判断失真。
ALTER TABLE source_items ADD COLUMN IF NOT EXISTS detail_refreshed_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS source_refresh_queue (
    id          BIGSERIAL PRIMARY KEY,
    task_type   TEXT        NOT NULL,                       -- catalog_fetch | detail_refresh
    target_kind TEXT        NOT NULL,                       -- provider | item
    target_id   BIGINT      NOT NULL,                       -- provider_id 或 source_item_id
    payload     JSONB       NOT NULL DEFAULT '{}'::jsonb,   -- 任务参数(分类范围 / 页数等)
    priority    SMALLINT    NOT NULL DEFAULT 5,             -- 数值越小越优先
    status      TEXT        NOT NULL DEFAULT 'pending',     -- pending | running | done | failed
    retry_count SMALLINT    NOT NULL DEFAULT 0,
    last_error  TEXT,
    next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (task_type, target_kind, target_id)
);

-- Claim 用：只扫 pending，按优先级 + 到期时间取。
CREATE INDEX IF NOT EXISTS idx_source_refresh_queue_claim
    ON source_refresh_queue (priority, next_run_at)
    WHERE status = 'pending';

-- 调度用：快速找出需要追更的连载剧（detail 已加载且超过 TTL）。
CREATE INDEX IF NOT EXISTS idx_source_items_detail_refresh
    ON source_items (detail_refreshed_at)
    WHERE detail_loaded = TRUE;
