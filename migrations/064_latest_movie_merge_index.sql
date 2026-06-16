-- Accelerate LatestBatch movie representative lookup without scanning every movie row.
CREATE INDEX IF NOT EXISTS idx_items_latest_movie_merge_group
  ON items (library_id, COALESCE(merged_to_id, id), created_at DESC)
  WHERE type = 'Movie';
