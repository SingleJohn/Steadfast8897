-- Phase 5: 补充 scrape_queue 结构化诊断。
-- 用于保存 identify/no-match 等非 HTTP 失败的详细上下文，
-- 前端详情页可直接展示 parsed / attempts / candidates / threshold。

ALTER TABLE scrape_queue ADD COLUMN IF NOT EXISTS detail_json JSONB;
