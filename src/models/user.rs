use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};
use sqlx::PgPool;
use uuid::Uuid;

use crate::error::{AppError, AppResult};

#[derive(Debug, Clone, sqlx::FromRow, Serialize)]
pub struct User {
    pub id: Uuid,
    pub name: String,
    pub password_hash: String,
    pub is_admin: bool,
    pub is_disabled: bool,
    pub is_hidden: bool,
    pub last_login_date: Option<NaiveDateTime>,
    pub last_activity_date: Option<NaiveDateTime>,
    pub created_at: NaiveDateTime,
    pub emby_password_hash: Option<String>,
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct UserPolicy {
    pub user_id: Uuid,
    pub is_administrator: bool,
    pub enable_all_folders: bool,
    pub enable_remote_access: bool,
    pub enable_media_playback: bool,
    pub enable_audio_transcoding: bool,
    pub enable_video_transcoding: bool,
    pub enable_playback_remuxing: bool,
    pub enable_content_deletion: bool,
    pub enable_content_downloading: bool,
    pub enable_subtitle_management: bool,
    pub enable_live_tv_access: bool,
    pub enable_live_tv_management: bool,
    pub enable_user_preference_access: bool,
    pub enable_remote_control: bool,
    pub enable_shared_device_control: bool,
    pub max_parental_rating: Option<i32>,
    pub remote_client_bitrate_limit: i32,
    pub simultaneous_stream_limit: i32,
    pub invalid_login_attempt_count: i32,
    pub login_attempts_before_lockout: i32,
}

pub async fn find_user_by_name(pool: &PgPool, name: &str) -> AppResult<Option<User>> {
    let user = sqlx::query_as::<_, User>("SELECT * FROM users WHERE name = $1")
        .bind(name)
        .fetch_optional(pool)
        .await?;
    Ok(user)
}

pub async fn find_user_by_id(pool: &PgPool, id: &Uuid) -> AppResult<Option<User>> {
    let user = sqlx::query_as::<_, User>("SELECT * FROM users WHERE id = $1")
        .bind(id)
        .fetch_optional(pool)
        .await?;
    Ok(user)
}

pub async fn get_public_users(pool: &PgPool) -> AppResult<Vec<User>> {
    let users = sqlx::query_as::<_, User>(
        "SELECT * FROM users WHERE is_hidden = FALSE AND is_disabled = FALSE ORDER BY name",
    )
    .fetch_all(pool)
    .await?;
    Ok(users)
}

pub async fn get_all_users(pool: &PgPool) -> AppResult<Vec<User>> {
    let users = sqlx::query_as::<_, User>("SELECT * FROM users ORDER BY name")
        .fetch_all(pool)
        .await?;
    Ok(users)
}

pub async fn get_user_count(pool: &PgPool) -> AppResult<i64> {
    let count: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM users")
        .fetch_one(pool)
        .await?;
    Ok(count.0)
}

pub async fn create_user(
    pool: &PgPool,
    name: &str,
    password: &str,
    is_admin: bool,
) -> AppResult<User> {
    let hash = bcrypt::hash(password, 10)
        .map_err(|e| AppError::Internal(anyhow::anyhow!("bcrypt error: {e}")))?;

    let user = sqlx::query_as::<_, User>(
        "INSERT INTO users (name, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(name)
    .bind(&hash)
    .bind(is_admin)
    .fetch_one(pool)
    .await?;

    // Create default policy
    sqlx::query(
        "INSERT INTO user_policies (user_id, is_administrator, enable_content_deletion) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
    )
    .bind(user.id)
    .bind(is_admin)
    .bind(is_admin)
    .execute(pool)
    .await?;

    Ok(user)
}

pub async fn update_user(
    pool: &PgPool,
    id: &Uuid,
    name: Option<&str>,
    is_hidden: Option<bool>,
) -> AppResult<Option<User>> {
    if name.is_none() && is_hidden.is_none() {
        return find_user_by_id(pool, id).await;
    }

    if let Some(n) = name {
        sqlx::query("UPDATE users SET name = $1 WHERE id = $2")
            .bind(n)
            .bind(id)
            .execute(pool)
            .await?;
    }
    if let Some(h) = is_hidden {
        sqlx::query("UPDATE users SET is_hidden = $1 WHERE id = $2")
            .bind(h)
            .bind(id)
            .execute(pool)
            .await?;
    }

    find_user_by_id(pool, id).await
}

pub async fn delete_user(pool: &PgPool, id: &Uuid) -> AppResult<()> {
    sqlx::query("DELETE FROM users WHERE id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn update_password(pool: &PgPool, id: &Uuid, new_password: &str) -> AppResult<()> {
    let hash = bcrypt::hash(new_password, 10)
        .map_err(|e| AppError::Internal(anyhow::anyhow!("bcrypt error: {e}")))?;
    sqlx::query("UPDATE users SET password_hash = $1 WHERE id = $2")
        .bind(&hash)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn verify_password(pool: &PgPool, user: &User, password: &str) -> bool {
    // 1. Try bcrypt first
    if bcrypt::verify(password, &user.password_hash).unwrap_or(false) {
        return true;
    }
    // 2. Try Emby SHA1 fallback
    if let Some(ref emby_hash) = user.emby_password_hash {
        if !emby_hash.is_empty() {
            use sha1::Digest;
            let mut hasher = sha1::Sha1::new();
            hasher.update(password.as_bytes());
            let result = format!("{:X}", hasher.finalize());
            if result == *emby_hash {
                // Auto-upgrade to bcrypt
                if let Ok(new_hash) = bcrypt::hash(password, 10) {
                    sqlx::query("UPDATE users SET password_hash = $1, emby_password_hash = NULL WHERE id = $2")
                        .bind(&new_hash)
                        .bind(user.id)
                        .execute(pool)
                        .await
                        .ok();
                    tracing::info!("[Auth] Upgraded Emby password to bcrypt for user: {}", user.name);
                }
                return true;
            }
        }
    }
    false
}

pub async fn set_user_disabled(pool: &PgPool, id: &Uuid, disabled: bool) -> AppResult<()> {
    sqlx::query("UPDATE users SET is_disabled = $1 WHERE id = $2")
        .bind(disabled)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn update_last_login(pool: &PgPool, id: &Uuid) -> AppResult<()> {
    sqlx::query(
        "UPDATE users SET last_login_date = NOW(), last_activity_date = NOW() WHERE id = $1",
    )
    .bind(id)
    .execute(pool)
    .await?;
    Ok(())
}

// --- User policies ---

pub async fn get_user_policy(pool: &PgPool, user_id: &Uuid) -> AppResult<Option<UserPolicy>> {
    let policy =
        sqlx::query_as::<_, UserPolicy>("SELECT * FROM user_policies WHERE user_id = $1")
            .bind(user_id)
            .fetch_optional(pool)
            .await?;
    Ok(policy)
}

#[derive(Debug, Default, Deserialize)]
pub struct PolicyUpdate {
    pub is_administrator: Option<bool>,
    pub enable_all_folders: Option<bool>,
    pub enable_remote_access: Option<bool>,
    pub enable_media_playback: Option<bool>,
    pub enable_audio_transcoding: Option<bool>,
    pub enable_video_transcoding: Option<bool>,
    pub enable_playback_remuxing: Option<bool>,
    pub enable_content_deletion: Option<bool>,
    pub enable_content_downloading: Option<bool>,
    pub enable_subtitle_management: Option<bool>,
    pub enable_live_tv_access: Option<bool>,
    pub enable_live_tv_management: Option<bool>,
    pub enable_user_preference_access: Option<bool>,
    pub enable_remote_control: Option<bool>,
    pub enable_shared_device_control: Option<bool>,
    pub max_parental_rating: Option<i32>,
    pub remote_client_bitrate_limit: Option<i32>,
    pub simultaneous_stream_limit: Option<i32>,
}

pub async fn upsert_user_policy(
    pool: &PgPool,
    user_id: &Uuid,
    policy: &PolicyUpdate,
) -> AppResult<()> {
    // Ensure row exists
    sqlx::query("INSERT INTO user_policies (user_id) VALUES ($1) ON CONFLICT DO NOTHING")
        .bind(user_id)
        .execute(pool)
        .await?;

    // Build dynamic SET clause
    let mut sets = Vec::new();
    let mut idx = 1u32;

    macro_rules! maybe_set {
        ($field:ident) => {
            if policy.$field.is_some() {
                sets.push((stringify!($field), idx));
                idx += 1;
            }
        };
    }

    maybe_set!(is_administrator);
    maybe_set!(enable_all_folders);
    maybe_set!(enable_remote_access);
    maybe_set!(enable_media_playback);
    maybe_set!(enable_audio_transcoding);
    maybe_set!(enable_video_transcoding);
    maybe_set!(enable_playback_remuxing);
    maybe_set!(enable_content_deletion);
    maybe_set!(enable_content_downloading);
    maybe_set!(enable_subtitle_management);
    maybe_set!(enable_live_tv_access);
    maybe_set!(enable_live_tv_management);
    maybe_set!(enable_user_preference_access);
    maybe_set!(enable_remote_control);
    maybe_set!(enable_shared_device_control);
    maybe_set!(remote_client_bitrate_limit);
    maybe_set!(simultaneous_stream_limit);

    if sets.is_empty() {
        return Ok(());
    }

    // For simplicity, update each field individually
    macro_rules! update_field {
        ($field:ident, bool) => {
            if let Some(val) = policy.$field {
                let sql = format!(
                    "UPDATE user_policies SET {} = $1 WHERE user_id = $2",
                    stringify!($field)
                );
                sqlx::query(&sql)
                    .bind(val)
                    .bind(user_id)
                    .execute(pool)
                    .await?;
            }
        };
        ($field:ident, i32) => {
            if let Some(val) = policy.$field {
                let sql = format!(
                    "UPDATE user_policies SET {} = $1 WHERE user_id = $2",
                    stringify!($field)
                );
                sqlx::query(&sql)
                    .bind(val)
                    .bind(user_id)
                    .execute(pool)
                    .await?;
            }
        };
    }

    update_field!(is_administrator, bool);
    update_field!(enable_all_folders, bool);
    update_field!(enable_remote_access, bool);
    update_field!(enable_media_playback, bool);
    update_field!(enable_audio_transcoding, bool);
    update_field!(enable_video_transcoding, bool);
    update_field!(enable_playback_remuxing, bool);
    update_field!(enable_content_deletion, bool);
    update_field!(enable_content_downloading, bool);
    update_field!(enable_subtitle_management, bool);
    update_field!(enable_live_tv_access, bool);
    update_field!(enable_live_tv_management, bool);
    update_field!(enable_user_preference_access, bool);
    update_field!(enable_remote_control, bool);
    update_field!(enable_shared_device_control, bool);
    update_field!(remote_client_bitrate_limit, i32);
    update_field!(simultaneous_stream_limit, i32);

    // Sync is_admin flag on users table
    if let Some(is_admin) = policy.is_administrator {
        sqlx::query("UPDATE users SET is_admin = $1 WHERE id = $2")
            .bind(is_admin)
            .bind(user_id)
            .execute(pool)
            .await?;
    }

    Ok(())
}

pub fn format_policy_response(
    policy: Option<&UserPolicy>,
    is_admin: bool,
) -> serde_json::Value {
    let p = policy;
    serde_json::json!({
        "IsAdministrator": p.map(|p| p.is_administrator).unwrap_or(is_admin),
        "IsDisabled": false,
        "IsHidden": false,
        "EnableAllFolders": p.map(|p| p.enable_all_folders).unwrap_or(true),
        "EnableRemoteAccess": p.map(|p| p.enable_remote_access).unwrap_or(true),
        "EnableMediaPlayback": p.map(|p| p.enable_media_playback).unwrap_or(true),
        "EnableAudioPlaybackTranscoding": p.map(|p| p.enable_audio_transcoding).unwrap_or(true),
        "EnableVideoPlaybackTranscoding": p.map(|p| p.enable_video_transcoding).unwrap_or(true),
        "EnablePlaybackRemuxing": p.map(|p| p.enable_playback_remuxing).unwrap_or(true),
        "EnableContentDeletion": p.map(|p| p.enable_content_deletion).unwrap_or(is_admin),
        "EnableContentDownloading": p.map(|p| p.enable_content_downloading).unwrap_or(true),
        "EnableSubtitleDownloading": p.map(|p| p.enable_subtitle_management).unwrap_or(true),
        "EnableSubtitleManagement": p.map(|p| p.enable_subtitle_management).unwrap_or(true),
        "EnableLiveTvAccess": p.map(|p| p.enable_live_tv_access).unwrap_or(true),
        "EnableLiveTvManagement": p.map(|p| p.enable_live_tv_management).unwrap_or(false),
        "EnableUserPreferenceAccess": p.map(|p| p.enable_user_preference_access).unwrap_or(true),
        "EnableRemoteControlOfOtherUsers": p.map(|p| p.enable_remote_control).unwrap_or(false),
        "EnableSharedDeviceControl": p.map(|p| p.enable_shared_device_control).unwrap_or(false),
        "MaxParentalRating": p.and_then(|p| p.max_parental_rating),
        "RemoteClientBitrateLimit": p.map(|p| p.remote_client_bitrate_limit).unwrap_or(0),
        "SimultaneousStreamLimit": p.map(|p| p.simultaneous_stream_limit).unwrap_or(0),
        "EnableSyncTranscoding": true,
        "EnableMediaConversion": true,
    })
}

// --- Library access ---

pub async fn get_user_library_access(pool: &PgPool, user_id: &Uuid) -> AppResult<Vec<Uuid>> {
    let rows: Vec<(Uuid,)> =
        sqlx::query_as("SELECT library_id FROM user_library_access WHERE user_id = $1")
            .bind(user_id)
            .fetch_all(pool)
            .await?;
    Ok(rows.into_iter().map(|r| r.0).collect())
}

pub async fn set_user_library_access(
    pool: &PgPool,
    user_id: &Uuid,
    library_ids: &[Uuid],
) -> AppResult<()> {
    sqlx::query("DELETE FROM user_library_access WHERE user_id = $1")
        .bind(user_id)
        .execute(pool)
        .await?;
    for lib_id in library_ids {
        sqlx::query("INSERT INTO user_library_access (user_id, library_id) VALUES ($1, $2)")
            .bind(user_id)
            .bind(lib_id)
            .execute(pool)
            .await?;
    }
    Ok(())
}
