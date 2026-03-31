-- Store resolved real path for strm files (avoids runtime fs.readFileSync)
ALTER TABLE items ADD COLUMN IF NOT EXISTS resolved_path TEXT;
