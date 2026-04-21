-- 040: 移除"多源投票"刮削策略(aggregated),识别只保留 sequential 单源流程
-- 清理两处残留:
--   1) system_config.scrape_strategy 键
--   2) libraries.scrape_config JSONB 里的 "strategy" 键
-- 剥离后若 scrape_config 变成空对象 {},归位到 NULL(= 完全继承全局)
--
-- JSONB 层的残留不影响运行:ConfigOverride 删掉 Strategy 字段后,
-- json.Unmarshal 对未知键静默忽略;本迁移是清洁措施,幂等可重入。

DELETE FROM system_config WHERE key = 'scrape_strategy';

UPDATE libraries
   SET scrape_config = scrape_config - 'strategy'
 WHERE scrape_config IS NOT NULL
   AND scrape_config ? 'strategy';

UPDATE libraries
   SET scrape_config = NULL
 WHERE scrape_config = '{}'::jsonb;
