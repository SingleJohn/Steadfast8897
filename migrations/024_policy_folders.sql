-- 024: Add blocked/enabled folders to user policies
ALTER TABLE user_policies ADD COLUMN IF NOT EXISTS blocked_media_folders TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE user_policies ADD COLUMN IF NOT EXISTS enabled_folders TEXT[] NOT NULL DEFAULT '{}';
