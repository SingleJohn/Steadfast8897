-- 媒体库软删除：deleted_at 非空即表示已标记为删除,等待后台清理 goroutine
-- 分批删除 items/libraries 行。所有读 libraries 的查询需要加 deleted_at IS NULL 过滤。
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

-- 部分索引：只索引未删除的库，不占空间但让 WHERE deleted_at IS NULL 查询走索引。
CREATE INDEX IF NOT EXISTS idx_libraries_alive ON libraries(id) WHERE deleted_at IS NULL;
