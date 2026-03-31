use axum::body::Body;
use axum::extract::{Path, Query, State};
use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::get;
use axum::Router;
use image::imageops::FilterType;
use serde::Deserialize;
use sqlx::Row;
use std::path::PathBuf;
use std::sync::Arc;
use tokio::sync::Semaphore;

use crate::error::{AppError, AppResult};
use crate::models::item::get_item_by_id;
use crate::state::AppState;

// Limit concurrent image processing to avoid memory explosion
static IMAGE_SEMAPHORE: std::sync::LazyLock<Semaphore> = std::sync::LazyLock::new(|| Semaphore::new(3));

#[derive(Deserialize, Default)]
#[serde(default)]
struct ImageQuery {
    #[serde(alias = "Width", alias = "width")]
    width_param: Option<String>,
    #[serde(alias = "Height", alias = "height")]
    height_param: Option<String>,
    #[serde(alias = "MaxWidth", alias = "maxWidth")]
    max_width: Option<String>,
    #[serde(alias = "MaxHeight", alias = "maxHeight")]
    max_height: Option<String>,
    #[serde(alias = "Quality", alias = "quality")]
    quality: Option<String>,
    #[serde(alias = "Format", alias = "format")]
    format: Option<String>,
    #[serde(alias = "Tag", alias = "tag")]
    tag: Option<String>,
}

