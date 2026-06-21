CREATE TABLE IF NOT EXISTS source_play_sources (
  id BIGSERIAL PRIMARY KEY,
  public_uuid UUID NOT NULL,           -- NewSHA1(playSourceNamespace, sourceItemUUID+lineName+episodeKey)
  source_item_id BIGINT NOT NULL REFERENCES source_items(id) ON DELETE CASCADE,
  provider_id BIGINT NOT NULL REFERENCES source_providers(id) ON DELETE CASCADE,
  line_name TEXT NOT NULL,
  episode_title TEXT NOT NULL DEFAULT '',
  episode_key TEXT NOT NULL DEFAULT '',
  episode_number INTEGER,
  raw_url TEXT NOT NULL,
  parse_mode TEXT NOT NULL DEFAULT 'unknown',  -- direct|resolver|magnet|cloud_share|live|unsupported|unknown
  flag TEXT,
  headers JSONB NOT NULL DEFAULT '{}',
  resolver_payload JSONB NOT NULL DEFAULT '{}',
  sort_order INTEGER NOT NULL DEFAULT 0,
  health_status TEXT NOT NULL DEFAULT 'unknown',
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  avg_latency_ms INTEGER,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(source_item_id, line_name, episode_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_source_play_sources_public_uuid ON source_play_sources(public_uuid);
