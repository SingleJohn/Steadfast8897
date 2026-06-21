CREATE TABLE IF NOT EXISTS source_runtime_artifacts (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT REFERENCES source_providers(id) ON DELETE SET NULL,
  source_type TEXT NOT NULL DEFAULT 'tvbox',
  artifact_kind TEXT NOT NULL, -- drpy_engine|drpy_rule|sidecar_asset
  name TEXT NOT NULL,
  source_url TEXT NOT NULL,
  base_url TEXT,
  relative_path TEXT,
  local_path TEXT NOT NULL,
  md5 TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  byte_size BIGINT NOT NULL DEFAULT 0,
  content_type TEXT,
  trust_status TEXT NOT NULL DEFAULT 'unverified',
  status TEXT NOT NULL DEFAULT 'active',
  last_fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  verified_at TIMESTAMPTZ,
  last_error TEXT,
  raw JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(artifact_kind, sha256)
);

CREATE INDEX IF NOT EXISTS idx_source_runtime_artifacts_provider
  ON source_runtime_artifacts(provider_id);

CREATE INDEX IF NOT EXISTS idx_source_runtime_artifacts_url
  ON source_runtime_artifacts(source_url);
