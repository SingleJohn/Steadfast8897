-- Accelerate getLatestItems/getLatestBatch: library + type + updated_at DESC
CREATE INDEX IF NOT EXISTS idx_items_library_type_updated
  ON items(library_id, type, updated_at DESC);
