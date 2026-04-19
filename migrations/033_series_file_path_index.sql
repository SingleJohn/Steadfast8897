-- 给 Series 的 file_path (Show 根目录) 建 partial index,用于扫描器按路径
-- 定位 Series UUID 的查询 (scanner.scanOneShow 的 findExistingByPath)。
-- 不回填老数据:既有 Series 的 file_path 保持 NULL,扫描器下次扫到
-- 对应 Show 目录时按 name 兜底并惰性回填 file_path。
CREATE INDEX IF NOT EXISTS idx_items_series_file_path
  ON items (library_id, file_path)
  WHERE type = 'Series' AND file_path IS NOT NULL;
