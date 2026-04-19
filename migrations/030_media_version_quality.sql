-- M7.3: media_versions 画质标签结构化字段
-- 为每个版本记录 resolution/HDR/codec/source/label,前端展示"4K HDR"胶囊。
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS resolution    VARCHAR(16);
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS hdr_format    VARCHAR(16);
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS video_codec   VARCHAR(16);
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS audio_codec   VARCHAR(16);
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS source        VARCHAR(16);
ALTER TABLE media_versions ADD COLUMN IF NOT EXISTS quality_label VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_media_versions_resolution
    ON media_versions (resolution) WHERE resolution IS NOT NULL;
