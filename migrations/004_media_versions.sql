-- Store multiple media versions per item (e.g. multiple strm files = different resolutions)
CREATE TABLE IF NOT EXISTS media_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  name VARCHAR(500) NOT NULL,
  file_path VARCHAR(1000) NOT NULL,
  container VARCHAR(20),
  is_primary BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_media_versions_item_id ON media_versions(item_id);
