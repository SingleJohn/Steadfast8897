CREATE TABLE IF NOT EXISTS source_items (
  id BIGSERIAL PRIMARY KEY,
  public_uuid UUID NOT NULL,           -- NewSHA1(sourceItemNamespace, providerKey + '\x00' + source_item_id)
  provider_id BIGINT NOT NULL REFERENCES source_providers(id) ON DELETE CASCADE,
  source_item_id TEXT NOT NULL,
  source_parent_id TEXT,
  item_type TEXT NOT NULL DEFAULT 'unknown',   -- Movie|Series|Episode|Folder|unknown
  title TEXT NOT NULL,
  original_title TEXT,
  sort_title TEXT,
  year INTEGER,
  region TEXT,                         -- 归一后地区：CN/HK/TW/US/JP/KR/EU/...（驱动虚拟库）
  area TEXT,                           -- 来源原始 area 文本
  language TEXT,
  category_name TEXT,                  -- 来源原始 type_name
  normalized_kind TEXT NOT NULL DEFAULT 'unknown',  -- movie|series|anime|variety|documentary|...
  season_number INTEGER,
  episode_number INTEGER,
  poster_url TEXT,
  backdrop_url TEXT,
  remarks TEXT,
  summary TEXT,
  directors TEXT[] NOT NULL DEFAULT '{}',
  actors TEXT[] NOT NULL DEFAULT '{}',
  provider_ids JSONB NOT NULL DEFAULT '{}',
  raw JSONB NOT NULL DEFAULT '{}',
  detail_loaded BOOLEAN NOT NULL DEFAULT FALSE,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(provider_id, source_item_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_source_items_public_uuid ON source_items(public_uuid);
CREATE INDEX IF NOT EXISTS idx_source_items_kind_region ON source_items(normalized_kind, region);
CREATE INDEX IF NOT EXISTS idx_source_items_provider_seen ON source_items(provider_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_source_items_title ON source_items(provider_id, lower(title));
