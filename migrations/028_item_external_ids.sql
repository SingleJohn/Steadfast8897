CREATE TABLE IF NOT EXISTS item_external_ids (
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (item_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_item_external_ids_lookup
    ON item_external_ids (provider, external_id);

INSERT INTO item_external_ids (item_id, provider, external_id, updated_at)
SELECT id, 'tmdb', tmdb_id::text, NOW()
FROM items
WHERE tmdb_id IS NOT NULL AND tmdb_id > 0
ON CONFLICT (item_id, provider) DO UPDATE
SET external_id = EXCLUDED.external_id,
    updated_at = EXCLUDED.updated_at;

INSERT INTO item_external_ids (item_id, provider, external_id, updated_at)
SELECT id, 'imdb', imdb_id, NOW()
FROM items
WHERE imdb_id IS NOT NULL AND btrim(imdb_id) <> ''
ON CONFLICT (item_id, provider) DO UPDATE
SET external_id = EXCLUDED.external_id,
    updated_at = EXCLUDED.updated_at;
