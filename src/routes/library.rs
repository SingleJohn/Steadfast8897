use axum::extract::{Path, Query, State};
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use std::collections::HashMap;
use std::sync::Arc;

use crate::auth::{RequireAdmin, RequireAuth};
use crate::dto::format::{format_item_dto, ItemRow, UserDataRow};
use crate::error::{AppError, AppResult};
use crate::models::item::*;
use crate::models::library as lib_model;
use crate::state::AppState;

// --- Browse: Views ---

async fn get_views(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(_user_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    // Check cache
    if let Some(cached) = state.cache.get_json::<serde_json::Value>("views:all").await {
        return Ok(Json(cached));
    }

    let libraries = lib_model::get_all_libraries(&state.db).await?;

    // Aggregated count query
    let count_rows: Vec<(uuid::Uuid, i64, i64)> = sqlx::query_as(
        "SELECT library_id,
            COUNT(*) FILTER (WHERE type IN ('Movie', 'Series')) as child_count,
            COUNT(*) as recursive_count
        FROM items GROUP BY library_id",
    )
    .fetch_all(&state.db)
    .await?;

    let mut count_map: HashMap<String, (i64, i64)> = HashMap::new();
    for (lid, cc, rc) in &count_rows {
        count_map.insert(lid.to_string(), (*cc, *rc));
    }

    let items: Vec<serde_json::Value> = libraries
        .iter()
        .map(|lib| {
            let (child_count, recursive_count) =
                count_map.get(&lib.id.to_string()).copied().unwrap_or((0, 0));
            json!({
                "Name": lib.name,
                "ServerId": state.config.server_id,
                "Id": lib.id.to_string(),
                "Etag": lib.id.to_string(),
                "Type": "CollectionFolder",
                "CollectionType": lib.collection_type,
                "IsFolder": true,
                "ChildCount": child_count,
                "RecursiveItemCount": recursive_count,
                "ImageTags": if let Some(ref tag) = lib.primary_image_tag {
                    json!({"Primary": tag})
                } else {
                    json!({})
                },
                "BackdropImageTags": [],
                "SortName": lib.name.to_lowercase(),
                "DateCreated": lib.created_at.and_utc().to_rfc3339(),
                "UserData": {
                    "PlaybackPositionTicks": 0,
                    "PlayCount": 0,
                    "IsFavorite": false,
                    "Played": false,
                    "UnplayedItemCount": child_count,
                },
            })
        })
        .collect();

    let result = json!({ "Items": items, "TotalRecordCount": items.len() });
    state.cache.set_json("views:all", &result, 120).await;
    Ok(Json(result))
}

// --- Browse: Items ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct BrowseQuery {
    #[serde(alias = "ParentId", alias = "parentId")]
    parent_id: Option<String>,
    #[serde(alias = "IncludeItemTypes", alias = "includeItemTypes")]
    include_item_types: Option<String>,
    #[serde(alias = "Filters", alias = "filters")]
    filters: Option<String>,
    #[serde(alias = "GenreIds", alias = "genreIds")]
    genre_ids: Option<String>,
    #[serde(alias = "Years", alias = "years")]
    years: Option<String>,
    #[serde(alias = "SortBy", alias = "sortBy")]
    sort_by: Option<String>,
    #[serde(alias = "SortOrder", alias = "sortOrder")]
    sort_order: Option<String>,
    #[serde(alias = "Limit", alias = "limit")]
    limit: Option<String>,
    #[serde(alias = "StartIndex", alias = "startIndex")]
    start_index: Option<String>,
    #[serde(alias = "Recursive", alias = "recursive")]
    recursive: Option<String>,
    #[serde(alias = "SearchTerm", alias = "searchTerm")]
    search_term: Option<String>,
}

