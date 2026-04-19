-- 任务中心：统一追踪 scan/scrape/probe/backfill/update 的运行历史。
-- 每次任务启动落一条 run；Backfill 一次触发 = 1 父 run + 3 子 run（按 stage 分行），
-- 父 run 的 stage 为 NULL，子 run 通过 parent_id 指向父 run。
CREATE TABLE IF NOT EXISTS task_runs (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT        NOT NULL,
    stage        TEXT,
    parent_id    BIGINT      REFERENCES task_runs(id) ON DELETE SET NULL,
    status       TEXT        NOT NULL,
    trigger      TEXT        NOT NULL DEFAULT 'manual',
    total        BIGINT      NOT NULL DEFAULT 0,
    processed    BIGINT      NOT NULL DEFAULT 0,
    success      BIGINT      NOT NULL DEFAULT 0,
    failed       BIGINT      NOT NULL DEFAULT 0,
    counters     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    message      TEXT,
    error        TEXT,
    payload      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    duration_ms  BIGINT
);

CREATE INDEX IF NOT EXISTS idx_task_runs_kind_started
    ON task_runs (kind, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_task_runs_parent
    ON task_runs (parent_id)
    WHERE parent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_runs_open
    ON task_runs (status)
    WHERE status IN ('queued', 'running', 'stopping');
