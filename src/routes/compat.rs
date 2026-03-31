use axum::extract::{Path, Query, State};
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use std::sync::Arc;
use uuid::Uuid;

use crate::auth::{RequireAdmin, RequireAuth};
use crate::dto::format::{format_item_dto, UserDataRow};
use crate::error::{AppError, AppResult};
use crate::models::item::{get_child_count, get_item_by_id, row_to_item_fields, row_to_user_data};
use crate::state::AppState;

// --- Sessions ---

async fn get_sessions(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> impl IntoResponse {
    let sessions = state.session_manager.get_active_sessions();
    let mut result: Vec<serde_json::Value> = Vec::new();

    for s in &sessions {
        let mut val = json!({
            "Id": format!("{}_{}", s.user_id, s.device_id),
            "UserId": s.user_id,
            "UserName": s.user_name,
            "DeviceId": s.device_id,
            "DeviceName": s.device_name,
            "Client": s.app_name,
            "ApplicationVersion": s.app_version,
            "RemoteEndPoint": s.client_ip,
            "LastActivityDate": s.last_activity.to_rfc3339(),
        });
        if let Some(ref np) = s.now_playing {
            val["NowPlayingItem"] = json!({
                "Id": np.item_id,
                "Name": np.item_name,
                "Type": np.item_type,
                "SeriesName": np.series_name,
                "RunTimeTicks": np.runtime_ticks,
                "IndexNumber": np.episode_index,
                "ParentIndexNumber": np.season_index,
                "PrimaryImageItemId": np.primary_image_item_id,
            });
            val["PlayState"] = json!({
                "PositionTicks": np.position_ticks,
                "IsPaused": np.is_paused,
                "CanSeek": true,
                "PlayMethod": "DirectPlay",
            });

            // Fetch media stream info for the playing item
            if let Ok(streams) = sqlx::query(
                "SELECT type, codec, width, height, bit_rate, channels, display_title, language, is_default
                 FROM media_streams WHERE item_id = $1::uuid ORDER BY stream_index"
            ).bind(&np.item_id).fetch_all(&state.db).await {
                let media_streams: Vec<serde_json::Value> = streams.iter().map(|r| {
                    let stype: String = r.try_get("type").unwrap_or_default();
                    let codec: String = r.try_get("codec").unwrap_or_default();
                    let width: Option<i32> = r.try_get("width").ok();
                    let height: Option<i32> = r.try_get("height").ok();
                    let bit_rate: Option<i32> = r.try_get("bit_rate").ok();
                    let channels: Option<i32> = r.try_get("channels").ok();
                    let display_title: String = r.try_get("display_title").unwrap_or_default();
                    let is_default: bool = r.try_get("is_default").unwrap_or(false);
                    json!({
                        "Type": stype,
                        "Codec": codec,
                        "Width": width,
                        "Height": height,
                        "BitRate": bit_rate,
                        "Channels": channels,
                        "DisplayTitle": display_title,
                        "IsDefault": is_default,
                    })
                }).collect();
                val["NowPlayingItem"]["MediaStreams"] = json!(media_streams);
            }

            // Fetch media version info (container, bitrate)
            if let Ok(Some(ver)) = sqlx::query(
                "SELECT container, bitrate, size FROM media_versions WHERE item_id = $1::uuid AND is_primary = true LIMIT 1"
            ).bind(&np.item_id).fetch_optional(&state.db).await {
                let container: String = ver.try_get("container").unwrap_or_default();
                let bitrate: Option<i32> = ver.try_get("bitrate").ok();
                val["NowPlayingItem"]["Container"] = json!(container);
                val["NowPlayingItem"]["Bitrate"] = json!(bitrate);
            }
        }
        result.push(val);
    }

    Json(json!(result))
}

// --- DisplayPreferences ---

async fn get_display_prefs(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({
        "Id": "usersettings",
        "SortBy": "SortName",
        "SortOrder": "Ascending",
        "RememberIndexing": false,
        "RememberSorting": false,
        "CustomPrefs": {},
    }))
}

async fn post_display_prefs(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    StatusCode::NO_CONTENT
}

// --- Stub endpoints ---

async fn capabilities(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    StatusCode::NO_CONTENT
}

async fn plugins(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!([]))
}

