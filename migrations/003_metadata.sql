-- Genres
CREATE TABLE IF NOT EXISTS genres (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS item_genres (
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  genre_id UUID NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, genre_id)
);

-- Cast/Crew
CREATE TABLE IF NOT EXISTS cast_members (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  character VARCHAR(255),
  role VARCHAR(20) NOT NULL DEFAULT 'Actor',
  order_index INTEGER NOT NULL DEFAULT 0,
  image_url VARCHAR(1000),
  tmdb_id INTEGER
);

CREATE INDEX IF NOT EXISTS idx_cast_members_item_id ON cast_members(item_id);

-- Items table extensions
ALTER TABLE items ADD COLUMN IF NOT EXISTS tagline VARCHAR(500);
ALTER TABLE items ADD COLUMN IF NOT EXISTS tmdb_id INTEGER;
ALTER TABLE items ADD COLUMN IF NOT EXISTS imdb_id VARCHAR(20);

-- System configuration
CREATE TABLE IF NOT EXISTS system_config (
  key VARCHAR(100) PRIMARY KEY,
  value TEXT
);

-- Default config values
INSERT INTO system_config (key, value) VALUES
  ('tmdb_api_key', ''),
  ('tmdb_language', 'zh-CN'),
  ('auto_scrape_enabled', 'false')
ON CONFLICT (key) DO NOTHING;
