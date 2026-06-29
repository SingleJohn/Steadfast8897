CREATE TABLE IF NOT EXISTS user_media_version_data (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  media_version_id UUID NOT NULL REFERENCES media_versions(id) ON DELETE CASCADE,
  playback_position_ticks BIGINT NOT NULL DEFAULT 0,
  play_count INTEGER NOT NULL DEFAULT 0,
  played BOOLEAN NOT NULL DEFAULT FALSE,
  last_played_date TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, media_version_id)
);

CREATE INDEX IF NOT EXISTS idx_user_media_version_data_item
  ON user_media_version_data(user_id, item_id, last_played_date DESC);

CREATE INDEX IF NOT EXISTS idx_user_media_version_data_resume
  ON user_media_version_data(user_id, playback_position_ticks)
  WHERE playback_position_ticks > 0 AND played = FALSE;