async fn live_tv(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Services": [], "IsEnabled": false, "EnabledUsers": [] }))
}

async fn channels(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Items": [], "TotalRecordCount": 0 }))
}

async fn notifications(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "UnreadCount": 0, "MaxUnreadCount": 0 }))
}

async fn next_up(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Items": [], "TotalRecordCount": 0 }))
}

async fn studios(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Items": [], "TotalRecordCount": 0 }))
}

async fn artists(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Items": [], "TotalRecordCount": 0 }))
}

async fn persons_stub(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    Json(json!({ "Items": [], "TotalRecordCount": 0 }))
}

// --- Shows: Seasons & Episodes ---

async fn show_seasons(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(series_id): Path<String>,
    Query(q): Query<std::collections::HashMap<String, String>>,
) -> AppResult<impl IntoResponse> {
    let user_id = q.get("UserId").or(q.get("userId")).cloned();

    let mut sql = "SELECT i.* FROM items i WHERE i.parent_id = $1::uuid AND i.type = 'Season' ORDER BY i.index_number ASC".to_string();
    let rows = sqlx::query(&sql)
        .bind(&series_id)
        .fetch_all(&state.db)
        .await?;

    // Fetch series info for image fallback
    let series_row = crate::models::item::get_item_by_id(&state.db, &state.cache, &series_id).await.ok().flatten();
    let series_image_tag: Option<String> = series_row.as_ref().and_then(|r| r.try_get("primary_image_tag").ok());
    let series_backdrop_tag: Option<String> = series_row.as_ref().and_then(|r| r.try_get("backdrop_image_tag").ok());
    let series_name_val: Option<String> = series_row.as_ref().and_then(|r| r.try_get::<String, _>("name").ok());

    let mut items = Vec::new();
    for row in &rows {
        let item = row_to_item_fields(row);
        let mut dto = format_item_dto(&item, &state.config.server_id, None);
        dto.series_id = Some(series_id.clone());
        dto.series_name = row.try_get::<String, _>("series_name").ok().or_else(|| series_name_val.clone());
        dto.child_count = Some(
            get_child_count(&state.db, &item.id).await?,
        );
        // Fallback: use series image via flat Emby fields (NOT ImageTags)
        if dto.image_tags.as_ref().map_or(true, |t| t.is_empty()) {
            if let Some(ref tag) = series_image_tag {
                dto.series_primary_image_tag = Some(tag.clone());
                dto.series_primary_image_item_id = Some(series_id.clone());
                dto.parent_primary_image_item_id = Some(series_id.clone());
                dto.parent_primary_image_tag = Some(tag.clone());
                dto.parent_thumb_item_id = Some(series_id.clone());
                dto.parent_thumb_image_tag = Some(tag.clone());
            }
        }
        if dto.backdrop_image_tags.as_ref().map_or(true, |t| t.is_empty()) {
            if let Some(ref tag) = series_backdrop_tag {
                dto.parent_backdrop_item_id = Some(series_id.clone());
                dto.parent_backdrop_image_tags = Some(vec![tag.clone()]);
            }
        }
        items.push(serde_json::to_value(&dto).unwrap_or_default());
    }

    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": items.len(),
    })))
}

