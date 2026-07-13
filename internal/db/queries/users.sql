-- name: GetUserByName :one
SELECT id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash
FROM users
WHERE name = $1;

-- name: GetUserByID :one
SELECT id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash
FROM users
WHERE id = $1;

-- name: ListVisibleUsers :many
SELECT id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash
FROM users
WHERE is_hidden = FALSE AND is_disabled = FALSE
ORDER BY name;

-- name: ListUsers :many
SELECT id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash
FROM users
ORDER BY name;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (name, password_hash, is_admin)
VALUES ($1, $2, $3)
RETURNING id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash;

-- name: EnsureUserPolicy :exec
INSERT INTO user_policies (user_id, is_administrator, enable_content_deletion)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: UpdateUserName :exec
UPDATE users SET name = $1 WHERE id = $2;

-- name: UpdateUserHidden :exec
UPDATE users SET is_hidden = $1 WHERE id = $2;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: UpdateUserPasswordHash :exec
UPDATE users SET password_hash = $1 WHERE id = $2;

-- name: UpgradeEmbyPasswordHash :exec
UPDATE users SET password_hash = $1, emby_password_hash = NULL WHERE id = $2;

-- name: UpdateUserAdmin :exec
UPDATE users SET is_admin = $1 WHERE id = $2;

-- name: SetUserDisabled :exec
UPDATE users SET is_disabled = $1 WHERE id = $2;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_date = NOW(), last_activity_date = NOW() WHERE id = $1;

-- name: UpdateLastActivity :exec
-- Emby 语义：任意用户活动（含播放）刷新 LastActivityDate。
-- Sakura_embyboss 活跃保号只读该字段，不读 playback_activity。
UPDATE users SET last_activity_date = NOW() WHERE id = $1;

-- name: GetUserPolicy :one
SELECT user_id, is_administrator, enable_all_folders, enable_remote_access,
       enable_media_playback, enable_audio_transcoding, enable_video_transcoding,
       enable_playback_remuxing, enable_content_deletion, enable_content_downloading,
       enable_subtitle_management, enable_live_tv_access, enable_live_tv_management,
       enable_user_preference_access, enable_remote_control, enable_shared_device_control,
       max_parental_rating, remote_client_bitrate_limit, simultaneous_stream_limit,
       invalid_login_attempt_count, login_attempts_before_lockout,
       blocked_media_folders, enabled_folders
FROM user_policies
WHERE user_id = $1;

-- name: EnsureUserPolicyDefaults :exec
INSERT INTO user_policies (user_id)
VALUES ($1)
ON CONFLICT DO NOTHING;

-- name: UpdateUserPolicyBlockedFolders :exec
UPDATE user_policies SET blocked_media_folders = $1 WHERE user_id = $2;

-- name: UpdateUserPolicyEnabledFolders :exec
UPDATE user_policies SET enabled_folders = $1 WHERE user_id = $2;

-- name: UpdateUserPolicyFields :exec
UPDATE user_policies SET
  is_administrator = COALESCE(sqlc.narg('is_administrator')::boolean, is_administrator),
  enable_all_folders = COALESCE(sqlc.narg('enable_all_folders')::boolean, enable_all_folders),
  enable_remote_access = COALESCE(sqlc.narg('enable_remote_access')::boolean, enable_remote_access),
  enable_media_playback = COALESCE(sqlc.narg('enable_media_playback')::boolean, enable_media_playback),
  enable_audio_transcoding = COALESCE(sqlc.narg('enable_audio_transcoding')::boolean, enable_audio_transcoding),
  enable_video_transcoding = COALESCE(sqlc.narg('enable_video_transcoding')::boolean, enable_video_transcoding),
  enable_playback_remuxing = COALESCE(sqlc.narg('enable_playback_remuxing')::boolean, enable_playback_remuxing),
  enable_content_deletion = COALESCE(sqlc.narg('enable_content_deletion')::boolean, enable_content_deletion),
  enable_content_downloading = COALESCE(sqlc.narg('enable_content_downloading')::boolean, enable_content_downloading),
  enable_subtitle_management = COALESCE(sqlc.narg('enable_subtitle_management')::boolean, enable_subtitle_management),
  enable_live_tv_access = COALESCE(sqlc.narg('enable_live_tv_access')::boolean, enable_live_tv_access),
  enable_live_tv_management = COALESCE(sqlc.narg('enable_live_tv_management')::boolean, enable_live_tv_management),
  enable_user_preference_access = COALESCE(sqlc.narg('enable_user_preference_access')::boolean, enable_user_preference_access),
  enable_remote_control = COALESCE(sqlc.narg('enable_remote_control')::boolean, enable_remote_control),
  enable_shared_device_control = COALESCE(sqlc.narg('enable_shared_device_control')::boolean, enable_shared_device_control),
  remote_client_bitrate_limit = COALESCE(sqlc.narg('remote_client_bitrate_limit')::int, remote_client_bitrate_limit),
  simultaneous_stream_limit = COALESCE(sqlc.narg('simultaneous_stream_limit')::int, simultaneous_stream_limit)
WHERE user_id = sqlc.arg('user_id');

-- name: GetItemLibraryIDForAccess :one
SELECT library_id::text
FROM items
WHERE id = $1::uuid;

-- name: ListUserLibraryAccess :many
SELECT library_id FROM user_library_access WHERE user_id = $1;

-- name: ClearUserLibraryAccess :exec
DELETE FROM user_library_access WHERE user_id = $1;

-- name: AddUserLibraryAccess :exec
INSERT INTO user_library_access (user_id, library_id) VALUES ($1, $2);
