-- 识别阶段的冷却字段：识别失败的 item 在 cooldown_until 之前不再被自动重试，
-- 避免扫库/刮削任务把 TMDB 额度反复消耗在同一批坏样本上。
-- 手动触发（按 TMDB ID 重刮、用户搜索选择）会绕过 cooldown。
ALTER TABLE items ADD COLUMN IF NOT EXISTS identify_attempted_at TIMESTAMPTZ;
ALTER TABLE items ADD COLUMN IF NOT EXISTS identify_cooldown_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_items_identify_cooldown
    ON items (identify_cooldown_until)
    WHERE identify_cooldown_until IS NOT NULL;
