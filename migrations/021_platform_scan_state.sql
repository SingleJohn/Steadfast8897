-- 021: Platform scan state

ALTER TABLE items
  ADD COLUMN IF NOT EXISTS platform_scan_status VARCHAR(20) NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS platform_scan_source VARCHAR(20),
  ADD COLUMN IF NOT EXISTS platform_scan_error TEXT,
  ADD COLUMN IF NOT EXISTS platform_scanned_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_items_platform_scan_status
  ON items (platform_scan_status)
  WHERE type IN ('Movie', 'Series');

CREATE INDEX IF NOT EXISTS idx_items_platform_scan_tmdb
  ON items (platform_scan_status, tmdb_id)
  WHERE type IN ('Movie', 'Series');

UPDATE items
SET platform_scan_status = 'matched',
    platform_scan_source = COALESCE(platform_scan_source, 'legacy'),
    platform_scanned_at = COALESCE(platform_scanned_at, NOW())
WHERE studio IS NOT NULL
  AND studio <> '';

UPDATE items
SET platform_scan_status = 'no_match',
    platform_scan_source = COALESCE(platform_scan_source, 'legacy'),
    platform_scanned_at = COALESCE(platform_scanned_at, NOW())
WHERE studio = '';

UPDATE items
SET studio = NULL
WHERE studio = '';
