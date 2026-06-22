CREATE TABLE IF NOT EXISTS source_parsers (
  id BIGSERIAL PRIMARY KEY,
  config_id BIGINT REFERENCES source_config_imports(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL DEFAULT 'tvbox',
  name TEXT NOT NULL,
  parser_type INTEGER NOT NULL DEFAULT 0, -- TVBox parse type: 0/1 URL template, 3 sniff
  url TEXT NOT NULL,
  base_url TEXT,
  timeout_ms INTEGER NOT NULL DEFAULT 8000,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  trust_status TEXT NOT NULL DEFAULT 'unverified',
  status TEXT NOT NULL DEFAULT 'active',
  last_check_at TIMESTAMPTZ,
  last_error TEXT,
  raw JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(config_id, name, url)
);

CREATE INDEX IF NOT EXISTS idx_source_parsers_config
  ON source_parsers(config_id);

CREATE INDEX IF NOT EXISTS idx_source_parsers_enabled
  ON source_parsers(enabled, status);

CREATE INDEX IF NOT EXISTS idx_source_parsers_url
  ON source_parsers(url);
