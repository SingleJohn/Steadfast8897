ALTER TABLE source_library_views
  ADD COLUMN IF NOT EXISTS cover_image_path TEXT,
  ADD COLUMN IF NOT EXISTS cover_image_tag TEXT;
