-- 019: Platform libraries (virtual libraries by studio/network)

-- Add studio column to items
ALTER TABLE items ADD COLUMN IF NOT EXISTS studio VARCHAR(200);
CREATE INDEX IF NOT EXISTS idx_items_studio ON items (studio) WHERE studio IS NOT NULL;

-- Platform libraries configuration
CREATE TABLE IF NOT EXISTS platform_libraries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  platform_name VARCHAR(100) NOT NULL UNIQUE,
  enabled BOOLEAN NOT NULL DEFAULT false,
  collection_type VARCHAR(50) NOT NULL DEFAULT 'mixed',
  icon_url VARCHAR(500),
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Preset platforms
INSERT INTO platform_libraries (platform_name, enabled) VALUES
  ('Netflix', false),
  ('HBO', false),
  ('Disney+', false),
  ('Apple TV+', false),
  ('Amazon', false),
  ('Hulu', false),
  ('Paramount+', false),
  ('Peacock', false)
ON CONFLICT (platform_name) DO NOTHING;

-- System config toggle
INSERT INTO system_config (key, value) VALUES ('platform_libraries_enabled', 'false')
ON CONFLICT (key) DO NOTHING;
