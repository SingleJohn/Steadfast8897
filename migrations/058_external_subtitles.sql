CREATE TABLE IF NOT EXISTS external_subtitles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  media_version_id UUID NOT NULL REFERENCES media_versions(id) ON DELETE CASCADE,
  file_path VARCHAR(1000) NOT NULL,
  codec VARCHAR(20) NOT NULL,
  language VARCHAR(20),
  title VARCHAR(255),
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  is_forced BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_subtitles_version_path
  ON external_subtitles (media_version_id, file_path);

CREATE INDEX IF NOT EXISTS idx_external_subtitles_item_id
  ON external_subtitles (item_id);

CREATE INDEX IF NOT EXISTS idx_external_subtitles_media_version_id
  ON external_subtitles (media_version_id);
