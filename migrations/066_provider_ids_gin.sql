-- 066: provider_ids 反查走 GIN 索引,消除整表 jsonb_each_text 顺序扫描
--
-- 客户端按外部站点 ID 反查条目(Infuse/聚合器 AnyProviderIdEquals=tmdb.755898)走的是:
--   EXISTS (SELECT 1 FROM jsonb_each_text(i.provider_ids) pe
--           WHERE LOWER(pe.key)=$k AND pe.value=$v)  OR ...
-- 每行把 JSONB 展开 + LOWER(key),无法用索引 → 整表顺序扫(日志 avg ~450ms / max 2s)。
--
-- 把每个条目的 provider_ids 规范化成 `lower(key)=value` 文本数组,建 GIN(array_ops)。
-- 查询改写成数组重叠 `item_provider_kv(provider_ids) && ARRAY['tmdb=755898',...]`,
-- 等价于原来的 OR 语义(任一命中),且大小写、精确值匹配语义完全一致,可走索引。
-- 写入路径不变 —— 仅新增函数 + 索引。

-- key 统一小写、value 原样,与历史查询 LOWER(pe.key)=k AND value=v 语义一致。
-- 非 object(含 NULL)安全退化为空数组,不报错。
CREATE OR REPLACE FUNCTION item_provider_kv(p jsonb)
RETURNS text[]
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
  SELECT ARRAY(
    SELECT lower(e.key) || '=' || e.value
    FROM jsonb_each_text(
      CASE WHEN jsonb_typeof(p) = 'object' THEN p ELSE '{}'::jsonb END
    ) e
  )
$$;

CREATE INDEX IF NOT EXISTS idx_items_provider_kv
  ON items USING gin (item_provider_kv(provider_ids));
