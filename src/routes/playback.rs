use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::IntoResponse;
use axum::routing::post;
use axum::{Json, Router};
use chrono::Utc;
use dashmap::DashMap;
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use std::sync::Arc;

use crate::auth::RequireAuth;
use crate::error::{AppError, AppResult};
use crate::models::item::{get_item_by_id, get_user_item_data, upsert_user_item_data};
use crate::services::progress_buffer::ProgressEntry;
use crate::state::AppState;

// Active playback tracking for duration calculation
struct ActivePlayback {
    item_id: String,
    item_name: String,
    item_type: String,
    series_name: String,
    client_name: String,
    device_name: String,
    client_ip: String,
    start_time: i64,
    last_progress_time: i64,
}

// We store this in AppState-like fashion using a lazy static
static ACTIVE_PLAYBACKS: std::sync::LazyLock<DashMap<String, ActivePlayback>> =
    std::sync::LazyLock::new(DashMap::new);

fn playback_key(user_id: &str, device_id: &str) -> String {
    format!("{user_id}:{device_id}")
}

/// Count active playbacks for a given user (used by videos.rs for stream limit check)
pub fn active_playback_count(user_id: &str) -> usize {
    ACTIVE_PLAYBACKS.iter()
        .filter(|e| e.key().starts_with(&format!("{user_id}:")))
        .count()
}

fn get_client_ip(headers: &axum::http::HeaderMap) -> String {
    headers
        .get("x-forwarded-for")
        .and_then(|v| v.to_str().ok())
        .and_then(|s| s.split(',').next())
        .map(|s| s.trim().to_string())
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|v| v.to_str().ok())
                .map(|s| s.to_string())
        })
        .unwrap_or_else(|| "unknown".to_string())
}

fn resolve_client_name(auth_client: Option<&str>, user_agent: Option<&str>) -> String {
    let client = auth_client.unwrap_or("");
    if !client.is_empty() && client != "Unknown" && client != "Unknown Client" {
        return client.to_string();
    }
    let ua = user_agent.unwrap_or("");
    if ua.contains("VidHub") { "VidHub".into() }
    else if ua.contains("Infuse") { "Infuse".into() }
    else if ua.contains("Emby") { "Emby".into() }
    else if ua.contains("SenPlayer") { "SenPlayer".into() }
    else if ua.contains("nPlayer") { "nPlayer".into() }
    else if ua.contains("Mozilla") { "Web Browser".into() }
    else { "Unknown".into() }
}

fn resolve_device_name(auth_device: Option<&str>, user_agent: Option<&str>) -> String {
    let dev = auth_device.unwrap_or("");
    if !dev.is_empty() && dev != "Unknown" && dev != "Unknown Device" {
        return dev.to_string();
    }
    let ua = user_agent.unwrap_or("");
    if ua.contains("iPhone") { "iPhone".into() }
    else if ua.contains("iPad") { "iPad".into() }
    else if ua.contains("Android") { "Android".into() }
    else if ua.contains("Mac") { "Mac".into() }
    else if ua.contains("Windows") { "Windows".into() }
    else if ua.contains("Apple TV") { "Apple TV".into() }
    else { "Unknown".into() }
}

fn insert_activity(pool: &sqlx::PgPool, user_id: &str, session: &ActivePlayback, duration_sec: i64) {
    let pool = pool.clone();
    let uid = user_id.to_string();
    let item_id = session.item_id.clone();
    let item_type = session.item_type.clone();
    let item_name = session.item_name.clone();
    let client_name = session.client_name.clone();
    let device_name = session.device_name.clone();
    let client_ip = session.client_ip.clone();
    let series_name = if session.series_name.is_empty() { None } else { Some(session.series_name.clone()) };

    tokio::spawn(async move {
        let _ = sqlx::query(
            "INSERT INTO playback_activity (user_id, item_id, item_type, item_name, play_method, client_name, device_name, play_duration, client_ip, series_name)
             VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10)"
        )
        .bind(&uid).bind(&item_id).bind(&item_type).bind(&item_name)
        .bind("DirectPlay").bind(&client_name).bind(&device_name)
        .bind(duration_sec as i32).bind(&client_ip).bind(&series_name)
        .execute(&pool).await;
    });
}

