-- 065: 为名称模糊搜索建立 pg_trgm GIN 索引
--
-- 搜索路径(item_query_repository.go)使用前导通配符 ILIKE:
--   i.name ILIKE '%kw%' OR EXISTS(media_versions mv WHERE mv.name ILIKE '%kw%')
-- 前导 % 无法走 B-Tree,只能整表顺序扫描(日志中 avg ~450ms / max 2s)。
-- pg_trgm 的 GIN 索引支持 LIKE/ILIKE 的任意位置通配,把顺序扫转为索引扫。

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_items_name_trgm
  ON items USING gin (name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_media_versions_name_trgm
  ON media_versions USING gin (name gin_trgm_ops);