async fn show_episodes(
    State(state): State<Arc<AppState>>,
    RequireAuth(auth_user, _): RequireAuth,
    Path(series_id): Path<String>,
    Query(q): Query<std::collections::HashMap<String, String>>,
) -> AppResult<impl IntoResponse> {
    let season_id = q.get("SeasonId").or(q.get("seasonId")).cloned();
    let user_id = q.get("UserId").or(q.get("userId")).cloned()
        .filter(|s| !s.is_empty() && uuid::Uuid::parse_str(s).is_ok())
        .unwrap_or_else(|| {
            // If auth_user.id is a valid UUID use it, otherwise use nil UUID
            if uuid::Uuid::parse_str(&auth_user.id).is_ok() {
                auth_user.id.clone()
            } else {
                uuid::Uuid::nil().to_string()
            }
        });
    let limit: Option<i64> = q.get("Limit").and_then(|s| s.parse().ok());
    let start_index: Option<i64> = q.get("StartIndex").and_then(|s| s.parse().ok());

    let (count_sql, item_sql, params) = if let Some(ref sid) = season_id {
        (
            "SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid AND type = 'Episode'",
            "SELECT i.*, uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date \
             FROM items i LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = $2::uuid \
             WHERE i.parent_id = $1::uuid AND i.type = 'Episode' ORDER BY i.index_number ASC",
            vec![sid.clone(), user_id.clone()],
        )
    } else {
        (
            "SELECT COUNT(*) FROM items WHERE series_id = $1::uuid AND type = 'Episode'",
            "SELECT i.*, uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date \
             FROM items i LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = $2::uuid \
             WHERE i.series_id = $1::uuid AND i.type = 'Episode' ORDER BY i.parent_index_number ASC, i.index_number ASC",
            vec![series_id.clone(), user_id.clone()],
        )
    };

    let total: (i64,) = sqlx::query_as(count_sql)
        .bind(&params[0])
        .fetch_one(&state.db)
        .await?;

    let mut full_sql = item_sql.to_string();
    let mut param_idx = params.len() + 1; // next param index after existing binds
    if limit.is_some() {
        full_sql.push_str(&format!(" LIMIT ${param_idx}"));
        param_idx += 1;
    }
    if start_index.is_some() {
        full_sql.push_str(&format!(" OFFSET ${param_idx}"));
    }

    let mut query = sqlx::query(&full_sql)
        .bind(&params[0])
        .bind(&params[1]);
    if let Some(lim) = limit {
        query = query.bind(lim);
    }
    if let Some(off) = start_index {
        query = query.bind(off);
    }
    let rows = query.fetch_all(&state.db).await?;

    // Fetch series info for image fallback
    let series_row = crate::models::item::get_item_by_id(&state.db, &state.cache, &series_id).await.ok().flatten();
    let series_image_tag: Option<String> = series_row.as_ref().and_then(|r| r.try_get("primary_image_tag").ok());
    let series_backdrop_tag: Option<String> = series_row.as_ref().and_then(|r| r.try_get("backdrop_image_tag").ok());
    let series_name_val: Option<String> = series_row.as_ref().and_then(|r| r.try_get::<String, _>("name").ok());

    let mut items: Vec<serde_json::Value> = Vec::new();
    for row in &rows {
        let item = row_to_item_fields(row);
        let ud = row_to_user_data(row);
        let mut dto = format_item_dto(&item, &state.config.server_id, ud.as_ref());
        dto.series_id = Some(series_id.clone());
        dto.series_name = row.try_get::<String, _>("series_name").ok().or_else(|| series_name_val.clone());

        // Fallback: use series image via flat Emby fields (NOT ImageTags)
        if dto.image_tags.as_ref().map_or(true, |t| t.is_empty()) {
            if let Some(ref tag) = series_image_tag {
                dto.series_primary_image_tag = Some(tag.clone());
                dto.series_primary_image_item_id = Some(series_id.clone());
                dto.parent_primary_image_item_id = Some(series_id.clone());
                dto.parent_primary_image_tag = Some(tag.clone());
                dto.parent_thumb_item_id = Some(series_id.clone());
                dto.parent_thumb_image_tag = Some(tag.clone());
            }
        }
        if dto.backdrop_image_tags.as_ref().map_or(true, |t| t.is_empty()) {
            if let Some(ref tag) = series_backdrop_tag {
                dto.parent_backdrop_item_id = Some(series_id.clone());
                dto.parent_backdrop_image_tags = Some(vec![tag.clone()]);
            }
        }

        let mut dto_val = serde_json::to_value(&dto).unwrap_or_default();

        // Add MediaSources for episodes
        if let Some(ref fp) = item.file_path {
            let stream_rows = crate::models::item::get_media_streams(&state.db, &item.id).await.unwrap_or_default();
            let stream_dtos: Vec<serde_json::Value> = stream_rows.iter().map(|r| {
                let s = crate::dto::format::StreamRow {
                    stream_type: r.try_get("type").unwrap_or_default(),
                    stream_index: r.try_get("stream_index").unwrap_or(0),
                    codec: r.try_get("codec").ok(),
                    language: r.try_get("language").ok(),
                    title: r.try_get("title").ok(),
                    is_default: r.try_get::<bool, _>("is_default").ok(),
                    is_forced: r.try_get::<bool, _>("is_forced").ok(),
                    channels: r.try_get("channels").ok(),
                    sample_rate: r.try_get("sample_rate").ok(),
                    bit_rate: r.try_get("bit_rate").ok(),
                    bit_depth: r.try_get("bit_depth").ok(),
                    width: r.try_get("width").ok(),
                    height: r.try_get("height").ok(),
                    pixel_format: r.try_get("pixel_format").ok(),
                    display_title: r.try_get("display_title").ok(),
                };
                serde_json::to_value(&crate::dto::format::format_media_stream_dto(&s)).unwrap_or_default()
            }).collect();
            let media_sources = crate::routes::videos::build_all_media_sources(
                &state.db, &item.id, &item.name, fp,
                item.container.as_deref(),
                item.runtime_ticks.unwrap_or(0),
                &stream_dtos,
            ).await;
            dto_val["MediaSources"] = serde_json::to_value(&media_sources).unwrap_or_default();
        }

        items.push(dto_val);
    }

    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": total.0,
    })))
}

