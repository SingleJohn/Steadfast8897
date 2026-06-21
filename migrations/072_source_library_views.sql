CREATE TABLE IF NOT EXISTS source_library_views (
  id BIGSERIAL PRIMARY KEY,
  public_uuid UUID NOT NULL,           -- NewSHA1(sourceLibNamespace, dimension + '\x00' + match_value)
  name TEXT NOT NULL,
  display_name TEXT,
  dimension TEXT NOT NULL,             -- normalized_kind | region | kind_region | provider | custom
  match_value TEXT NOT NULL,
  match_values TEXT[] NOT NULL DEFAULT '{}',
  collection_type TEXT NOT NULL DEFAULT 'mixed',
  provider_ids BIGINT[] NOT NULL DEFAULT '{}',
  filter JSONB NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  expose_to_emby BOOLEAN NOT NULL DEFAULT FALSE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  config JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(dimension, match_value)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_source_library_views_public_uuid ON source_library_views(public_uuid);
