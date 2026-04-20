-- Phase 5: 刮削队列诊断字段。
-- 前端展开单行时展示最后一次尝试的 TMDB 请求 URL(已脱敏 api_key)、
-- HTTP 状态码、响应 body(仅失败时写入,成功与 retry 清空)。
-- 三列只在按 id 查详情时读,不建索引。

ALTER TABLE scrape_queue ADD COLUMN IF NOT EXISTS request_url      TEXT;
ALTER TABLE scrape_queue ADD COLUMN IF NOT EXISTS response_status  INTEGER;
ALTER TABLE scrape_queue ADD COLUMN IF NOT EXISTS response_sample  TEXT;
