-- 081: dynamic latest-movie virtual library
--
-- Existing platform_libraries rows are dimension-based virtual libraries. The
-- latest rule reuses the same identity, cover, ordering and enablement model,
-- while item_limit caps its dynamic membership.

ALTER TABLE platform_libraries
  ADD COLUMN IF NOT EXISTS item_limit INTEGER;

CREATE INDEX IF NOT EXISTS idx_items_latest_virtual_movies
  ON items (created_at DESC, id DESC)
  WHERE type = 'Movie' AND merged_to_id IS NULL;
