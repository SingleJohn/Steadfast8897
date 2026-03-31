-- Homepage performance indexes

-- Accelerate getLatestItems: library + type + created_at DESC
CREATE INDEX IF NOT EXISTS idx_items_library_type_created
  ON items(library_id, type, created_at DESC);

-- Accelerate Views COUNT queries: library + type
CREATE INDEX IF NOT EXISTS idx_items_library_type
  ON items(library_id, type);

-- Accelerate Resume query (playback_position_ticks > 0) — partial index
CREATE INDEX IF NOT EXISTS idx_uid_resumable
  ON user_item_data(user_id, playback_position_ticks)
  WHERE playback_position_ticks > 0;

-- Accelerate Favorite query — partial index
CREATE INDEX IF NOT EXISTS idx_uid_favorite
  ON user_item_data(user_id, is_favorite)
  WHERE is_favorite = TRUE;

-- Accelerate DatePlayed sorting
CREATE INDEX IF NOT EXISTS idx_uid_last_played
  ON user_item_data(user_id, last_played_date DESC);
