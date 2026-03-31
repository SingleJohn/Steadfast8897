use axum::body::Body;
use axum::extract::{Path, Query, State};
use axum::http::{header, HeaderMap, StatusCode};
use axum::response::{IntoResponse, Redirect, Response};
use axum::routing::get;
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use std::collections::HashMap;
use std::path::Path as FilePath;
use std::sync::Arc;
use tokio::fs::File;
use tokio_util::io::ReaderStream;
use uuid::Uuid;

use crate::auth::RequireAuth;
use crate::dto::format::{format_media_stream_dto, StreamRow};
use crate::error::{AppError, AppResult};
use crate::models::item::{get_item_by_id, get_item_by_any_id, get_media_streams, row_to_item_fields};
use crate::state::AppState;

fn resolve_strm_path(file_path: &str) -> Option<String> {
    if !file_path.ends_with(".strm") {
        return None;
    }
    let content = std::fs::read_to_string(file_path).ok()?;
    let line = content.lines().next()?.trim();
    if line.is_empty() || line.starts_with('#') {
        return None;
    }
    let mut resolved = line.to_string();
    if !resolved.starts_with("http") && resolved.starts_with('/') {
        if !std::path::Path::new(&resolved).exists() {
            let mnt_path = format!("/mnt{resolved}");
            if std::path::Path::new(&mnt_path).exists() {
                resolved = mnt_path;
            } else {
                let fixed = resolved.replacen("/CloudNAS", "/mnt/CloudNAS", 1);
                if fixed != resolved && std::path::Path::new(&fixed).exists() {
                    resolved = fixed;
                }
            }
        }
    }
    Some(resolved)
}

struct ResolvedPath {
    file_path: String,
    container: String,
    is_remote: bool,
}