async fn browse_items(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(user_id): Path<String>,
    Query(q): Query<BrowseQuery>,
) -> AppResult<impl IntoResponse> {
    let options = ItemQueryOptions {
        parent_id: q.parent_id,
        include_item_types: q.include_item_types.map(|s| split_csv(&s)),
        sort_by: q.sort_by,
        sort_order: q.sort_order,
        limit: q.limit.and_then(|s| s.parse().ok()).filter(|&v: &i64| v > 0),
        start_index: q.start_index.and_then(|s| s.parse().ok()).filter(|&v: &i64| v > 0),
        recursive: q.recursive.as_deref() == Some("true"),
        user_id: Some(user_id),
        filters: q.filters.map(|s| split_csv(&s)),
        search_term: q.search_term,
        genre_ids: q.genre_ids.map(|s| split_csv(&s)),
        years: q
            .years
            .map(|s| s.split(',').filter_map(|v| v.parse().ok()).collect()),
        ..Default::default()
    };

    let result = query_items(&state.db, &options).await?;

    let items: Vec<serde_json::Value> = result
        .items
        .iter()
        .map(|row| {
            let item = row_to_item_fields(row);
            let ud = row_to_user_data(row);
            let dto = format_item_dto(&item, &state.config.server_id, ud.as_ref());
            serde_json::to_value(&dto).unwrap_or_default()
        })
        .collect();

    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": result.total_count,
    })))
}

// --- Resume ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct ResumeQuery {
    #[serde(alias = "Limit")]
    limit: Option<String>,
}

async fn get_resume(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(user_id): Path<String>,
    Query(q): Query<ResumeQuery>,
) -> AppResult<impl IntoResponse> {
    let limit = q.limit.and_then(|s| s.parse().ok()).unwrap_or(12i64);

    let result = query_items(
        &state.db,
        &ItemQueryOptions {
            user_id: Some(user_id),
            filters: Some(vec!["IsResumable".to_string()]),
            sort_by: Some("DatePlayed".to_string()),
            sort_order: Some("Descending".to_string()),
            limit: Some(limit),
            include_item_types: Some(vec!["Movie".to_string(), "Episode".to_string()]),
            ..Default::default()
        },
    )
    .await?;

    let items: Vec<serde_json::Value> = result
        .items
        .iter()
        .map(|row| {
            let item = row_to_item_fields(row);
            let ud = row_to_user_data(row);
            serde_json::to_value(&format_item_dto(&item, &state.config.server_id, ud.as_ref()))
                .unwrap_or_default()
        })
        .collect();

    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": result.total_count,
    })))
}

// --- Latest ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct LatestQuery {
    #[serde(alias = "ParentId", alias = "parentId")]
    parent_id: Option<String>,
    #[serde(alias = "Limit")]
    limit: Option<String>,
}

async fn get_latest(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(_user_id): Path<String>,
    Query(q): Query<LatestQuery>,
) -> AppResult<impl IntoResponse> {
    let parent_id = match q.parent_id {
        Some(pid) if !pid.is_empty() => pid,
        _ => return Ok(Json(json!([]))),
    };
    let limit = q.limit.and_then(|s| s.parse().ok()).unwrap_or(20i64);

    let rows = get_latest_items(&state.db, &state.cache, &parent_id, limit).await?;
    let items: Vec<serde_json::Value> = rows
        .iter()
        .map(|row| {
            let item = row_to_item_fields(row);
            serde_json::to_value(&format_item_dto(&item, &state.config.server_id, None))
                .unwrap_or_default()
        })
        .collect();

    Ok(Json(json!(items)))
}

// --- Latest batch ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct LatestBatchQuery {
    #[serde(alias = "LibraryIds", alias = "libraryIds")]
    library_ids: Option<String>,
    #[serde(alias = "Limit")]
    limit: Option<String>,
}

async fn get_latest_batch_handler(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(_user_id): Path<String>,
    Query(q): Query<LatestBatchQuery>,
) -> AppResult<impl IntoResponse> {
    let ids = match q.library_ids {
        Some(ref s) if !s.is_empty() => split_csv(s),
        _ => return Ok(Json(json!({}))),
    };
    let limit = q.limit.and_then(|s| s.parse().ok()).unwrap_or(16i64);

    let batch = get_latest_batch(&state.db, &state.cache, &ids, limit).await?;

    let mut formatted: serde_json::Map<String, serde_json::Value> = serde_json::Map::new();
    for (lib_id, rows) in &batch {
        let items: Vec<serde_json::Value> = rows
            .iter()
            .map(|row| {
                let item = row_to_item_fields(row);
                serde_json::to_value(&format_item_dto(&item, &state.config.server_id, None))
                    .unwrap_or_default()
            })
            .collect();
        formatted.insert(lib_id.clone(), json!(items));
    }

    Ok(Json(serde_json::Value::Object(formatted)))
}

// --- Single item detail ---

