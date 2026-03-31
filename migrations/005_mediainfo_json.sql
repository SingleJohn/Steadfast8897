-- Store the raw mediainfo JSON data from companion files
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS mediainfo JSONB;
-- Also store size/bitrate/runtime extracted from JSON
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS size BIGINT;
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS bitrate INTEGER;
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS runtime_ticks BIGINT;
