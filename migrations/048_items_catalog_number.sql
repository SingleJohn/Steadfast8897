-- 048: items 番号(catalog number)字段，用于"按番号前缀"虚拟库分类
-- 来源:NFO <num> 优先,文件名正则兜底。前缀(如 IPZZ-857 → IPZZ)用表达式派生。

ALTER TABLE items ADD COLUMN IF NOT EXISTS catalog_number VARCHAR(40);

-- 番号前缀 = 番号开头的连续字母段(大写)。函数索引加速分组(DISTINCT prefix)与匹配。
CREATE INDEX IF NOT EXISTS idx_items_catalog_prefix
  ON items ((substring(upper(catalog_number) from '^[A-Z]+')))
  WHERE catalog_number IS NOT NULL;
