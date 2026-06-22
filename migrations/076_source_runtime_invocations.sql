CREATE TABLE IF NOT EXISTS source_runtime_invocations (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT REFERENCES source_providers(id) ON DELETE SET NULL,
  runtime_kind TEXT NOT NULL,
  method TEXT NOT NULL,
  status TEXT NOT NULL,
  error_type TEXT,
  error_message TEXT,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  engine_ok BOOLEAN,
  worker_pid INTEGER,
  artifact_ids BIGINT[] NOT NULL DEFAULT '{}',
  url_hash TEXT,
  raw JSONB NOT NULL DEFAULT '{}',
  invoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_source_runtime_invocations_provider
  ON source_runtime_invocations(provider_id, invoked_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_runtime_invocations_method
  ON source_runtime_invocations(method, invoked_at DESC);

CREATE INDEX IF NOT EXISTS idx_source_runtime_invocations_status
  ON source_runtime_invocations(status, invoked_at DESC);
