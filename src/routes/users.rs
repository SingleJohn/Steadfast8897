use axum::extract::{Path, State};
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use std::sync::Arc;
use uuid::Uuid;

use crate::auth::{OptionalAuth, RequireAdmin, RequireAuth};
use crate::error::{AppError, AppResult};
use crate::models::session::{create_access_token, delete_token};
use crate::models::user::*;
use crate::state::AppState;

// Login rate limiter: track failed attempts per IP
static LOGIN_FAILURES: std::sync::LazyLock<dashmap::DashMap<String, (u32, std::time::Instant)>> =
    std::sync::LazyLock::new(|| dashmap::DashMap::new());

fn check_login_rate(ip: &str) -> Result<(), AppError> {
    if let Some(entry) = LOGIN_FAILURES.get(ip) {
        let (count, last_attempt) = entry.value();
        let elapsed = last_attempt.elapsed().as_secs();
        // Reset after 15 minutes
        if elapsed > 900 { return Ok(()); }
        // Block after 10 failures within 15 min
        if *count >= 10 {
            return Err(AppError::Forbidden(format!("Too many login attempts. Try again in {} seconds.", 900 - elapsed)));
        }
    }
    Ok(())
}

fn record_login_failure(ip: &str) {
    let mut entry = LOGIN_FAILURES.entry(ip.to_string()).or_insert((0, std::time::Instant::now()));
    if entry.1.elapsed().as_secs() > 900 {
        *entry = (1, std::time::Instant::now());
    } else {
        entry.0 += 1;
    }
}

fn clear_login_failures(ip: &str) {
    LOGIN_FAILURES.remove(ip);
}

async fn build_user_response(
    pool: &sqlx::PgPool,
    user: &User,
    include_config: bool,
    server_id: &str,
) -> AppResult<serde_json::Value> {
    let policy = get_user_policy(pool, &user.id).await?;
    let mut policy_json = format_policy_response(policy.as_ref(), user.is_admin);
    // Overlay IsDisabled / IsHidden from user record
    if let Some(obj) = policy_json.as_object_mut() {
        obj.insert("IsDisabled".into(), json!(user.is_disabled));
        obj.insert("IsHidden".into(), json!(user.is_hidden));
    }

    let mut resp = json!({
        "Name": user.name,
        "ServerId": server_id,
        "Id": user.id.to_string(),
        "HasPassword": true,
        "HasConfiguredPassword": true,
        "HasConfiguredEasyPassword": false,
        "Policy": policy_json,
    });

    if let Some(d) = user.last_login_date {
        resp["LastLoginDate"] = json!(d.and_utc().to_rfc3339());
    }
    if let Some(d) = user.last_activity_date {
        resp["LastActivityDate"] = json!(d.and_utc().to_rfc3339());
    }

    if include_config {
        resp["Configuration"] = json!({
            "PlayDefaultAudioTrack": true,
            "SubtitleLanguagePreference": "",
            "DisplayMissingEpisodes": false,
            "SubtitleMode": "Default",
            "EnableLocalPassword": false,
            "OrderedViews": [],
            "LatestItemsExcludes": [],
            "MyMediaExcludes": [],
            "HidePlayedInLatest": true,
            "RememberAudioSelections": true,
            "RememberSubtitleSelections": true,
            "EnableNextEpisodeAutoPlay": true,
        });
    }

    Ok(resp)
}

// --- Public routes ---

async fn get_public_users(State(state): State<Arc<AppState>>) -> AppResult<impl IntoResponse> {
    let users = get_public_users_db(&state.db).await?;
    let mut result = Vec::new();
    for u in &users {
        result.push(build_user_response(&state.db, u, false, &state.config.server_id).await?);
    }
    Ok(Json(json!(result)))
}

// Renamed to avoid conflict
async fn get_public_users_db(pool: &sqlx::PgPool) -> AppResult<Vec<User>> {
    crate::models::user::get_public_users(pool).await
}

