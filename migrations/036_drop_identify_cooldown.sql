-- Phase 5: 删除 items.identify_cooldown_until 列。
-- Phase 2 引入 scrape_queue.next_run_at 后,identify 冷却语义完全被接管
--  (Fail 按 2→4→8→16→32min 指数退避;worker.Claim 只取 next_run_at <= NOW())。
-- UnmatchedPage 前端同步迁移到从 scrape_queue LEFT JOIN 读 next_retry_at。

DROP INDEX IF EXISTS idx_items_identify_cooldown;
ALTER TABLE items DROP COLUMN IF EXISTS identify_cooldown_until;

-- identify_attempted_at 保留:它是"最后一次尝试识别的时间",与冷却语义不同,
-- 作为诊断/审计仍有价值。
