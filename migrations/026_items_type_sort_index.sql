-- 优化大量 Episode 分页查询性能
-- 覆盖 WHERE type=X ORDER BY sort_name LIMIT/OFFSET 场景
CREATE INDEX IF NOT EXISTS idx_items_type_sortname ON items (type, sort_name);

-- 覆盖 WHERE type=X ORDER BY created_at DESC 场景（最新内容）
CREATE INDEX IF NOT EXISTS idx_items_type_created ON items (type, created_at DESC);
