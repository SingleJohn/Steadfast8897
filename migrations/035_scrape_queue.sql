-- Phase 2: 刮削持久化队列。
-- 统一 autoScrape 和 3 个 backfill(quality / episode_name / episode_image)
-- 到一张表,由 scrape_worker 消费,共享 TMDB rate.Limiter。
--
-- item_id 的语义随 task_type 不同:
--   identify              → Movie/Series item.id
--   backfill_quality      → Movie/Episode item.id(worker 处理该 item 下所有 resolution IS NULL 的 media_versions)
--   backfill_episode_name → Series item.id(worker 用 tmdb_id 拉 season.episodes 回填标题)
--   backfill_episode_image→ Season item.id(worker 用 tmdb_id+season_num 拉 stills 分发到 episodes)

CREATE TABLE IF NOT EXISTS scrape_queue (
    id           BIGSERIAL PRIMARY KEY,
    item_id      UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    task_type    VARCHAR(32) NOT NULL,
    priority     SMALLINT NOT NULL DEFAULT 5,
    status       VARCHAR(16) NOT NULL DEFAULT 'pending',
    next_run_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retry_count  SMALLINT NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(item_id, task_type)
);

-- 仅索引 pending 行,避免 done/failed 长期堆积拖慢扫描。
CREATE INDEX IF NOT EXISTS idx_scrape_queue_ready
    ON scrape_queue (priority, next_run_at)
    WHERE status = 'pending';

-- 用于启动 reconcile / admin 面板按状态分组统计。
CREATE INDEX IF NOT EXISTS idx_scrape_queue_status
    ON scrape_queue (status, updated_at);