// Spawn stale session flusher
pub fn spawn_stale_flusher(pool: sqlx::PgPool) {
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(30));
        loop {
            interval.tick().await;
            let now = Utc::now().timestamp_millis();
            let mut to_remove = Vec::new();
            for entry in ACTIVE_PLAYBACKS.iter() {
                if now - entry.value().last_progress_time > 120_000 {
                    to_remove.push(entry.key().clone());
                }
            }
            for key in to_remove {
                if let Some((_, session)) = ACTIVE_PLAYBACKS.remove(&key) {
                    let duration_sec = (session.last_progress_time - session.start_time) / 1000;
                    if duration_sec > 5 {
                        let user_id = key.split(':').next().unwrap_or("");
                        insert_activity(&pool, user_id, &session, duration_sec);
                    }
                }
            }
        }
    });
}

// --- Handlers ---

#[derive(Deserialize)]
struct PlayingBody {
    #[serde(alias = "ItemId")]
    item_id: Option<String>,
    #[serde(alias = "PositionTicks")]
    position_ticks: Option<i64>,
    #[serde(alias = "IsPaused")]
    is_paused: Option<bool>,
    #[serde(alias = "MediaSourceId")]
    media_source_id: Option<String>,
    #[serde(alias = "PlaySessionId")]
    play_session_id: Option<String>,
}

async fn playing_start(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, auth_info): RequireAuth,
    headers: HeaderMap,
    Json(body): Json<PlayingBody>,
) -> AppResult<impl IntoResponse> {
    let ua = headers.get("user-agent").and_then(|v| v.to_str().ok()).map(|s| s.to_string());
    let client_ip = get_client_ip(&headers);

    let item_id = body.item_id.ok_or(AppError::BadRequest("ItemId is required".into()))?;
    let position = body.position_ticks.unwrap_or(0);

    // Check simultaneous stream limit
    if let Ok(uid) = user.id.parse::<uuid::Uuid>() {
        if let Ok(Some(policy)) = crate::models::user::get_user_policy(&state.db, &uid).await {
            if policy.simultaneous_stream_limit > 0 {
                let active_count = ACTIVE_PLAYBACKS.iter()
                    .filter(|e| e.key().starts_with(&format!("{}:", user.id)))
                    .count();
                if active_count as i32 >= policy.simultaneous_stream_limit {
                    tracing::warn!("[Play] User '{}' exceeded stream limit ({}/{})",
                        user.name, active_count, policy.simultaneous_stream_limit);
                    return Err(AppError::Forbidden(
                        format!("Stream limit reached ({}/{})", active_count, policy.simultaneous_stream_limit)
                    ));
                }
            }
        }
    }

    // Buffer progress
    state.progress_buffer.buffer_progress(ProgressEntry {
        user_id: user.id.clone(),
        item_id: item_id.clone(),
        position_ticks: position,
        play_count: None,
        is_favorite: None,
        played: None,
    });

    // Update session
    let device_id = auth_info.device_id.as_deref().unwrap_or("unknown");
    state.session_manager.update_session(
        &user.id, &user.name, device_id,
        auth_info.device.as_deref().unwrap_or(""),
        auth_info.client.as_deref().unwrap_or(""),
        auth_info.version.as_deref().unwrap_or(""),
        &client_ip,
    );

    // Get item info for tracking
    let item_row = get_item_by_id(&state.db, &state.cache, &item_id).await?;
    let (item_name, item_type, series_name, season_index, episode_index, series_id) = if let Some(ref row) = item_row {
        (
            row.try_get::<String, _>("name").unwrap_or_default(),
            row.try_get::<String, _>("type").unwrap_or_default(),
            row.try_get::<String, _>("series_name").unwrap_or_default(),
            row.try_get::<i32, _>("season_index").ok(),
            row.try_get::<i32, _>("episode_index").ok(),
            row.try_get::<String, _>("series_id").ok(),
        )
    } else {
        ("Unknown".into(), "Unknown".into(), String::new(), None, None, None)
    };

    // Determine image item ID (for Episode/Season, use Series ID for cover)
    let primary_image_item_id = if item_type == "Episode" || item_type == "Season" {
        series_id.or(Some(item_id.clone()))
    } else {
        Some(item_id.clone())
    };

    let runtime_ticks = item_row.as_ref().and_then(|r| r.try_get::<i64, _>("runtime_ticks").ok());

    let client_name = resolve_client_name(auth_info.client.as_deref(), ua.as_deref());
    let device_name = resolve_device_name(auth_info.device.as_deref(), ua.as_deref());

    tracing::info!("[Play] User '{}' started playing '{}' ({})", user.name, item_name, client_name);

    // Set NowPlaying on session
    state.session_manager.set_now_playing(
        &user.id, device_id, &item_id, &item_name, &item_type,
        if series_name.is_empty() { None } else { Some(series_name.as_str()) },
        runtime_ticks, position, body.is_paused.unwrap_or(false),
        season_index, episode_index,
        primary_image_item_id.as_deref(),
    );

    let now = Utc::now().timestamp_millis();
    ACTIVE_PLAYBACKS.insert(
        playback_key(&user.id, device_id),
        ActivePlayback {
            item_id, item_name, item_type, series_name,
            client_name, device_name, client_ip,
            start_time: now, last_progress_time: now,
        },
    );

    Ok(StatusCode::NO_CONTENT)
}

