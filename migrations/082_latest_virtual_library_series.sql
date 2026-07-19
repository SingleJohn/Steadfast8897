-- 082: include updated series in the latest virtual library
--
-- Series membership is driven by the newest Episode.created_at, while the
-- virtual library still returns the parent Series item.

CREATE INDEX IF NOT EXISTS idx_items_latest_virtual_episodes
  ON items (series_id, created_at DESC, id DESC)
  WHERE type = 'Episode' AND series_id IS NOT NULL;

UPDATE platform_libraries
   SET collection_type = 'mixed',
       match_values = ARRAY['Movie', 'Series']
 WHERE dimension = 'latest' AND match_value = 'Movie';
