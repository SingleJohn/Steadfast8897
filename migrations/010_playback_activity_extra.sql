ALTER TABLE playback_activity ADD COLUMN IF NOT EXISTS client_ip VARCHAR(45);
ALTER TABLE playback_activity ADD COLUMN IF NOT EXISTS series_name TEXT;