async fn get_item_detail(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path((user_id, item_id)): Path<(String, String)>,
) -> AppResult<impl IntoResponse> {
    // Check if it's a library (CollectionFolder)
    if let Ok(uid) = item_id.parse::<uuid::Uuid>() {
        if let Some(lib) = lib_model::get_library_by_id(&state.db, &uid).await? {
            let child_count: (i64,) = sqlx::query_as(
                "SELECT COUNT(*) FROM items WHERE library_id = $1 AND type IN ('Movie', 'Series')",
            )
            .bind(&uid)
            .fetch_one(&state.db)
            .await?;
            let recursive_count: (i64,) = sqlx::query_as(
                "SELECT COUNT(*) FROM items WHERE library_id = $1",
            )
            .bind(&uid)
            .fetch_one(&state.db)
            .await?;

            return Ok(Json(json!({
                "Name": lib.name,
                "ServerId": state.config.server_id,
                "Id": lib.id.to_string(),
                "Etag": lib.id.to_string(),
                "Type": "CollectionFolder",
                "CollectionType": lib.collection_type,
                "IsFolder": true,
                "ChildCount": child_count.0,
                "RecursiveItemCount": recursive_count.0,
                "SortName": lib.name.to_lowercase(),
                "DateCreated": lib.created_at.and_utc().to_rfc3339(),
                "Path": lib.paths.first().cloned().unwrap_or_default(),
                "ImageTags": if let Some(ref tag) = lib.primary_image_tag {
                    json!({"Primary": tag})
                } else {
                    json!({})
                },
                "BackdropImageTags": [],
                "UserData": {
                    "PlaybackPositionTicks": 0,
                    "PlayCount": 0,
                    "IsFavorite": false,
                    "Played": false,
                    "UnplayedItemCount": child_count.0,
                },
            })));
        }
    }

    let row = get_item_by_id(&state.db, &state.cache, &item_id)
        .await?
        .ok_or(AppError::NotFound)?;

    let item = row_to_item_fields(&row);

    // Load series for fallback
    let series_item = if (item.item_type == "Episode" || item.item_type == "Season") {
        if let Some(ref sid) = item.series_id {
            get_item_by_id(&state.db, &state.cache, sid)
                .await?
                .map(|r| row_to_item_fields(&r))
        } else {
            None
        }
    } else {
        None
    };

    // User data
    let ud_row = get_user_item_data(&state.db, &user_id, &item_id).await?;
    let ud = ud_row.as_ref().map(|r| {
        use sqlx::Row;
        UserDataRow {
            playback_position_ticks: r.try_get("playback_position_ticks").ok(),
            play_count: r.try_get("play_count").ok(),
            is_favorite: r.try_get("is_favorite").ok(),
            played: r.try_get("played").ok(),
            last_played_date: r.try_get("last_played_date").ok(),
        }
    });

    let mut dto = format_item_dto(&item, &state.config.server_id, ud.as_ref());

    // Episode/Season: fallback images from Series (use flat Emby fields, NOT ImageTags)
    if let Some(ref series) = series_item {
        if dto.image_tags.is_none() || dto.image_tags.as_ref().map(|t| t.is_empty()).unwrap_or(true) {
            if let Some(ref tag) = series.primary_image_tag {
                dto.series_primary_image_tag = Some(tag.clone());
                dto.series_primary_image_item_id = Some(series.id.clone());
                dto.parent_primary_image_item_id = Some(series.id.clone());
                dto.parent_primary_image_tag = Some(tag.clone());
                dto.parent_thumb_item_id = Some(series.id.clone());
                dto.parent_thumb_image_tag = Some(tag.clone());
            }
        }
        if dto.backdrop_image_tags.is_none()
            || dto.backdrop_image_tags.as_ref().map(|t| t.is_empty()).unwrap_or(true)
        {
            if let Some(ref tag) = series.backdrop_image_tag {
                dto.parent_backdrop_item_id = Some(series.id.clone());
                dto.parent_backdrop_image_tags = Some(vec![tag.clone()]);
            }
        }
        // Fallback overview
        if dto.overview.is_none() {
            dto.overview = series.overview.clone();
        }
    }

    // Genres (fallback to Series)
    let mut genres = get_item_genres(&state.db, &item_id).await?;
    if genres.is_empty() {
        if let Some(ref series) = series_item {
            genres = get_item_genres(&state.db, &series.id).await?;
        }
    }
    if !genres.is_empty() {
        dto.genre_items = Some(
            genres
                .iter()
                .map(|(id, name)| crate::dto::items::GenreItem {
                    id: id.clone(),
                    name: name.clone(),
                })
                .collect(),
        );
        dto.genres = Some(genres.iter().map(|(_, name)| name.clone()).collect());
    }

    // Cast (fallback to Series)
    let mut cast = get_item_cast(&state.db, &item_id).await?;
    if cast.is_empty() {
        if let Some(ref series) = series_item {
            cast = get_item_cast(&state.db, &series.id).await?;
        }
    }
    if !cast.is_empty() {
        dto.people = Some(cast);
    }

    // Child count for folders
    if dto.is_folder == Some(true) {
        dto.child_count = Some(get_child_count(&state.db, &item_id).await?);
    }

    // MediaSources for playable items (Movie/Episode)
    let mut dto_val = serde_json::to_value(&dto).unwrap_or_default();
    if item.item_type == "Movie" || item.item_type == "Episode" {
        if let Some(ref fp) = item.file_path {
            let stream_rows = get_media_streams(&state.db, &item_id).await.unwrap_or_default();
            let stream_dtos: Vec<serde_json::Value> = stream_rows.iter().map(|r| {
                use sqlx::Row as _;
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
                &state.db, &item_id, &item.name, fp,
                item.container.as_deref(),
                item.runtime_ticks.unwrap_or(0),
                &stream_dtos,
            ).await;
            dto_val["MediaSources"] = serde_json::to_value(&media_sources).unwrap_or_default();

            // Extract MediaStreams from media_versions.mediainfo (primary version)
            if stream_dtos.is_empty() {
                let mi_streams: Option<serde_json::Value> = sqlx::query_scalar(
                    "SELECT mediainfo->'MediaStreams' FROM media_versions WHERE item_id = $1::uuid AND mediainfo IS NOT NULL ORDER BY is_primary DESC LIMIT 1"
                ).bind(&item_id).fetch_optional(&state.db).await.ok().flatten();
                if let Some(streams) = mi_streams {
                    dto_val["MediaStreams"] = streams;
                }
            } else {
                dto_val["MediaStreams"] = serde_json::to_value(&stream_dtos).unwrap_or_default();
            }
        }
    }

    Ok(Json(dto_val))
}

// --- Library management ---

async fn get_virtual_folders(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> AppResult<impl IntoResponse> {
    let libs = lib_model::get_all_libraries(&state.db).await?;
    let result: Vec<serde_json::Value> = libs
        .iter()
        .map(|lib| {
            json!({
                "Name": lib.name,
                "Locations": lib.paths,
                "CollectionType": lib.collection_type,
                "ItemId": lib.id.to_string(),
                "Guid": lib.id.to_string(),
            })
        })
        .collect();
    Ok(Json(json!(result)))
}

#[derive(Deserialize)]
struct AddLibraryQuery {
    name: Option<String>,
    #[serde(alias = "collectionType")]
    collection_type: Option<String>,
}

#[derive(Deserialize)]
struct AddLibraryBody {
    #[serde(alias = "Name")]
    name: Option<String>,
    #[serde(alias = "CollectionType")]
    collection_type: Option<String>,
    #[serde(alias = "PathInfos")]
    path_infos: Option<Vec<PathInfo>>,
    #[serde(alias = "Paths")]
    paths: Option<Vec<String>>,
}

#[derive(Deserialize)]
struct PathInfo {
    #[serde(alias = "Path")]
    path: String,
}

async fn add_virtual_folder(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<AddLibraryQuery>,
    Json(body): Json<AddLibraryBody>,
) -> AppResult<impl IntoResponse> {
    let name = q
        .name
        .or(body.name)
        .ok_or(AppError::BadRequest("Name is required".into()))?;
    let collection_type = q
        .collection_type
        .or(body.collection_type)
        .unwrap_or_else(|| "movies".to_string());
    let paths: Vec<String> = body
        .path_infos
        .map(|pi| pi.into_iter().map(|p| p.path).collect())
        .or(body.paths)
        .unwrap_or_default();

    lib_model::create_library(&state.db, &name, &collection_type, &paths).await?;
    state.cache.del("views:all").await;
    Ok(StatusCode::NO_CONTENT)
}

#[derive(Deserialize)]
struct DeleteLibQuery {
    id: Option<String>,
}

async fn delete_virtual_folder(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<DeleteLibQuery>,
) -> AppResult<impl IntoResponse> {
    let id_str = q.id.ok_or(AppError::BadRequest("Id is required".into()))?;
    let uid: uuid::Uuid = id_str.parse().map_err(|_| AppError::BadRequest("Invalid Id".into()))?;
    lib_model::delete_library(&state.db, &uid).await?;
    state.cache.del("views:all").await;
    Ok(StatusCode::NO_CONTENT)
}

#[derive(Deserialize)]
struct LibPathBody {
    #[serde(alias = "Id")]
    id: Option<String>,
    #[serde(alias = "Path")]
    path: Option<String>,
}

async fn add_library_path(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<LibPathBody>,
) -> AppResult<impl IntoResponse> {
    let id_str = body.id.ok_or(AppError::BadRequest("Id is required".into()))?;
    let path = body.path.ok_or(AppError::BadRequest("Path is required".into()))?;
    let uid: uuid::Uuid = id_str.parse().map_err(|_| AppError::BadRequest("Invalid Id".into()))?;
    lib_model::add_library_path(&state.db, &uid, &path).await?;
    Ok(StatusCode::NO_CONTENT)
}

async fn remove_library_path(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<LibPathBody>,
) -> AppResult<impl IntoResponse> {
    let id_str = q.id.ok_or(AppError::BadRequest("Id is required".into()))?;
    let path = q.path.ok_or(AppError::BadRequest("Path is required".into()))?;
    let uid: uuid::Uuid = id_str.parse().map_err(|_| AppError::BadRequest("Invalid Id".into()))?;
    lib_model::remove_library_path(&state.db, &uid, &path).await?;
    Ok(StatusCode::NO_CONTENT)
}

async fn get_virtual_folder_detail(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
    Path(id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let uid: uuid::Uuid = id.parse().map_err(|_| AppError::NotFound)?;
    let lib = lib_model::get_library_by_id(&state.db, &uid)
        .await?
        .ok_or(AppError::NotFound)?;
    let count: (i64,) = sqlx::query_as(
        "SELECT COUNT(*) FROM items WHERE library_id = $1 AND type IN ('Movie', 'Series')",
    )
    .bind(&uid)
    .fetch_one(&state.db)
    .await?;

    Ok(Json(json!({
        "Id": lib.id.to_string(),
        "ItemId": lib.id.to_string(),
        "Name": lib.name,
        "CollectionType": lib.collection_type,
        "Locations": lib.paths,
        "ItemCount": count.0,
        "DateCreated": lib.created_at.and_utc().to_rfc3339(),
        "ImageTag": lib.primary_image_tag,
    })))
}

// --- Genres ---

async fn get_genres(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> AppResult<impl IntoResponse> {
    let genres = get_all_genres_with_counts(&state.db).await?;
    let items: Vec<serde_json::Value> = genres
        .iter()
        .map(|(id, name, count)| {
            json!({
                "Name": name,
                "Id": id,
                "Type": "Genre",
                "ItemCount": count,
            })
        })
        .collect();
    Ok(Json(json!({
        "Items": items,
        "TotalRecordCount": items.len(),
    })))
}

// --- Scan progress ---

async fn scan_progress(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> impl IntoResponse {
    let items = state.scan_progress.get_all();
    let result: Vec<serde_json::Value> = items.iter().map(|p| {
        json!({
            "LibraryId": p.library_id,
            "LibraryName": p.library_name,
            "Status": p.status,
            "Percentage": p.percentage,
            "ProcessedItems": p.processed_items,
            "TotalItems": p.total_items,
            "CurrentItem": p.current_item,
            "StartedAt": chrono::DateTime::from_timestamp_millis(p.started_at)
                .map(|d| d.to_rfc3339()),
            "Error": p.error,
        })
    }).collect();
    Json(json!({ "Items": result }))
}

// --- Scan triggers ---

async fn refresh_library(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let uid: uuid::Uuid = id.parse().map_err(|_| AppError::NotFound)?;
    let lib = lib_model::get_library_by_id(&state.db, &uid).await?.ok_or(AppError::NotFound)?;
    tracing::info!("[Scan] Started scanning library '{}'", lib.name);
    crate::services::scanner::scan_library(
        &state.db, &state.cache, &state.scan_progress,
        &lib.id.to_string(), &lib.collection_type, &lib.paths, &lib.name,
    ).await;
    tracing::info!("[Scan] Finished scanning library '{}'", lib.name);
    Ok(StatusCode::NO_CONTENT)
}

async fn refresh_all(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<impl IntoResponse> {
    tracing::info!("[Scan] Started full library scan");
    let state2 = state.clone();
    tokio::spawn(async move {
        crate::services::scanner::scan_all_libraries(&state2.db, &state2.cache, &state2.scan_progress).await;
        tracing::info!("[Scan] Full library scan completed");
    });
    Ok(StatusCode::NO_CONTENT)
}

// --- Probe ---

async fn probe_start(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<serde_json::Value>,
) -> AppResult<impl IntoResponse> {
    let threads = body.get("Threads").or(body.get("threads"))
        .and_then(|v| v.as_i64()).unwrap_or(5) as i32;
    state.probe_task.start(state.db.clone(), threads).await
        .map_err(|e| AppError::BadRequest(e))?;
    Ok(Json(json!({ "message": format!("Probe started with {threads} threads") })))
}

async fn probe_stop(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> impl IntoResponse {
    tokio::spawn({
        let task = state.probe_task.clone();
        async move { task.stop().await; }
    });
    Json(json!({ "message": "Probe stop requested" }))
}

async fn probe_progress(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> AppResult<impl IntoResponse> {
    let p = state.probe_task.get_progress().await;
    let missing = if p.status == "idle" {
        crate::services::probe_task::get_missing_mediainfo_count(&state.db).await.unwrap_or(0)
    } else {
        p.total_items - p.processed_items
    };
    Ok(Json(json!({
        "status": p.status,
        "totalItems": p.total_items,
        "processedItems": p.processed_items,
        "successItems": p.success_items,
        "failedItems": p.failed_items,
        "percentage": p.percentage,
        "threads": p.threads,
        "missingCount": missing,
    })))
}

// --- Scrape ---

async fn scrape_item(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(item_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let result = crate::services::tmdb::scrape_item(&state.db, &item_id).await
        .map_err(|e| AppError::BadRequest(e))?;
    Ok(Json(result))
}

async fn scrape_all_missing(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<impl IntoResponse> {
    state.scrape_task.start(state.db.clone()).await
        .map_err(|e| AppError::BadRequest(e))?;
    Ok(Json(json!({ "message": "元数据刮削已开始" })))
}

async fn scrape_progress(
    State(state): State<Arc<AppState>>,
    RequireAuth(_, _): RequireAuth,
) -> impl IntoResponse {
    Json(serde_json::to_value(&state.scrape_task.get_progress().await).unwrap_or_default())
}

async fn scrape_stop(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> impl IntoResponse {
    state.scrape_task.stop().await;
    axum::http::StatusCode::NO_CONTENT
}

// --- Helpers ---

fn split_csv(s: &str) -> Vec<String> {
    s.split(',')
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty())
        .collect()
}

// --- Library image upload ---

async fn upload_library_image(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(id): Path<String>,
    mut multipart: axum::extract::Multipart,
) -> AppResult<impl IntoResponse> {
    let uid: uuid::Uuid = id.parse().map_err(|_| AppError::BadRequest("Invalid Id".into()))?;
    let _lib = lib_model::get_library_by_id(&state.db, &uid).await?.ok_or(AppError::NotFound)?;

    let field = multipart.next_field().await
        .map_err(|e| AppError::BadRequest(format!("Multipart error: {e}")))?
        .ok_or(AppError::BadRequest("No file uploaded".into()))?;
    let data = field.bytes().await
        .map_err(|e| AppError::BadRequest(format!("Read error: {e}")))?
        .to_vec();
    if data.is_empty() {
        return Err(AppError::BadRequest("Empty file".into()));
    }

    let save_dir = format!("data/library-images/{id}");
    std::fs::create_dir_all(&save_dir).ok();
    let save_path = format!("{save_dir}/primary.jpg");

    // Resize and save as JPEG
    tokio::task::spawn_blocking({
        let data = data.clone();
        let save_path = save_path.clone();
        move || {
            if let Ok(img) = image::load_from_memory(&data) {
                let resized = img.resize(800, 1200, image::imageops::FilterType::Lanczos3);
                resized.save(&save_path).ok();
            } else {
                std::fs::write(&save_path, &data).ok();
            }
        }
    }).await.ok();

    let tag = crate::services::scanner::generate_image_tag(&save_path)
        .unwrap_or_else(|| format!("{:x}", md5::compute(&data))[..16].to_string());

    lib_model::update_library_image(&state.db, &uid, &save_path, &tag).await?;

    // Invalidate views cache
    state.cache.del("views:all").await;

    Ok(Json(serde_json::json!({ "ImageTag": tag })))
}

async fn delete_library_image(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let uid: uuid::Uuid = id.parse().map_err(|_| AppError::BadRequest("Invalid Id".into()))?;
    lib_model::delete_library_image(&state.db, &uid).await?;
    let dir = format!("data/library-images/{id}");
    std::fs::remove_dir_all(&dir).ok();
    state.cache.del("views:all").await;
    Ok(StatusCode::NO_CONTENT)
}

// --- Browse server directories ---

#[derive(Deserialize, Default)]
#[serde(default)]
struct BrowseDirQuery {
    #[serde(alias = "Path", alias = "path")]
    path: Option<String>,
}

async fn browse_directories(
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<BrowseDirQuery>,
) -> AppResult<impl IntoResponse> {
    let base = q.path.unwrap_or_else(|| "/mnt".to_string());
    let base = base.trim().to_string();

    let read_dir = tokio::fs::read_dir(&base).await
        .map_err(|e| AppError::BadRequest(format!("Cannot read {base}: {e}")))?;

    let mut dirs: Vec<serde_json::Value> = Vec::new();
    let mut entries = read_dir;
    while let Ok(Some(entry)) = entries.next_entry().await {
        if let Ok(ft) = entry.file_type().await {
            if ft.is_dir() {
                let name = entry.file_name().to_string_lossy().to_string();
                let full_path = entry.path().to_string_lossy().to_string();
                dirs.push(json!({ "Name": name, "Path": full_path }));
            }
        }
    }
    dirs.sort_by(|a, b| {
        a["Name"].as_str().unwrap_or("").cmp(b["Name"].as_str().unwrap_or(""))
    });

    Ok(Json(json!({
        "Path": base,
        "Directories": dirs,
    })))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        // Browse routes (must come before parameterized)
        .route("/Users/{userId}/Views", get(get_views))
        .route("/Users/{userId}/Items/Resume", get(get_resume))
        .route("/Users/{userId}/Items/Latest", get(get_latest))
        .route("/Users/{userId}/Items/LatestBatch", get(get_latest_batch_handler))
        .route("/Users/{userId}/Items", get(browse_items))
        .route("/Users/{userId}/Items/{itemId}", get(get_item_detail))
        // Library management
        .route("/Library/VirtualFolders", get(get_virtual_folders))
        .route("/Library/VirtualFolders/Add", post(add_virtual_folder))
        .route("/Library/VirtualFolders/Paths", post(add_library_path).delete(remove_library_path))
        .route("/Library/VirtualFolders/{id}", get(get_virtual_folder_detail))
        .route("/Library/VirtualFolders/{id}/Image", post(upload_library_image).delete(delete_library_image)
            .layer(axum::extract::DefaultBodyLimit::max(20 * 1024 * 1024)))
        .route("/Library/VirtualFolders/{id}/Refresh", post(refresh_library))
        .route("/Library/VirtualFolders", axum::routing::delete(delete_virtual_folder))
        // Scan
        .route("/Library/Scan/Progress", get(scan_progress))
        .route("/Library/Refresh", post(refresh_all))
        // Probe
        .route("/Library/Probe/Start", post(probe_start))
        .route("/Library/Probe/Stop", post(probe_stop))
        .route("/Library/Probe/Progress", get(probe_progress))
        // Scrape
        .route("/Items/{itemId}/Refresh", post(scrape_item))
        .route("/Library/Refresh/Metadata", post(scrape_all_missing))
        .route("/Library/Scrape/Progress", get(scrape_progress))
        .route("/Library/Scrape/Stop", post(scrape_stop))
        // Browse directories
        .route("/Library/BrowseDirectories", get(browse_directories))
        // Genres
        .route("/Genres", get(get_genres))
}
