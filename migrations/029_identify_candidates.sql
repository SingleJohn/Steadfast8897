CREATE TABLE IF NOT EXISTS identify_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title TEXT,
    year INT,
    poster_url TEXT,
    score REAL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_identify_candidates_item
    ON identify_candidates (item_id);

CREATE INDEX IF NOT EXISTS idx_identify_candidates_created_at
    ON identify_candidates (created_at DESC);