// --- API Keys ---

async fn list_api_keys(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<impl IntoResponse> {
    let rows = sqlx::query(
        "SELECT ak.id, ak.name, ak.key, ak.created_at, ak.last_used_at, u.name as created_by_name \
         FROM api_keys ak LEFT JOIN users u ON ak.created_by = u.id ORDER BY ak.created_at DESC",
    )
    .fetch_all(&state.db)
    .await?;

    let items: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "Id": r.try_get::<i32, _>("id").unwrap_or(0),
                "Name": r.try_get::<String, _>("name").unwrap_or_default(),
                "Key": r.try_get::<String, _>("key").unwrap_or_default(),
                "CreatedAt": r.try_get::<chrono::NaiveDateTime, _>("created_at")
                    .map(|d| d.and_utc().to_rfc3339()).unwrap_or_default(),
                "LastUsedAt": r.try_get::<chrono::NaiveDateTime, _>("last_used_at")
                    .ok().map(|d| d.and_utc().to_rfc3339()),
                "CreatedBy": r.try_get::<String, _>("created_by_name").unwrap_or_else(|_| "Unknown".into()),
            })
        })
        .collect();

    Ok(Json(json!({ "Items": items })))
}

#[derive(Deserialize)]
struct CreateApiKeyBody {
    #[serde(alias = "Name")]
    name: Option<String>,
}

