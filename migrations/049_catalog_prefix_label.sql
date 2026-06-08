-- 049: 修正番号前缀(label)规则 + 回填已有条目的 catalog_number
--
-- 番号前缀此前用 '^[A-Z]+'(开头字母段),对 JAV 常见的"数字厂牌码+字母"格式失效:
--   300MIUM-1328 开头是数字 → 取出空。改为"去掉结尾 -数字"的整段 label:
--   300MIUM-1328 → 300MIUM,326IAV-002 → 326IAV,IPZZ-857 → IPZZ。
--
-- 同时直接从 items.name 回填 catalog_number(老条目入库时还没有该字段,无需重扫)。

-- 1) 重建前缀函数索引(表达式需与查询完全一致才会命中)
DROP INDEX IF EXISTS idx_items_catalog_prefix;
CREATE INDEX IF NOT EXISTS idx_items_catalog_prefix
  ON items ((regexp_replace(upper(catalog_number), '-[0-9]+$', '')))
  WHERE catalog_number IS NOT NULL;

-- 2) 从 name 回填 catalog_number(可选前导数字厂牌码 + 字母 - 数字)
UPDATE items
   SET catalog_number = upper((regexp_match(name, '(\d{0,4}[A-Za-z]{2,8}-\d{2,6})', 'i'))[1])
 WHERE (catalog_number IS NULL OR catalog_number = '')
   AND type IN ('Movie', 'Series')
   AND name ~* '\d{0,4}[A-Za-z]{2,8}-\d{2,6}';