async fn get_item_image(
    State(state): State<Arc<AppState>>,
    Path(params): Path<(String, String)>,
    Query(q): Query<ImageQuery>,
) -> AppResult<Response> {
    let (item_id, image_type) = params;

    let img_type = image_type.to_lowercase();

    // Try items table first
    let mut image_path: Option<String> = None;
    let mut remote_url: Option<String> = None;
    if let Ok(Some(row)) = get_item_by_id(&state.db, &state.cache, &item_id).await {
        image_path = if img_type == "primary" {
            row.try_get("primary_image_path").ok()
        } else if img_type == "backdrop" {
            row.try_get("backdrop_image_path").ok()
        } else {
            None
        };

        // Fallback for Episode/Season: use Series image
        if image_path.as_ref().map_or(true, |p| p.is_empty() || !std::path::Path::new(p).exists()) {
            let item_type: String = row.try_get("type").unwrap_or_default();
            if item_type == "Episode" || item_type == "Season" {
                let series_id: Option<String> = row.try_get("series_id").ok()
                    .or_else(|| row.try_get("parent_id").ok());
                if let Some(ref sid) = series_id {
                    if let Ok(Some(series_row)) = get_item_by_id(&state.db, &state.cache, sid).await {
                        image_path = if img_type == "primary" {
                            series_row.try_get("primary_image_path").ok()
                        } else if img_type == "backdrop" {
                            series_row.try_get("backdrop_image_path").ok()
                        } else {
                            None
                        };
                    }
                }
            }
        }
    }

    // Fallback: check cast_members table (for actor headshots)
    if image_path.as_ref().map_or(true, |p| p.is_empty() || !std::path::Path::new(p).exists()) {
        if let Ok(Some(url)) = sqlx::query_scalar::<_, String>(
            "SELECT image_url FROM cast_members WHERE id = $1::uuid AND image_url IS NOT NULL"
        ).bind(&item_id).fetch_optional(&state.db).await {
            if !url.is_empty() {
                remote_url = Some(url);
            }
        }
    }

    // Fallback: check libraries table (for CollectionFolder covers)
    if remote_url.is_none() && image_path.as_ref().map_or(true, |p| p.is_empty() || !std::path::Path::new(p).exists()) {
        if let Ok(uid) = item_id.parse::<uuid::Uuid>() {
            if let Ok(Some(lib)) = crate::models::library::get_library_by_id(&state.db, &uid).await {
                if img_type == "primary" {
                    image_path = lib.primary_image_path;
                }
            }
        }
    }

    // If we have a remote URL (e.g. TMDB actor headshot), download via proxy and cache
    if let Some(ref url) = remote_url {
        if image_path.as_ref().map_or(true, |p| p.is_empty() || !std::path::Path::new(p).exists()) {
            // Shared cache dir so both Node and Rust fyms can read
            let shared_cache = std::path::PathBuf::from("/home/fyms/data/cache/images/actors");
            std::fs::create_dir_all(&shared_cache).ok();
            let cache_file = shared_cache.join(format!("{item_id}.jpg"));

            if cache_file.exists() {
                return serve_image_file(&cache_file, "jpg").await;
            }

            match state.http_client.get(url).send().await {
                Ok(resp) if resp.status().is_success() => {
                    if let Ok(bytes) = resp.bytes().await {
                        std::fs::write(&cache_file, &bytes).ok();
                        return Ok(Response::builder()
                            .status(StatusCode::OK)
                            .header(header::CONTENT_TYPE, "image/jpeg")
                            .header(header::CACHE_CONTROL, "public, max-age=31536000")
                            .header(header::CONTENT_LENGTH, bytes.len())
                            .body(Body::from(bytes))
                            .unwrap());
                    }
                }
                _ => {
                    tracing::warn!("[Image] Failed to download actor image: {}", url);
                }
            }
            return Err(AppError::NotFound);
        }
    }

    let image_path = image_path
        .filter(|p| !p.is_empty() && std::path::Path::new(p).exists())
        .ok_or(AppError::NotFound)?;

    let req_width: Option<u32> = q
        .width_param
        .or(q.max_width)
        .and_then(|s| s.parse().ok())
        .filter(|&v: &u32| v > 0);
    let req_height: Option<u32> = q
        .height_param
        .or(q.max_height)
        .and_then(|s| s.parse().ok())
        .filter(|&v: &u32| v > 0);
    let req_quality = q
        .quality
        .and_then(|s| s.parse::<u8>().ok())
        .unwrap_or(90);
    let req_format = q.format.unwrap_or_else(|| "jpg".to_string()).to_lowercase();

    // Cache path — sanitize inputs to prevent path traversal
    let safe_id = item_id.replace(|c: char| !c.is_alphanumeric() && c != '-', "");
    let safe_type = image_type.replace(|c: char| !c.is_alphanumeric(), "");
    let safe_format = req_format.replace(|c: char| !c.is_alphanumeric(), "");
    let cache_key = format!(
        "{safe_id}_{safe_type}_{}_{}_{req_quality}_{safe_format}",
        req_width.unwrap_or(0),
        req_height.unwrap_or(0)
    );
    let cache_dir = state.config.cache_dir.join("images");
    std::fs::create_dir_all(&cache_dir).ok();
    let cache_path = cache_dir.join(&cache_key);

    // Serve from cache
    if cache_path.exists() {
        return serve_image_file(&cache_path, &req_format).await;
    }

    // Limit concurrent image processing (max 3 at a time)
    let _permit = IMAGE_SEMAPHORE.acquire().await
        .map_err(|_| AppError::Internal(anyhow::anyhow!("Semaphore closed")))?;

    // Process image, write to cache file, then drop all image memory
    let result = tokio::task::spawn_blocking({
        let image_path = image_path.clone();
        let cache_path = cache_path.clone();
        let req_format = req_format.clone();
        move || -> Result<(), AppError> {
            let img = image::open(&image_path)
                .map_err(|e| AppError::Internal(anyhow::anyhow!("Image open error: {e}")))?;

            let img = if req_width.is_some() || req_height.is_some() {
                let w = req_width.unwrap_or(img.width());
                let h = req_height.unwrap_or(img.height());
                img.resize(w, h, FilterType::Lanczos3)
            } else {
                img
            };

            // Write directly to cache file
            let file = std::fs::File::create(&cache_path)
                .map_err(|e| AppError::Internal(anyhow::anyhow!("Cache file create: {e}")))?;
            let mut writer = std::io::BufWriter::new(file);

            match req_format.as_str() {
                "png" => {
                    img.write_to(&mut writer, image::ImageFormat::Png)
                        .map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;
                }
                "webp" => {
                    img.write_to(&mut writer, image::ImageFormat::WebP)
                        .map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;
                }
                _ => {
                    let encoder = image::codecs::jpeg::JpegEncoder::new_with_quality(
                        &mut writer,
                        req_quality,
                    );
                    img.write_with_encoder(encoder)
                        .map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;
                }
            }

            Ok(())
        }
    })
    .await
    .map_err(|e| AppError::Internal(anyhow::anyhow!("Task join error: {e}")))?;

    drop(_permit);

    if result.is_err() {
        // Fallback: serve original
        return serve_image_file(&PathBuf::from(&image_path), "original").await;
    }

    // Serve from disk cache — no large buffer in memory
    serve_image_file(&cache_path, &req_format).await
}

async fn serve_image_file(path: &PathBuf, format: &str) -> AppResult<Response> {
    let data = tokio::fs::read(path)
        .await
        .map_err(|_| AppError::NotFound)?;

    let content_type = match format {
        "png" => "image/png",
        "webp" => "image/webp",
        _ => {
            // Detect from extension
            match path.extension().and_then(|e| e.to_str()).unwrap_or("") {
                "png" => "image/png",
                "webp" => "image/webp",
                _ => "image/jpeg",
            }
        }
    };

    Ok(Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, content_type)
        .header(header::CACHE_CONTROL, "public, max-age=31536000")
        .header(header::CONTENT_LENGTH, data.len())
        .body(Body::from(data))
        .unwrap())
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        .route(
            "/Items/{itemId}/Images/{imageType}",
            get(get_item_image),
        )
        .route(
            "/Items/{itemId}/Images/{imageType}/{imageIndex}",
            get(get_item_image),
        )
}