async fn create_api_key(
    State(state): State<Arc<AppState>>,
    RequireAdmin(user, _): RequireAdmin,
    Json(body): Json<CreateApiKeyBody>,
) -> AppResult<impl IntoResponse> {
    let name = body.name.ok_or(AppError::BadRequest("Name is required".into()))?;
    let key = hex::encode(rand::random::<[u8; 32]>());
    let created_by: Option<Uuid> = if user.id.starts_with("api-key-") {
        None
    } else {
        user.id.parse().ok()
    };

    let row = sqlx::query(
        "INSERT INTO api_keys (name, key, created_by) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(&name)
    .bind(&key)
    .bind(created_by)
    .fetch_one(&state.db)
    .await?;

    Ok(Json(json!({
        "Id": row.try_get::<i32, _>("id").unwrap_or(0),
        "Name": name,
        "Key": key,
        "CreatedAt": row.try_get::<chrono::NaiveDateTime, _>("created_at")
            .map(|d| d.and_utc().to_rfc3339()).unwrap_or_default(),
    })))
}

async fn delete_api_key(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(id): Path<i32>,
) -> AppResult<impl IntoResponse> {
    sqlx::query("DELETE FROM api_keys WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await?;
    Ok(StatusCode::NO_CONTENT)
}

// --- Items/Counts ---

async fn items_counts(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> AppResult<impl IntoResponse> {
    let movie: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM items WHERE type = 'Movie'")
        .fetch_one(&state.db).await?;
    let series: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM items WHERE type = 'Series'")
        .fetch_one(&state.db).await?;
    let episode: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM items WHERE type = 'Episode'")
        .fetch_one(&state.db).await?;

    Ok(Json(json!({
        "MovieCount": movie.0,
        "SeriesCount": series.0,
        "EpisodeCount": episode.0,
        "ArtistCount": 0, "ProgramCount": 0, "TrailerCount": 0,
        "SongCount": 0, "AlbumCount": 0, "MusicVideoCount": 0,
        "BoxSetCount": 0, "BookCount": 0,
        "ItemCount": movie.0 + series.0 + episode.0,
    })))
}

// --- Devices/Info ---

async fn device_info(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Query(q): Query<std::collections::HashMap<String, String>>,
) -> impl IntoResponse {
    let device_id = q.get("Id").cloned().unwrap_or_default();
    let sessions = state.session_manager.get_active_sessions();
    let session = sessions.iter().find(|s| s.device_id == device_id);

    Json(json!({
        "Id": device_id,
        "Name": session.map(|s| s.device_name.as_str()).unwrap_or("Unknown"),
        "AppName": session.map(|s| s.app_name.as_str()).unwrap_or("Unknown"),
        "LastUserName": session.map(|s| s.user_name.as_str()).unwrap_or(""),
        "LastUserId": session.map(|s| s.user_id.as_str()).unwrap_or(""),
        "DateLastActivity": session.map(|s| s.last_activity.to_rfc3339()).unwrap_or_default(),
    }))
}

// --- Session stop / message ---

async fn session_stop(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(session_id): Path<String>,
) -> impl IntoResponse {
    if let Some(idx) = session_id.find('_') {
        let user_id = &session_id[..idx];
        let device_id = &session_id[idx + 1..];
        state.session_manager.clear_now_playing(user_id, device_id);
    }
    StatusCode::NO_CONTENT
}

async fn session_message(RequireAuth(_, _): RequireAuth) -> impl IntoResponse {
    StatusCode::NO_CONTENT
}

// --- Custom SQL query (Emby Playback Reporting compat) ---

#[derive(Deserialize)]
struct CustomQueryBody {
    #[serde(alias = "CustomQueryString")]
    custom_query_string: Option<String>,
}

async fn custom_query(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<CustomQueryBody>,
) -> AppResult<impl IntoResponse> {
    let sql = body.custom_query_string.ok_or(AppError::BadRequest("CustomQueryString required".into()))?;
    let trimmed = sql.trim().to_uppercase();
    if !trimmed.starts_with("SELECT") {
        return Err(AppError::Forbidden("Only SELECT queries allowed".into()));
    }
    // Block dangerous keywords that could modify data or leak system info
    let forbidden = ["INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE",
                     "GRANT", "REVOKE", "COPY", "EXECUTE", "DO ", "CALL", "SET ",
                     "PG_READ_FILE", "PG_WRITE_FILE", "PG_SLEEP", "LO_IMPORT", "LO_EXPORT"];
    for kw in &forbidden {
        if trimmed.contains(kw) {
            return Err(AppError::Forbidden(format!("Forbidden keyword: {kw}")));
        }
    }
    // Only allow queries on known safe tables
    let allowed_tables = ["\"PlaybackActivity\"", "playback_activity", "items", "users",
                          "media_versions", "media_streams", "user_item_data", "genres", "item_genres"];
    let has_allowed = allowed_tables.iter().any(|t| sql.to_lowercase().contains(&t.to_lowercase()));
    if !has_allowed {
        return Err(AppError::Forbidden("Query must reference a known table".into()));
    }

    // Rewrite SQLite syntax to PostgreSQL
    let mut sql = sql;
    sql = sql.replace("PlaybackActivity", "\"PlaybackActivity\"");
    // rowid -> id
    let rowid_re = regex::Regex::new(r"(?i)\browid\b").unwrap();
    sql = rowid_re.replace_all(&sql, "id").to_string();
    // strftime('%Y-%m-%d', col) -> TO_CHAR(col, 'YYYY-MM-DD')
    let strftime_ymd = regex::Regex::new(r"(?i)strftime\s*\(\s*'%Y-%m-%d'\s*,\s*(\w+)\s*\)").unwrap();
    sql = strftime_ymd.replace_all(&sql, "TO_CHAR($1, 'YYYY-MM-DD')").to_string();
    let strftime_h = regex::Regex::new(r"(?i)strftime\s*\(\s*'%H'\s*,\s*(\w+)\s*\)").unwrap();
    sql = strftime_h.replace_all(&sql, "TO_CHAR($1, 'HH24')").to_string();
    let strftime_w = regex::Regex::new(r"(?i)strftime\s*\(\s*'%w'\s*,\s*(\w+)\s*\)").unwrap();
    sql = strftime_w.replace_all(&sql, "EXTRACT(DOW FROM $1)::text").to_string();
    // datetime('now', '-X days') -> (NOW() - INTERVAL 'X days')
    let datetime_days = regex::Regex::new(r"(?i)datetime\s*\(\s*'now'\s*,\s*'-(\d+)\s+days?'\s*\)").unwrap();
    sql = datetime_days.replace_all(&sql, "(NOW() - INTERVAL '$1 days')").to_string();
    let datetime_now = regex::Regex::new(r"(?i)datetime\s*\(\s*'now'\s*\)").unwrap();
    sql = datetime_now.replace_all(&sql, "NOW()").to_string();

    let rows = sqlx::query(&sql).fetch_all(&state.db).await
        .map_err(|e| AppError::BadRequest(e.to_string()))?;

    if rows.is_empty() {
        return Ok(Json(json!({ "colums": [], "results": [] })));
    }

    use sqlx::Column;
    let columns: Vec<String> = rows[0].columns().iter().map(|c| c.name().to_string()).collect();
    let results: Vec<Vec<serde_json::Value>> = rows
        .iter()
        .map(|row| {
            columns
                .iter()
                .map(|col: &String| {
                    // Try different types
                    if let Ok(v) = row.try_get::<String, _>(col.as_str()) {
                        json!(v)
                    } else if let Ok(v) = row.try_get::<i64, _>(col.as_str()) {
                        json!(v)
                    } else if let Ok(v) = row.try_get::<i32, _>(col.as_str()) {
                        json!(v)
                    } else if let Ok(v) = row.try_get::<f64, _>(col.as_str()) {
                        json!(v)
                    } else if let Ok(v) = row.try_get::<bool, _>(col.as_str()) {
                        json!(v)
                    } else {
                        json!(null)
                    }
                })
                .collect()
        })
        .collect();

    Ok(Json(json!({ "colums": columns, "results": results })))
}

// --- Generic /Items search ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct ItemsSearchQuery {
    #[serde(alias = "Ids")]
    ids: Option<String>,
    #[serde(alias = "SearchTerm")]
    search_term: Option<String>,
    #[serde(alias = "IncludeItemTypes")]
    include_item_types: Option<String>,
    #[serde(alias = "Fields")]
    fields: Option<String>,
    #[serde(alias = "Limit")]
    limit: Option<String>,
}

async fn items_search(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Query(q): Query<ItemsSearchQuery>,
) -> AppResult<impl IntoResponse> {
    let mut sql = "SELECT * FROM items WHERE 1=1".to_string();
    let mut params: Vec<String> = Vec::new();
    let mut idx = 1;

    // Detect if IDs are integers (emby_id) or UUIDs
    let use_emby_id = q.ids.as_ref().map(|ids| {
        ids.split(',').filter(|s| !s.is_empty()).all(|s| s.parse::<i64>().is_ok())
    }).unwrap_or(false);

    if let Some(ref ids) = q.ids {
        let id_list: Vec<&str> = ids.split(',').filter(|s| !s.is_empty()).collect();
        if use_emby_id {
            let placeholders: Vec<String> = id_list.iter().map(|_| { let p = format!("${idx}::int"); idx += 1; p }).collect();
            sql.push_str(&format!(" AND emby_id IN ({})", placeholders.join(",")));
        } else {
            let placeholders: Vec<String> = id_list.iter().map(|_| { let p = format!("${idx}::uuid"); idx += 1; p }).collect();
            sql.push_str(&format!(" AND id IN ({})", placeholders.join(",")));
        }
        params.extend(id_list.iter().map(|s| s.to_string()));
    }
    if let Some(ref types) = q.include_item_types {
        let type_list: Vec<&str> = types.split(',').filter(|s| !s.is_empty()).collect();
        let placeholders: Vec<String> = type_list.iter().map(|_| { let p = format!("${idx}"); idx += 1; p }).collect();
        sql.push_str(&format!(" AND type IN ({})", placeholders.join(",")));
        params.extend(type_list.iter().map(|s| s.to_string()));
    }
    if let Some(ref term) = q.search_term {
        sql.push_str(&format!(" AND name ILIKE ${idx}"));
        idx += 1;
        params.push(format!("%{term}%"));
    }
    let limit: i64 = q.limit.as_ref().and_then(|s| s.parse().ok()).unwrap_or(50);
    sql.push_str(&format!(" ORDER BY sort_name LIMIT ${idx}::bigint"));
    params.push(limit.to_string());

    let mut query = sqlx::query(&sql);
    for p in &params {
        query = query.bind(p);
    }
    let rows = query.fetch_all(&state.db).await?;

    let fields = q.fields.as_deref().unwrap_or("");
    let need_media_sources = fields.contains("MediaSources") || fields.contains("Path");

    let mut items: Vec<serde_json::Value> = Vec::new();
    for row in &rows {
        let item = row_to_item_fields(row);
        let emby_id: Option<i32> = row.try_get("emby_id").ok();
        let mut dto = serde_json::to_value(&format_item_dto(&item, &state.config.server_id, None))
            .unwrap_or_default();

        // Add EmbyId field; if request used integer IDs, also override Id
        if let Some(eid) = emby_id {
            dto["EmbyId"] = json!(eid);
            if use_emby_id {
                dto["Id"] = json!(eid.to_string());
            }
        }

        // Populate MediaSources for playable items when Fields requests it
        if need_media_sources && (item.item_type == "Movie" || item.item_type == "Episode") {
            if let Some(ref fp) = item.file_path {
                let stream_rows = crate::models::item::get_media_streams(&state.db, &item.id).await.unwrap_or_default();
                let stream_dtos: Vec<serde_json::Value> = stream_rows.iter().map(|r| {
                    let s = crate::dto::format::StreamRow {
                        stream_type: r.try_get("type").unwrap_or_default(),
                        stream_index: r.try_get("stream_index").unwrap_or(0),
                        codec: r.try_get("codec").ok(),
                        language: r.try_get("language").ok(),
                        title: r.try_get("title").ok(),
                        is_default: r.try_get::<bool, _>("is_default").ok(),
                        is_forced: r.try_get::<bool, _>("is_forced").ok(),
                        channels: r.try_get("channels").ok(),
                        sample_rate: r.try_get("sample_rate").ok(),
                        bit_rate: r.try_get("bit_rate").ok(),
                        bit_depth: r.try_get("bit_depth").ok(),
                        width: r.try_get("width").ok(),
                        height: r.try_get("height").ok(),
                        pixel_format: r.try_get("pixel_format").ok(),
                        display_title: r.try_get("display_title").ok(),
                    };
                    serde_json::to_value(&crate::dto::format::format_media_stream_dto(&s)).unwrap_or_default()
                }).collect();

                let media_sources = crate::routes::videos::build_all_media_sources(
                    &state.db, &item.id, &item.name, fp,
                    item.container.as_deref(),
                    item.runtime_ticks.unwrap_or(0),
                    &stream_dtos,
                ).await;
                dto["MediaSources"] = serde_json::to_value(&media_sources).unwrap_or_default();
            }
        }

        items.push(dto);
    }

    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": items.len(),
    })))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        // Sessions
        .route("/Sessions", get(get_sessions))
        .route("/DisplayPreferences/{id}", get(get_display_prefs).post(post_display_prefs))
        .route("/Sessions/Capabilities/Full", post(capabilities))
        .route("/Sessions/{sessionId}/Playing/Stop", post(session_stop))
        .route("/Sessions/{sessionId}/Message", post(session_message))
        // Stubs
        .route("/Plugins", get(plugins))
        .route("/LiveTv/Info", get(live_tv))
        .route("/Channels", get(channels))
        .route("/Notifications/{userId}/Summary", get(notifications))
        .route("/Shows/NextUp", get(next_up))
        .route("/Studios", get(studios))
        .route("/Persons", get(persons_stub))
        .route("/Artists", get(artists))
        // Shows
        .route("/Shows/{seriesId}/Seasons", get(show_seasons))
        .route("/Shows/{seriesId}/Episodes", get(show_episodes))
        // API Keys
        .route("/ApiKeys", get(list_api_keys).post(create_api_key))
        .route("/ApiKeys/{id}", axum::routing::delete(delete_api_key))
        // Counts & search
        .route("/Items/Counts", get(items_counts))
        .route("/Devices/Info", get(device_info))
        .route("/Items", get(items_search))
        // Custom query (Emby Playback Reporting)
        .route("/user_usage_stats/submit_custom_query", post(custom_query))
}