async fn playing_progress(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, auth_info): RequireAuth,
    headers: HeaderMap,
    Json(body): Json<PlayingBody>,
) -> AppResult<impl IntoResponse> {
    let ua = headers.get("user-agent").and_then(|v| v.to_str().ok()).map(|s| s.to_string());

    let item_id = body.item_id.ok_or(AppError::BadRequest("ItemId is required".into()))?;
    let position = body.position_ticks.unwrap_or(0);

    state.progress_buffer.buffer_progress(ProgressEntry {
        user_id: user.id.clone(),
        item_id: item_id.clone(),
        position_ticks: position,
        play_count: None, is_favorite: None, played: None,
    });

    let device_id = auth_info.device_id.as_deref().unwrap_or("unknown");
    let client_ip = get_client_ip(&headers);
    state.session_manager.update_session(
        &user.id, &user.name, device_id,
        auth_info.device.as_deref().unwrap_or(""),
        auth_info.client.as_deref().unwrap_or(""),
        auth_info.version.as_deref().unwrap_or(""),
        &client_ip,
    );

    // Update or create active playback
    let key = playback_key(&user.id, device_id);
    let now = Utc::now().timestamp_millis();

    let need_new = {
        let existing = ACTIVE_PLAYBACKS.get(&key);
        match existing {
            Some(ref e) if e.item_id == item_id => {
                drop(existing);
                if let Some(mut e) = ACTIVE_PLAYBACKS.get_mut(&key) {
                    e.last_progress_time = now;
                }
                false
            }
            Some(ref e) => {
                // Flush old session
                let dur = (now - e.start_time) / 1000;
                if dur > 5 {
                    insert_activity(&state.db, &user.id, &e, dur);
                }
                drop(existing);
                true
            }
            None => true,
        }
    };

    // Always update NowPlaying position on session (even if not need_new)
    {
        let item_row = get_item_by_id(&state.db, &state.cache, &item_id).await?;
        let (iname, itype, sname, s_idx, e_idx, sid) = if let Some(ref row) = item_row {
            (
                row.try_get::<String, _>("name").unwrap_or_default(),
                row.try_get::<String, _>("type").unwrap_or_default(),
                row.try_get::<String, _>("series_name").unwrap_or_default(),
                row.try_get::<i32, _>("season_index").ok(),
                row.try_get::<i32, _>("episode_index").ok(),
                row.try_get::<String, _>("series_id").ok(),
            )
        } else {
            ("Unknown".into(), "Unknown".into(), String::new(), None, None, None)
        };
        let img_id = if itype == "Episode" || itype == "Season" {
            sid.clone().or(Some(item_id.clone()))
        } else {
            Some(item_id.clone())
        };
        let runtime = item_row.as_ref().and_then(|r| r.try_get::<i64, _>("runtime_ticks").ok());

        state.session_manager.set_now_playing(
            &user.id, device_id, &item_id, &iname, &itype,
            if sname.is_empty() { None } else { Some(sname.as_str()) },
            runtime, position, body.is_paused.unwrap_or(false),
            s_idx, e_idx, img_id.as_deref(),
        );

        if need_new {
            ACTIVE_PLAYBACKS.insert(key, ActivePlayback {
                item_id, item_name: iname, item_type: itype, series_name: sname,
                client_name: resolve_client_name(auth_info.client.as_deref(), ua.as_deref()),
                device_name: resolve_device_name(auth_info.device.as_deref(), ua.as_deref()),
                client_ip,
                start_time: now, last_progress_time: now,
            });
        }
    }

    Ok(StatusCode::NO_CONTENT)
}

