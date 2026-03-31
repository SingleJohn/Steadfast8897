-- Add integer emby_id for gateway compatibility
ALTER TABLE items ADD COLUMN IF NOT EXISTS emby_id SERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_items_emby_id ON items(emby_id);
