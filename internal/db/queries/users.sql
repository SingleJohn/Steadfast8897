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

-- name: SetUserDisabled :exec
UPDATE users SET is_disabled = $1 WHERE id = $2;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_date = NOW(), last_activity_date = NOW() WHERE id = $1;

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

-- name: ListUserLibraryAccess :many
SELECT library_id FROM user_library_access WHERE user_id = $1;

-- name: ClearUserLibraryAccess :exec
DELETE FROM user_library_access WHERE user_id = $1;

-- name: AddUserLibraryAccess :exec
INSERT INTO user_library_access (user_id, library_id) VALUES ($1, $2);
