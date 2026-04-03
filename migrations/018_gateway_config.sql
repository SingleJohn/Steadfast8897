-- Gateway configuration tables for 302 redirect engine

CREATE TABLE IF NOT EXISTS gateway_config (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gateway_request_logs (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tag VARCHAR(16) NOT NULL DEFAULT '',
    source_id VARCHAR(64) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    method VARCHAR(16) NOT NULL DEFAULT '',
    path VARCHAR(512) NOT NULL DEFAULT '',
    query VARCHAR(2048) NOT NULL DEFAULT '',
    status INT NOT NULL DEFAULT 0,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    emby_user_id VARCHAR(32) NOT NULL DEFAULT '',
    emby_user_name VARCHAR(256) NOT NULL DEFAULT '',
    redirect_backend VARCHAR(64) NOT NULL DEFAULT '',
    redirect_source VARCHAR(32) NOT NULL DEFAULT '',
    redirect_location TEXT NOT NULL DEFAULT '',
    redirect_trace TEXT NOT NULL DEFAULT '',
    object_key TEXT NOT NULL DEFAULT '',
    route_id VARCHAR(128) NOT NULL DEFAULT '',
    pool_id VARCHAR(128) NOT NULL DEFAULT '',
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    referer VARCHAR(512) NOT NULL DEFAULT '',
    headers TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_grl_created_at ON gateway_request_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_grl_tag ON gateway_request_logs (tag);
CREATE INDEX IF NOT EXISTS idx_grl_source_id ON gateway_request_logs (source_id);
CREATE INDEX IF NOT EXISTS idx_grl_client_ip ON gateway_request_logs (client_ip);
CREATE INDEX IF NOT EXISTS idx_grl_status ON gateway_request_logs (status);
CREATE INDEX IF NOT EXISTS idx_grl_tag_status_created ON gateway_request_logs (tag, status, created_at);
CREATE INDEX IF NOT EXISTS idx_grl_redirect_backend ON gateway_request_logs (redirect_backend);
CREATE INDEX IF NOT EXISTS idx_grl_emby_user_id ON gateway_request_logs (emby_user_id);
CREATE INDEX IF NOT EXISTS idx_grl_route_id ON gateway_request_logs (route_id);
CREATE INDEX IF NOT EXISTS idx_grl_pool_id ON gateway_request_logs (pool_id);

CREATE TABLE IF NOT EXISTS gateway_daily_stats (
    id BIGSERIAL PRIMARY KEY,
    day DATE NOT NULL,
    tag VARCHAR(16) NOT NULL DEFAULT '',
    source_id VARCHAR(64) NOT NULL DEFAULT '',
    requests BIGINT NOT NULL DEFAULT 0,
    redirects302 BIGINT NOT NULL DEFAULT 0,
    status4xx BIGINT NOT NULL DEFAULT 0,
    status5xx BIGINT NOT NULL DEFAULT 0,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    latency_ms_sum BIGINT NOT NULL DEFAULT 0,
    latency_ms_max BIGINT NOT NULL DEFAULT 0,
    latency_ms_min BIGINT NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (day, tag, source_id)
);

CREATE TABLE IF NOT EXISTS gateway_ip_stats (
    source_id VARCHAR(64) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seen_count BIGINT NOT NULL DEFAULT 0,
    country VARCHAR(64) NOT NULL DEFAULT '',
    prov VARCHAR(64) NOT NULL DEFAULT '',
    city VARCHAR(64) NOT NULL DEFAULT '',
    isp VARCHAR(128) NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, ip)
);
