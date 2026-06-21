CREATE TABLE IF NOT EXISTS source_providers (
  id BIGSERIAL PRIMARY KEY,
  config_id BIGINT REFERENCES source_config_imports(id) ON DELETE SET NULL,
  source_key TEXT NOT NULL,
  name TEXT NOT NULL,
  provider_kind TEXT NOT NULL,        -- phase1 仅 cms_vod；预留 tvbox_site/alist/...
  runtime_kind TEXT NOT NULL,         -- phase1 仅 native_cms；预留 csp_dex/js_quickjs/py_chaquopy/...
  tvbox_type INTEGER,
  api TEXT NOT NULL,
  ext JSONB NOT NULL DEFAULT '{}',
  categories JSONB NOT NULL DEFAULT '[]',
  headers JSONB NOT NULL DEFAULT '{}',
  capabilities JSONB NOT NULL DEFAULT '{}',
  timeout_ms INTEGER NOT NULL DEFAULT 8000,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  visible BOOLEAN NOT NULL DEFAULT TRUE,
  searchable BOOLEAN NOT NULL DEFAULT TRUE,
  health_status TEXT NOT NULL DEFAULT 'unknown',
  last_check_at TIMESTAMPTZ,
  last_error TEXT,
  raw_site JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(config_id, source_key)
);

-- 有效启用 = source_config_imports.enabled AND source_providers.enabled，由应用层计算。
