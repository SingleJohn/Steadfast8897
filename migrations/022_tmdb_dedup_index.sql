-- Index to accelerate platform virtual library dedup queries
-- (studio + tmdb_id used for DISTINCT ON grouping)
CREATE INDEX IF NOT EXISTS idx_items_studio_tmdb
  ON items (studio, tmdb_id) WHERE studio IS NOT NULL AND tmdb_id IS NOT NULL;
