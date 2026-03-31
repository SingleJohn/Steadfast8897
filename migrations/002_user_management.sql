-- Extend users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_disabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_date TIMESTAMP;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_activity_date TIMESTAMP;

-- User policies table (1:1 with users)
CREATE TABLE IF NOT EXISTS user_policies (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  is_administrator BOOLEAN NOT NULL DEFAULT FALSE,
  enable_all_folders BOOLEAN NOT NULL DEFAULT TRUE,
  enable_remote_access BOOLEAN NOT NULL DEFAULT TRUE,
  enable_media_playback BOOLEAN NOT NULL DEFAULT TRUE,
  enable_audio_transcoding BOOLEAN NOT NULL DEFAULT TRUE,
  enable_video_transcoding BOOLEAN NOT NULL DEFAULT TRUE,
  enable_playback_remuxing BOOLEAN NOT NULL DEFAULT TRUE,
  enable_content_deletion BOOLEAN NOT NULL DEFAULT FALSE,
  enable_content_downloading BOOLEAN NOT NULL DEFAULT TRUE,
  enable_subtitle_management BOOLEAN NOT NULL DEFAULT TRUE,
  enable_live_tv_access BOOLEAN NOT NULL DEFAULT TRUE,
  enable_live_tv_management BOOLEAN NOT NULL DEFAULT FALSE,
  enable_user_preference_access BOOLEAN NOT NULL DEFAULT TRUE,
  enable_remote_control BOOLEAN NOT NULL DEFAULT FALSE,
  enable_shared_device_control BOOLEAN NOT NULL DEFAULT FALSE,
  max_parental_rating INTEGER,
  remote_client_bitrate_limit INTEGER NOT NULL DEFAULT 0,
  simultaneous_stream_limit INTEGER NOT NULL DEFAULT 0,
  invalid_login_attempt_count INTEGER NOT NULL DEFAULT 0,
  login_attempts_before_lockout INTEGER NOT NULL DEFAULT 0
);

-- User library access table (many-to-many)
CREATE TABLE IF NOT EXISTS user_library_access (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, library_id)
);

-- Migrate existing users: create policy rows for existing users
INSERT INTO user_policies (user_id, is_administrator, enable_content_deletion)
SELECT id, is_admin, is_admin FROM users
ON CONFLICT (user_id) DO NOTHING;