async fn get_me(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
) -> AppResult<impl IntoResponse> {
    let uid: Uuid = user.id.parse().map_err(|_| AppError::Unauthorized)?;
    let u = find_user_by_id(&state.db, &uid)
        .await?
        .ok_or(AppError::NotFound)?;
    Ok(Json(
        build_user_response(&state.db, &u, true, &state.config.server_id).await?,
    ))
}

async fn get_all(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<impl IntoResponse> {
    let users = get_all_users(&state.db).await?;
    let mut result = Vec::new();
    for u in &users {
        result.push(build_user_response(&state.db, u, true, &state.config.server_id).await?);
    }
    Ok(Json(json!(result)))
}

#[derive(Deserialize)]
struct NewUserBody {
    #[serde(alias = "Name")]
    name: Option<String>,
    #[serde(alias = "Password")]
    password: Option<String>,
}

async fn create_new_user(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<NewUserBody>,
) -> AppResult<impl IntoResponse> {
    let name = body.name.as_deref().ok_or(AppError::BadRequest("Name is required".into()))?;
    if find_user_by_name(&state.db, name).await?.is_some() {
        return Err(AppError::BadRequest("User already exists".into()));
    }
    let user = create_user(&state.db, name, body.password.as_deref().unwrap_or(""), false).await?;
    Ok(Json(
        build_user_response(&state.db, &user, true, &state.config.server_id).await?,
    ))
}

#[derive(Deserialize)]
struct AuthByNameBody {
    #[serde(alias = "Username")]
    username: Option<String>,
    #[serde(alias = "Pw")]
    pw: Option<String>,
    #[serde(alias = "Password")]
    password: Option<String>,
}

async fn authenticate_by_name(
    State(state): State<Arc<AppState>>,
    uri: axum::http::Uri,
    OptionalAuth(_, mut auth_info): OptionalAuth,
    headers: axum::http::HeaderMap,
    body_bytes: axum::body::Bytes,
) -> AppResult<impl IntoResponse> {
    // Support both JSON and form-urlencoded body (some clients like AfuseKt use form)
    let content_type = headers.get("content-type").and_then(|v| v.to_str().ok()).unwrap_or("");
    let body: AuthByNameBody = if content_type.contains("application/json") {
        serde_json::from_slice(&body_bytes).map_err(|e| AppError::BadRequest(format!("Invalid JSON: {e}")))?
    } else {
        // Try form-urlencoded, then fallback to JSON
        serde_urlencoded::from_bytes(&body_bytes)
            .or_else(|_| serde_json::from_slice(&body_bytes))
            .map_err(|e| AppError::BadRequest(format!("Invalid body: {e}")))?
    };
    // Some clients (AfuseKt) pass auth info as query params instead of headers
    if let Some(qs) = uri.query() {
        let qp: std::collections::HashMap<String, String> = qs.split('&')
            .filter_map(|p| p.split_once('=').map(|(k, v)| (k.to_string(), urlencoding::decode(v).unwrap_or_default().to_string())))
            .collect();
        if auth_info.device_id.is_none() {
            auth_info.device_id = qp.get("X-Emby-Device-Id").cloned();
        }
        if auth_info.device.is_none() {
            auth_info.device = qp.get("X-Emby-Device-Name").cloned();
        }
        if auth_info.client.is_none() {
            auth_info.client = qp.get("X-Emby-Client").cloned();
        }
        if auth_info.version.is_none() {
            auth_info.version = qp.get("X-Emby-Client-Version").cloned();
        }
    }

    let client_ip = headers.get("x-forwarded-for")
        .and_then(|v| v.to_str().ok())
        .and_then(|s| s.split(',').next())
        .map(|s| s.trim().to_string())
        .or_else(|| headers.get("x-real-ip").and_then(|v| v.to_str().ok()).map(|s| s.to_string()))
        .unwrap_or_else(|| "unknown".to_string());

    // Rate limit check
    check_login_rate(&client_ip)?;

    let username = body
        .username
        .as_deref()
        .ok_or(AppError::BadRequest("Username is required".into()))?;
    let password = body.pw.as_deref().or(body.password.as_deref()).unwrap_or("");

    let user = find_user_by_name(&state.db, username)
        .await?
        .ok_or_else(|| { record_login_failure(&client_ip); AppError::Unauthorized })?;

    if user.is_disabled {
        record_login_failure(&client_ip);
        return Err(AppError::Unauthorized);
    }

    if !verify_password(&state.db, &user, password).await {
        record_login_failure(&client_ip);
        tracing::warn!("[Auth] Failed login attempt for user '{}' from {}", username, client_ip);
        return Err(AppError::Unauthorized);
    }

    clear_login_failures(&client_ip);
    update_last_login(&state.db, &user.id).await?;

    tracing::info!("[Auth] User '{}' logged in (device: {}, client: {})",
        username,
        auth_info.device.as_deref().unwrap_or("unknown"),
        auth_info.client.as_deref().unwrap_or("unknown"));

    let token = create_access_token(
        &state.db,
        &user.id,
        auth_info.device_id.as_deref().unwrap_or("unknown"),
        auth_info.device.as_deref().unwrap_or("Unknown Device"),
        auth_info.client.as_deref().unwrap_or("Unknown Client"),
        auth_info.version.as_deref().unwrap_or("0.0.0"),
    )
    .await?;

    let user_resp =
        build_user_response(&state.db, &user, true, &state.config.server_id).await?;

    Ok(Json(json!({
        "User": user_resp,
        "AccessToken": token,
        "ServerId": state.config.server_id,
    })))
}

async fn get_user_by_id(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(user_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let uid: Uuid = user_id.parse().map_err(|_| AppError::NotFound)?;
    let user = find_user_by_id(&state.db, &uid)
        .await?
        .ok_or(AppError::NotFound)?;
    Ok(Json(
        build_user_response(&state.db, &user, true, &state.config.server_id).await?,
    ))
}

#[derive(Deserialize)]
struct UpdateUserBody {
    #[serde(alias = "Name")]
    name: Option<String>,
    #[serde(alias = "Policy")]
    policy: Option<serde_json::Value>,
}

async fn update_user_handler(
    State(state): State<Arc<AppState>>,
    RequireAuth(auth_user, _): RequireAuth,
    Path(user_id): Path<String>,
    Json(body): Json<UpdateUserBody>,
) -> AppResult<impl IntoResponse> {
    let uid: Uuid = user_id.parse().map_err(|_| AppError::NotFound)?;
    if !auth_user.is_admin && auth_user.id != user_id {
        return Err(AppError::Forbidden("Forbidden".into()));
    }

    let new_name = body.name.as_deref();
    let mut new_hidden: Option<bool> = None;

    if let Some(ref p) = body.policy {
        if let Some(h) = p.get("IsHidden").and_then(|v| v.as_bool()) {
            new_hidden = Some(h);
        }
        if let Some(d) = p.get("IsDisabled").and_then(|v| v.as_bool()) {
            set_user_disabled(&state.db, &uid, d).await?;
        }
        // Update policy fields
        let policy_update = emby_policy_to_update(p);
        if has_any_field(&policy_update) {
            upsert_user_policy(&state.db, &uid, &policy_update).await?;
        }
    }

    update_user(&state.db, &uid, new_name, new_hidden).await?;

    let updated = find_user_by_id(&state.db, &uid)
        .await?
        .ok_or(AppError::NotFound)?;
    Ok(Json(
        build_user_response(&state.db, &updated, true, &state.config.server_id).await?,
    ))
}

async fn delete_user_handler(
    State(state): State<Arc<AppState>>,
    RequireAdmin(admin, _): RequireAdmin,
    Path(user_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    if admin.id == user_id {
        return Err(AppError::BadRequest("Cannot delete yourself".into()));
    }
    let uid: Uuid = user_id.parse().map_err(|_| AppError::NotFound)?;
    delete_user(&state.db, &uid).await?;
    Ok(StatusCode::NO_CONTENT)
}

async fn update_policy(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(user_id): Path<String>,
    Json(body): Json<serde_json::Value>,
) -> AppResult<impl IntoResponse> {
    let uid: Uuid = user_id.parse().map_err(|_| AppError::NotFound)?;

    if let Some(d) = body.get("IsDisabled").and_then(|v| v.as_bool()) {
        set_user_disabled(&state.db, &uid, d).await?;
    }
    if let Some(h) = body.get("IsHidden").and_then(|v| v.as_bool()) {
        update_user(&state.db, &uid, None, Some(h)).await?;
    }

    let policy_update = emby_policy_to_update(&body);
    upsert_user_policy(&state.db, &uid, &policy_update).await?;

    Ok(StatusCode::NO_CONTENT)
}

#[derive(Deserialize)]
struct PasswordBody {
    #[serde(alias = "CurrentPw")]
    current_pw: Option<String>,
    #[serde(alias = "NewPw")]
    new_pw: Option<String>,
}

async fn change_password(
    State(state): State<Arc<AppState>>,
    RequireAuth(auth_user, _): RequireAuth,
    Path(user_id): Path<String>,
    raw_body: axum::body::Bytes,
) -> AppResult<impl IntoResponse> {
    let body: PasswordBody = serde_json::from_slice(&raw_body)
        .or_else(|_| serde_urlencoded::from_bytes(&raw_body))
        .unwrap_or(PasswordBody { current_pw: None, new_pw: None });
    let uid: Uuid = user_id.parse().map_err(|_| AppError::NotFound)?;
    if !auth_user.is_admin && auth_user.id != user_id {
        return Err(AppError::Forbidden("Forbidden".into()));
    }

    let user = find_user_by_id(&state.db, &uid)
        .await?
        .ok_or(AppError::NotFound)?;

    if !auth_user.is_admin {
        if let Some(ref curr) = body.current_pw {
            if !verify_password(&state.db, &user, curr).await {
                return Err(AppError::Forbidden("Current password is incorrect".into()));
            }
        }
    }

    update_password(&state.db, &uid, body.new_pw.as_deref().unwrap_or("")).await?;
    Ok(StatusCode::NO_CONTENT)
}

async fn user_configuration(
    RequireAuth(_, _): RequireAuth,
) -> impl IntoResponse {
    StatusCode::NO_CONTENT
}

async fn logout(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, auth_info): RequireAuth,
) -> AppResult<impl IntoResponse> {
    if let Some(ref token) = auth_info.token {
        delete_token(&state.db, token).await?;
    }
    Ok(StatusCode::NO_CONTENT)
}

// --- Startup wizard ---

async fn startup_config(State(state): State<Arc<AppState>>) -> AppResult<impl IntoResponse> {
    let count = get_user_count(&state.db).await?;
    Ok(Json(json!({
        "IsComplete": count > 0,
        "UICulture": "zh-CN",
        "MetadataCountryCode": "CN",
        "PreferredMetadataLanguage": "zh",
    })))
}

async fn startup_user(State(state): State<Arc<AppState>>) -> AppResult<impl IntoResponse> {
    // Block if users already exist (setup is complete)
    let count = get_user_count(&state.db).await?;
    if count > 0 {
        return Err(AppError::Forbidden("Setup already complete".into()));
    }
    Ok(Json(json!({ "Name": "", "ServerId": state.config.server_id })))
}

#[derive(Deserialize)]
struct StartupUserBody {
    #[serde(alias = "Name")]
    name: Option<String>,
    #[serde(alias = "Password")]
    password: Option<String>,
}

async fn create_startup_user(
    State(state): State<Arc<AppState>>,
    Json(body): Json<StartupUserBody>,
) -> AppResult<impl IntoResponse> {
    // Block if users already exist — prevent hijacking after initial setup
    let count = get_user_count(&state.db).await?;
    if count > 0 {
        return Err(AppError::Forbidden("Setup already complete. Use admin panel to create users.".into()));
    }
    let name = body.name.as_deref().ok_or(AppError::BadRequest("Name is required".into()))?;
    create_user(&state.db, name, body.password.as_deref().unwrap_or(""), true).await?;
    Ok(StatusCode::NO_CONTENT)
}

async fn startup_complete() -> impl IntoResponse {
    StatusCode::NO_CONTENT
}

// --- Helpers ---

fn emby_policy_to_update(p: &serde_json::Value) -> PolicyUpdate {
    let mut pu = PolicyUpdate::default();
    macro_rules! map_bool {
        ($emby:literal, $field:ident) => {
            if let Some(v) = p.get($emby).and_then(|v| v.as_bool()) {
                pu.$field = Some(v);
            }
        };
    }
    macro_rules! map_i32 {
        ($emby:literal, $field:ident) => {
            if let Some(v) = p.get($emby).and_then(|v| v.as_i64()) {
                pu.$field = Some(v as i32);
            }
        };
    }

    map_bool!("IsAdministrator", is_administrator);
    map_bool!("EnableAllFolders", enable_all_folders);
    map_bool!("EnableRemoteAccess", enable_remote_access);
    map_bool!("EnableMediaPlayback", enable_media_playback);
    map_bool!("EnableAudioPlaybackTranscoding", enable_audio_transcoding);
    map_bool!("EnableVideoPlaybackTranscoding", enable_video_transcoding);
    map_bool!("EnablePlaybackRemuxing", enable_playback_remuxing);
    map_bool!("EnableContentDeletion", enable_content_deletion);
    map_bool!("EnableContentDownloading", enable_content_downloading);
    map_bool!("EnableSubtitleManagement", enable_subtitle_management);
    map_bool!("EnableLiveTvAccess", enable_live_tv_access);
    map_bool!("EnableLiveTvManagement", enable_live_tv_management);
    map_bool!("EnableUserPreferenceAccess", enable_user_preference_access);
    map_bool!("EnableRemoteControlOfOtherUsers", enable_remote_control);
    map_bool!("EnableSharedDeviceControl", enable_shared_device_control);
    map_i32!("RemoteClientBitrateLimit", remote_client_bitrate_limit);
    map_i32!("SimultaneousStreamLimit", simultaneous_stream_limit);

    pu
}

fn has_any_field(p: &PolicyUpdate) -> bool {
    p.is_administrator.is_some()
        || p.enable_all_folders.is_some()
        || p.enable_remote_access.is_some()
        || p.enable_media_playback.is_some()
        || p.enable_audio_transcoding.is_some()
        || p.enable_video_transcoding.is_some()
        || p.enable_playback_remuxing.is_some()
        || p.enable_content_deletion.is_some()
        || p.enable_content_downloading.is_some()
        || p.enable_subtitle_management.is_some()
        || p.enable_live_tv_access.is_some()
        || p.enable_live_tv_management.is_some()
        || p.enable_user_preference_access.is_some()
        || p.enable_remote_control.is_some()
        || p.enable_shared_device_control.is_some()
        || p.remote_client_bitrate_limit.is_some()
        || p.simultaneous_stream_limit.is_some()
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        // Fixed-path routes MUST come before /:userId
        .route("/Users/Public", get(get_public_users))
        .route("/Users/Me", get(get_me))
        .route("/Users/New", post(create_new_user))
        .route("/Users/AuthenticateByName", post(authenticate_by_name))
        .route("/Users/authenticatebyname", post(authenticate_by_name))
        .route("/Users/Query", get(get_all)) // simplified — full query in compat
        .route("/Users", get(get_all))
        // Parameterized routes
        .route("/Users/{userId}", get(get_user_by_id).post(update_user_handler).delete(delete_user_handler))
        .route("/Users/{userId}/Policy", post(update_policy))
        .route("/Users/{userId}/Password", post(change_password))
        .route("/Users/{userId}/Configuration", post(user_configuration))
        // Session
        .route("/Sessions/Logout", post(logout))
        // Startup wizard
        .route("/Startup/Configuration", get(startup_config))
        .route("/Startup/User", get(startup_user).post(create_startup_user))
        .route("/Startup/Complete", post(startup_complete))
}