async fn playing_stopped(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, auth_info): RequireAuth,
    Json(body): Json<PlayingBody>,
) -> AppResult<impl IntoResponse> {
    let item_id = body.item_id.ok_or(AppError::BadRequest("ItemId is required".into()))?;
    let position = body.position_ticks.unwrap_or(0);

    // Get existing play_count and increment
    let existing = get_user_item_data(&state.db, &user.id, &item_id).await?;
    let play_count = existing
        .as_ref()
        .and_then(|r| r.try_get::<i32, _>("play_count").ok())
        .unwrap_or(0)
        + 1;

    upsert_user_item_data(
        &state.db, &user.id, &item_id,
        Some(position), Some(play_count), None, None,
    ).await?;

    // Record activity
    let device_id = auth_info.device_id.as_deref().unwrap_or("unknown");
    let key = playback_key(&user.id, device_id);
    if let Some((_, session)) = ACTIVE_PLAYBACKS.remove(&key) {
        let now = Utc::now().timestamp_millis();
        let dur = (now - session.start_time) / 1000;
        if dur > 5 {
            tracing::info!("[Play] User '{}' stopped '{}' after {}s", user.name, session.item_name, dur);
            insert_activity(&state.db, &user.id, &session, dur);
        }
    }

    state.session_manager.clear_now_playing(&user.id, device_id);
    Ok(StatusCode::NO_CONTENT)
}

// Mark played / unplayed
async fn mark_played(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
    Path((_user_id, item_id)): Path<(String, String)>,
) -> AppResult<impl IntoResponse> {
    upsert_user_item_data(&state.db, &user.id, &item_id, Some(0), None, None, Some(true)).await?;
    Ok(Json(json!({ "Played": true })))
}

async fn mark_unplayed(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
    Path((_user_id, item_id)): Path<(String, String)>,
) -> AppResult<impl IntoResponse> {
    upsert_user_item_data(&state.db, &user.id, &item_id, Some(0), None, None, Some(false)).await?;
    Ok(Json(json!({ "Played": false })))
}

async fn add_favorite(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
    Path((_user_id, item_id)): Path<(String, String)>,
) -> AppResult<impl IntoResponse> {
    upsert_user_item_data(&state.db, &user.id, &item_id, None, None, Some(true), None).await?;
    Ok(Json(json!({ "IsFavorite": true })))
}

async fn remove_favorite(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
    Path((_user_id, item_id)): Path<(String, String)>,
) -> AppResult<impl IntoResponse> {
    upsert_user_item_data(&state.db, &user.id, &item_id, None, None, Some(false), None).await?;
    Ok(Json(json!({ "IsFavorite": false })))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        .route("/Sessions/Playing", post(playing_start))
        .route("/Sessions/Playing/Progress", post(playing_progress))
        .route("/Sessions/Playing/Stopped", post(playing_stopped))
        .route("/Users/{userId}/PlayedItems/{itemId}", post(mark_played).delete(mark_unplayed))
        .route("/Users/{userId}/FavoriteItems/{itemId}", post(add_favorite).delete(remove_favorite))
}
