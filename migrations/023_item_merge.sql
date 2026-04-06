-- Persistent multi-version merge: secondary items point to their primary
ALTER TABLE items ADD COLUMN IF NOT EXISTS merged_to_id UUID REFERENCES items(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_items_merged_to
  ON items (merged_to_id) WHERE merged_to_id IS NOT NULL;
