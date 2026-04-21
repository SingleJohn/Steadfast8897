-- 给 libraries 加库级刮削配置。NULL 表示完全继承全局 system_config;
-- 非 NULL 时必须是 object,字段级 merge 由后端 scraper.MergeOverride 处理。
--
-- JSONB schema(全部可选):
-- {
--   "providers_enabled":    ["tmdb", "bangumi"],       -- 非 nil 整体覆盖
--   "provider_priority":    {"tmdb": 1, "bangumi": 2}, -- per-key merge
--   "field_priority":       {"rating": ["bangumi", "tmdb"]},
--   "confidence_threshold": 0.72,
--   "auto_apply":           true,
--   "strategy":             "aggregated" | "sequential"
-- }
ALTER TABLE libraries
    ADD COLUMN IF NOT EXISTS scrape_config JSONB;

-- 轻量校验:非 null 必须是 object,挡掉误写的 array/string/number
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'libraries_scrape_config_is_object'
    ) THEN
        ALTER TABLE libraries
            ADD CONSTRAINT libraries_scrape_config_is_object
            CHECK (scrape_config IS NULL OR jsonb_typeof(scrape_config) = 'object');
    END IF;
END $$;
