-- Emby SHA1 password hash for migration compatibility
ALTER TABLE users ADD COLUMN IF NOT EXISTS emby_password_hash VARCHAR(64);
