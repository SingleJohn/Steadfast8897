-- 020: Library sort order
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0;
