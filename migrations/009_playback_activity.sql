CREATE TABLE IF NOT EXISTS playback_activity (
  id BIGSERIAL PRIMARY KEY,
  date_created TIMESTAMP NOT NULL DEFAULT NOW(),
  user_id UUID NOT NULL,
  item_id UUID NOT NULL,
  item_type VARCHAR(20),
  item_name TEXT,
  play_method VARCHAR(20) DEFAULT 'DirectPlay',
  client_name VARCHAR(100),
  device_name VARCHAR(100),
  play_duration INT DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_pa_date ON playback_activity(date_created);
CREATE INDEX IF NOT EXISTS idx_pa_user ON playback_activity(user_id);
CREATE INDEX IF NOT EXISTS idx_pa_item ON playback_activity(item_id);
