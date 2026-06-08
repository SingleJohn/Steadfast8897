-- 050: 本地预告片字段 + 清理被误扫成独立影片的 extras 视频
--
-- trailers/ 等 extras 目录里的视频此前会被 fsnotify 文件事件扫成独立电影。
-- 这里加 local_trailer_path(关联到所属电影),并删除已误扫的条目。
-- items 的子表(media_versions/media_streams/cast_members/item_genres/item_tags/
-- item_images/user_item_data 等)均 ON DELETE CASCADE,删 items 即可级联清理。

ALTER TABLE items ADD COLUMN IF NOT EXISTS local_trailer_path VARCHAR(1000);

-- 删除 file_path 直接父目录名属于 extras 集合的电影条目(保守:仅 type='Movie')。
DELETE FROM items
 WHERE type = 'Movie'
   AND file_path IS NOT NULL
   AND lower(substring(file_path from '([^/]+)/[^/]+$')) IN (
     'trailers', 'extras', 'featurettes', 'behind the scenes', 'deleted scenes',
     'interviews', 'scenes', 'samples', 'shorts', 'theme-music', 'backdrops'
   );