fn resolve_playable_path(file_path: &str, container: Option<&str>) -> Option<ResolvedPath> {
    let ext = FilePath::new(file_path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_lowercase();

    if ext == "strm" {
        let real = resolve_strm_path(file_path)?;
        let real_ext = FilePath::new(&real)
            .extension()
            .and_then(|e| e.to_str())
            .unwrap_or("mkv")
            .to_lowercase();
        let is_remote = real.starts_with("http");
        return Some(ResolvedPath {
            file_path: real,
            container: real_ext,
            is_remote,
        });
    }

    Some(ResolvedPath {
        file_path: file_path.to_string(),
        container: container.unwrap_or(&ext).to_string(),
        is_remote: false,
    })
}

fn build_media_source(
    item_id: &str,
    item_name: &str,
    file_path: &str,
    container: Option<&str>,
    runtime_ticks: i64,
    streams: &[serde_json::Value],
) -> Option<serde_json::Value> {
    let resolved = resolve_playable_path(file_path, container)?;

    let file_size = if !resolved.is_remote {
        std::fs::metadata(&resolved.file_path)
            .map(|m| m.len() as i64)
            .unwrap_or(0)
    } else {
        0
    };

    Some(json!({
        "Id": item_id,
        "Path": resolved.file_path,
        "Protocol": if resolved.is_remote { "Http" } else { "File" },
        "Type": "Default",
        "Container": resolved.container,
        "Size": file_size,
        "Name": item_name,
        "IsRemote": resolved.is_remote,
        "ETag": item_id,
        "RunTimeTicks": runtime_ticks,
        "SupportsDirectPlay": true,
        "SupportsDirectStream": true,
        "SupportsTranscoding": false,
        "RequiresOpening": false,
        "RequiresClosing": false,
        "RequiresLooping": false,
        "ReadAtNativeFramerate": false,
        "MediaStreams": streams,
        "Formats": [],
        "Bitrate": 0,
        "DirectStreamUrl": format!("/Videos/{item_id}/stream.{}?MediaSourceId={item_id}&Static=true", resolved.container),
    }))
}

pub async fn build_all_media_sources(
    pool: &sqlx::PgPool,
    item_id: &str,
    item_name: &str,
    file_path: &str,
    container: Option<&str>,
    runtime_ticks: i64,
    db_streams: &[serde_json::Value],
) -> Vec<serde_json::Value> {
    // Check media_versions table
    let versions: Vec<sqlx::postgres::PgRow> = sqlx::query(
        "SELECT * FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, name",
    )
    .bind(item_id)
    .fetch_all(pool)
    .await
    .unwrap_or_default();

    if versions.is_empty() {
        return build_media_source(item_id, item_name, file_path, container, runtime_ticks, db_streams)
            .into_iter()
            .collect();
    }

    versions
        .iter()
        .enumerate()
        .filter_map(|(idx, v)| {
            let v_path: String = v.try_get("file_path").unwrap_or_default();
            let v_container: Option<String> = v.try_get("container").ok();
            let v_id: String = v.try_get::<Uuid, _>("id").map(|u| u.to_string()).unwrap_or_default();
            let v_name: String = v.try_get("name").unwrap_or_else(|_| item_name.to_string());
            let v_runtime: i64 = v.try_get("runtime_ticks").unwrap_or(runtime_ticks);
            let mediainfo: Option<serde_json::Value> = v.try_get("mediainfo").ok();

            let resolved = resolve_playable_path(&v_path, v_container.as_deref());
            let (actual_path, actual_container, is_remote) = if let Some(r) = resolved {
                (r.file_path, r.container, r.is_remote)
            } else if v_path.ends_with(".strm") {
                // Direct read strm
                if let Ok(content) = std::fs::read_to_string(&v_path) {
                    let line = content.lines().next().unwrap_or("").trim().to_string();
                    let is_remote = line.starts_with("http");
                    let ext = FilePath::new(&line).extension().and_then(|e| e.to_str()).unwrap_or("mkv").to_lowercase();
                    (line, ext, is_remote)
                } else {
                    return None;
                }
            } else {
                return None;
            };

            // MediaStreams from mediainfo JSON or DB
            let media_streams: Vec<serde_json::Value> = if let Some(ref mi) = mediainfo {
                if let Some(ms) = mi.get("MediaStreams").and_then(|v| v.as_array()) {
                    ms.clone()
                } else if idx == 0 {
                    db_streams.to_vec()
                } else {
                    vec![]
                }
            } else if idx == 0 {
                db_streams.to_vec()
            } else {
                vec![]
            };

            let file_size = if !is_remote {
                std::fs::metadata(&actual_path).map(|m| m.len() as i64).unwrap_or(0)
            } else { 0 };

            Some(json!({
                "Id": v_id,
                "Path": actual_path,
                "Protocol": if is_remote { "Http" } else { "File" },
                "Type": "Default",
                "Container": actual_container,
                "Size": file_size,
                "Name": v_name,
                "IsRemote": is_remote,
                "RunTimeTicks": v_runtime,
                "SupportsDirectPlay": true,
                "SupportsDirectStream": true,
                "SupportsTranscoding": false,
                "ReadAtNativeFramerate": false,
                "MediaStreams": media_streams,
                "DirectStreamUrl": format!("/Videos/{item_id}/stream.{actual_container}?MediaSourceId={v_id}&Static=true"),
            }))
        })
        .collect()
}

// --- Handlers ---

async fn playback_info(
    State(state): State<Arc<AppState>>,
    RequireAuth(user, _): RequireAuth,
    Path(item_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    // Check simultaneous stream limit
    if let Ok(uid) = user.id.parse::<uuid::Uuid>() {
        if let Ok(Some(policy)) = crate::models::user::get_user_policy(&state.db, &uid).await {
            if policy.simultaneous_stream_limit > 0 {
                let active_count = crate::routes::playback::active_playback_count(&user.id);
                if active_count as i32 >= policy.simultaneous_stream_limit {
                    tracing::warn!("[PlaybackInfo] User '{}' exceeded stream limit ({}/{})",
                        user.name, active_count, policy.simultaneous_stream_limit);
                    return Err(AppError::Forbidden(
                        format!("Stream limit reached ({}/{})", active_count, policy.simultaneous_stream_limit)
                    ));
                }
            }
        }
    }

    let row = get_item_by_any_id(&state.db, &item_id)
        .await?
        .ok_or(AppError::NotFound)?;

    let item = row_to_item_fields(&row);
    let real_id = &item.id; // always UUID
    let stream_rows = get_media_streams(&state.db, real_id).await?;
    let streams: Vec<serde_json::Value> = stream_rows
        .iter()
        .map(|r| {
            let s = StreamRow {
                codec: r.try_get("codec").ok(),
                stream_type: r.try_get("type").unwrap_or_default(),
                stream_index: r.try_get("stream_index").unwrap_or(0),
                language: r.try_get("language").ok(),
                title: r.try_get("title").ok(),
                is_default: r.try_get("is_default").ok(),
                is_forced: r.try_get("is_forced").ok(),
                width: r.try_get("width").ok(),
                height: r.try_get("height").ok(),
                bit_rate: r.try_get::<i32, _>("bit_rate").ok().map(|v| v as i64),
                channels: r.try_get("channels").ok(),
                sample_rate: r.try_get("sample_rate").ok(),
                bit_depth: r.try_get("bit_depth").ok(),
                pixel_format: r.try_get("pixel_format").ok(),
                display_title: r.try_get("display_title").ok(),
            };
            serde_json::to_value(&format_media_stream_dto(&s)).unwrap_or_default()
        })
        .collect();

    let play_session_id = Uuid::new_v4().to_string().replace('-', "");
    let sources = build_all_media_sources(
        &state.db,
        real_id,
        &item.name,
        item.file_path.as_deref().unwrap_or(""),
        item.container.as_deref(),
        item.runtime_ticks.unwrap_or(0),
        &streams,
    )
    .await;

    Ok(Json(json!({
        "MediaSources": sources,
        "PlaySessionId": play_session_id,
    })))
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct StreamQuery {
    #[serde(alias = "MediaSourceId", alias = "mediasourceid")]
    media_source_id: Option<String>,
    #[serde(alias = "api_key")]
    api_key: Option<String>,
}

async fn stream_video(
    State(state): State<Arc<AppState>>,
    Path(params): Path<HashMap<String, String>>,
    Query(q): Query<StreamQuery>,
    headers: HeaderMap,
) -> AppResult<Response> {
    let item_id = params.get("itemId").cloned().unwrap_or_default();

    // Check simultaneous stream limit via token/api_key
    let user_id = if let Some(ref api_key) = q.api_key {
        // Look up user from access_token
        if let Ok(Some(row)) = sqlx::query_scalar::<_, String>(
            "SELECT user_id::text FROM access_tokens WHERE token = $1"
        ).bind(api_key).fetch_optional(&state.db).await {
            Some(row)
        } else {
            None
        }
    } else if let Some(token) = headers.get("X-Emby-Token").and_then(|v| v.to_str().ok()) {
        if let Ok(Some(row)) = sqlx::query_scalar::<_, String>(
            "SELECT user_id::text FROM access_tokens WHERE token = $1"
        ).bind(token).fetch_optional(&state.db).await {
            Some(row)
        } else {
            None
        }
    } else {
        None
    };

    if let Some(ref uid) = user_id {
        if let Ok(parsed) = uid.parse::<uuid::Uuid>() {
            if let Ok(Some(policy)) = crate::models::user::get_user_policy(&state.db, &parsed).await {
                if policy.simultaneous_stream_limit > 0 {
                    let active_count = crate::routes::playback::active_playback_count(uid);
                    if active_count as i32 > policy.simultaneous_stream_limit {
                        tracing::warn!("[Stream] User exceeded stream limit ({}/{})", active_count, policy.simultaneous_stream_limit);
                        return Err(AppError::Forbidden(
                            format!("Stream limit reached ({}/{})", active_count, policy.simultaneous_stream_limit)
                        ));
                    }
                }
            }
        }
    }
    // Look up file path from media_version or item
    let mut file_path: Option<String> = None;
    let mut ver_container: Option<String> = None;

    if let Some(ref msid) = q.media_source_id {
        if let Ok(Some(row)) = sqlx::query("SELECT * FROM media_versions WHERE id = $1::uuid")
            .bind(msid)
            .fetch_optional(&state.db)
            .await
        {
            file_path = row.try_get("file_path").ok();
            ver_container = row.try_get("container").ok();
        }
    }

    if file_path.is_none() {
        let row = get_item_by_any_id(&state.db, &item_id)
            .await?
            .ok_or(AppError::NotFound)?;
        file_path = row.try_get("file_path").ok();
        ver_container = row.try_get("container").ok();
    }

    let fp = file_path.ok_or(AppError::NotFound)?;
    let resolved = resolve_playable_path(&fp, ver_container.as_deref())
        .ok_or(AppError::NotFound)?;

    // Remote URL: redirect
    if resolved.is_remote {
        return Ok(Redirect::temporary(&resolved.file_path).into_response());
    }

    // Local file: serve with Range support
    let path = std::path::PathBuf::from(&resolved.file_path);
    if !path.exists() {
        return Err(AppError::NotFound);
    }

    let metadata = tokio::fs::metadata(&path).await
        .map_err(|_| AppError::NotFound)?;
    let file_size = metadata.len();

    let content_type = match resolved.container.as_str() {
        "mp4" | "m4v" => "video/mp4",
        "mkv" => "video/x-matroska",
        "avi" => "video/x-msvideo",
        "webm" => "video/webm",
        "ts" | "m2ts" => "video/mp2t",
        "mov" => "video/quicktime",
        _ => "video/mp4",
    };

    // Range request
    if let Some(range_header) = headers.get(header::RANGE) {
        let range_str = range_header.to_str().unwrap_or("");
        if let Some(range) = parse_range(range_str, file_size) {
            let (start, end) = range;
            let chunk_size = end - start + 1;

            use tokio::io::{AsyncReadExt, AsyncSeekExt};
            let mut file = File::open(&path).await.map_err(|_| AppError::NotFound)?;
            file.seek(std::io::SeekFrom::Start(start)).await.map_err(|_| AppError::NotFound)?;
            let stream = ReaderStream::new(file.take(chunk_size));

            let body = Body::from_stream(stream);
            Ok(Response::builder()
                .status(StatusCode::PARTIAL_CONTENT)
                .header(header::CONTENT_TYPE, content_type)
                .header(header::CONTENT_LENGTH, chunk_size)
                .header(header::ACCEPT_RANGES, "bytes")
                .header(
                    header::CONTENT_RANGE,
                    format!("bytes {start}-{end}/{file_size}"),
                )
                .body(body)
                .unwrap())
        } else {
            Err(AppError::BadRequest("Invalid range".into()))
        }
    } else {
        let file = File::open(&path).await.map_err(|_| AppError::NotFound)?;
        let stream = ReaderStream::new(file);
        let body = Body::from_stream(stream);
        Ok(Response::builder()
            .status(StatusCode::OK)
            .header(header::CONTENT_TYPE, content_type)
            .header(header::CONTENT_LENGTH, file_size)
            .header(header::ACCEPT_RANGES, "bytes")
            .body(body)
            .unwrap())
    }
}

fn parse_range(range: &str, file_size: u64) -> Option<(u64, u64)> {
    let range = range.strip_prefix("bytes=")?;
    let parts: Vec<&str> = range.split('-').collect();
    if parts.len() != 2 {
        return None;
    }
    let start: u64 = parts[0].parse().ok()?;
    let end: u64 = if parts[1].is_empty() {
        file_size - 1
    } else {
        parts[1].parse().ok()?
    };
    if start <= end && end < file_size {
        Some((start, end))
    } else {
        None
    }
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        .route("/Items/{itemId}/PlaybackInfo", get(playback_info).post(playback_info))
        .route("/Videos/{itemId}/stream", get(stream_video))
        .route("/Videos/{itemId}/stream.{container}", get(stream_video))
}
