-- gen_random_uuid() is built-in since PostgreSQL 13, no extension needed

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  is_admin BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS access_tokens (
  token VARCHAR(255) PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id VARCHAR(255) NOT NULL,
  device_name VARCHAR(255) NOT NULL DEFAULT '',
  app_name VARCHAR(255) NOT NULL DEFAULT '',
  app_version VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS libraries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  collection_type VARCHAR(50) NOT NULL,
  paths TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  parent_id UUID REFERENCES items(id) ON DELETE CASCADE,
  library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  type VARCHAR(50) NOT NULL,
  name VARCHAR(500) NOT NULL,
  sort_name VARCHAR(500),
  overview TEXT,
  production_year INTEGER,
  premiere_date DATE,
  community_rating REAL,
  official_rating VARCHAR(20),
  runtime_ticks BIGINT,
  index_number INTEGER,
  parent_index_number INTEGER,
  file_path VARCHAR(1000),
  container VARCHAR(20),
  primary_image_path VARCHAR(1000),
  primary_image_tag VARCHAR(64),
  backdrop_image_path VARCHAR(1000),
  backdrop_image_tag VARCHAR(64),
  provider_ids JSONB DEFAULT '{}',
  series_name VARCHAR(500),
  series_id UUID,
  season_id UUID,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_items_parent_id ON items(parent_id);
CREATE INDEX IF NOT EXISTS idx_items_library_id ON items(library_id);
CREATE INDEX IF NOT EXISTS idx_items_type ON items(type);
CREATE INDEX IF NOT EXISTS idx_items_sort_name ON items(sort_name);
CREATE INDEX IF NOT EXISTS idx_items_file_path ON items(file_path);

CREATE TABLE IF NOT EXISTS media_streams (
  id SERIAL PRIMARY KEY,
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  stream_index INTEGER NOT NULL,
  type VARCHAR(20) NOT NULL,
  codec VARCHAR(50),
  language VARCHAR(10),
  title VARCHAR(255),
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  is_forced BOOLEAN NOT NULL DEFAULT FALSE,
  width INTEGER,
  height INTEGER,
  bit_rate INTEGER,
  channels INTEGER,
  sample_rate INTEGER,
  bit_depth INTEGER,
  pixel_format VARCHAR(20),
  display_title VARCHAR(255)
);

CREATE INDEX IF NOT EXISTS idx_media_streams_item_id ON media_streams(item_id);

CREATE TABLE IF NOT EXISTS user_item_data (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  playback_position_ticks BIGINT NOT NULL DEFAULT 0,
  play_count INTEGER NOT NULL DEFAULT 0,
  is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
  played BOOLEAN NOT NULL DEFAULT FALSE,
  last_played_date TIMESTAMP,
  PRIMARY KEY (user_id, item_id)
);
