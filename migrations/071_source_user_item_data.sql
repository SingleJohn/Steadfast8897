CREATE TABLE IF NOT EXISTS source_user_item_data (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_item_id BIGINT NOT NULL REFERENCES source_items(id) ON DELETE CASCADE,
  playback_position_ticks BIGINT NOT NULL DEFAULT 0,
  play_count INTEGER NOT NULL DEFAULT 0,
  is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
  played BOOLEAN NOT NULL DEFAULT FALSE,
  last_played_date TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, source_item_id)
);
