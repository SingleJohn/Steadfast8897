-- Step 1: Clean up ALL existing duplicates first (keep the oldest)
DELETE FROM items WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY library_id, type, name, COALESCE(production_year, 0)
      ORDER BY created_at ASC
    ) as rn
    FROM items WHERE type = 'Movie'
  ) sub WHERE rn > 1
);

DELETE FROM items WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY library_id, name
      ORDER BY created_at ASC
    ) as rn
    FROM items WHERE type = 'Series'
  ) sub WHERE rn > 1
);

DELETE FROM items WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY parent_id, index_number
      ORDER BY created_at ASC
    ) as rn
    FROM items WHERE type = 'Season'
  ) sub WHERE rn > 1
);

-- Dedup file_path (keep oldest)
DELETE FROM items WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY file_path
      ORDER BY created_at ASC
    ) as rn
    FROM items WHERE file_path IS NOT NULL
  ) sub WHERE rn > 1
);

-- Dedup media_versions
DELETE FROM media_versions WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY item_id, file_path
      ORDER BY created_at ASC
    ) as rn
    FROM media_versions
  ) sub WHERE rn > 1
);

-- Also clean orphaned media_versions and media_streams
DELETE FROM media_versions WHERE item_id NOT IN (SELECT id FROM items);
DELETE FROM media_streams WHERE item_id NOT IN (SELECT id FROM items);

-- Step 2: Now add unique constraints
CREATE UNIQUE INDEX IF NOT EXISTS idx_items_movie_unique
  ON items (library_id, type, name, COALESCE(production_year, 0))
  WHERE type = 'Movie';

CREATE UNIQUE INDEX IF NOT EXISTS idx_items_series_unique
  ON items (library_id, name)
  WHERE type = 'Series';

CREATE UNIQUE INDEX IF NOT EXISTS idx_items_season_unique
  ON items (parent_id, index_number)
  WHERE type = 'Season';

CREATE UNIQUE INDEX IF NOT EXISTS idx_items_filepath_unique
  ON items (file_path)
  WHERE file_path IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_versions_unique
  ON media_versions (item_id, file_path);
