CREATE TABLE IF NOT EXISTS source_config_imports (
  id BIGSERIAL PRIMARY KEY,
  source_type TEXT NOT NULL DEFAULT 'tvbox',
  name TEXT NOT NULL,
  source_url TEXT,
  base_url TEXT,
  content_sha256 TEXT NOT NULL,
  spider_ref TEXT,
  spider_md5 TEXT,
  raw_config JSONB NOT NULL,
  import_status TEXT NOT NULL DEFAULT 'active',   -- active|disabled|invalid|superseded
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  imported_by UUID REFERENCES users(id) ON DELETE SET NULL,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(source_type, content_sha256)
);
