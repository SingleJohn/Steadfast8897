-- 放开 items 的"同库不能重名"业务约束。
-- 原因:
--   1) 重名剧集/电影在现实中普遍(同名不同年份、同名中译名、多平台引进版等)。
--   2) 刮削阶段把 items.name 归一到 TMDB 标题时,常撞上另一条已归一的同名记录,
--      导致 UPDATE 报 23505(duplicate key value violates unique constraint),
--      采纳/扫描返回 500。
-- 物理/业务唯一性由以下字段继续保障,不受本次变更影响:
--   - items.id        UUID 主键         系统内部唯一 ID
--   - items.emby_id   SERIAL + UNIQUE   Emby 协议兼容整数 ID
--   - items.file_path idx_items_filepath_unique   扫描幂等键(同路径只入一条)
--   - items.tmdb_id   多版本合并通过 MergeVersions + merged_to_id 关联
DROP INDEX IF EXISTS idx_items_series_unique;
DROP INDEX IF EXISTS idx_items_movie_unique;
