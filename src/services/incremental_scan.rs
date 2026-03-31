// Incremental scan — handles file change events from file watcher or webhook
// Processes create/delete/rename events with debouncing and path mapping

use sqlx::PgPool;
use std::path::Path;

use crate::services::cache::CacheService;
use crate::services::scanner::{
    parse_movie_name, parse_episode_info, find_image, find_nfo, parse_nfo,
    apply_nfo_data, read_mediainfo_json, resolve_strm_path, generate_image_tag,
};

const VIDEO_EXTENSIONS: &[&str] = &[
    ".mp4", ".mkv", ".avi", ".wmv", ".flv", ".webm", ".m4v", ".mov", ".ts",
    ".mpg", ".mpeg", ".iso", ".m2ts", ".vob", ".rmvb", ".rm", ".3gp", ".ogv", ".strm",
];

fn is_video_ext(ext: &str) -> bool {
    VIDEO_EXTENSIONS.contains(&ext.to_lowercase().as_str())
}

#[derive(Debug, Clone)]
pub struct FileChangeEvent {
    pub action: String,
    pub is_dir: bool,
    pub source_file: String,
    pub destination_file: Option<String>,
}

async fn get_path_mappings(pool: &PgPool) -> Vec<(String, String)> {
    let val: Option<String> = sqlx::query_scalar(
        "SELECT value FROM system_config WHERE key = 'webhook_path_mappings'",
    )
    .fetch_optional(pool)
    .await
    .ok()
    .flatten();

    if let Some(ref v) = val {
        if let Ok(arr) = serde_json::from_str::<Vec<serde_json::Value>>(v) {
            return arr
                .iter()
                .filter_map(|m| {
                    Some((
                        m.get("from")?.as_str()?.to_string(),
                        m.get("to")?.as_str()?.to_string(),
                    ))
                })
                .collect();
        }
    }
    vec![]
}

fn apply_path_mappings(path: &str, mappings: &[(String, String)]) -> String {
    for (from, to) in mappings {
        if path.starts_with(from) {
            return format!("{to}{}", &path[from.len()..]);
        }
    }
    path.to_string()
}

async fn find_library_for_path(
    pool: &PgPool,
    file_path: &str,
) -> Option<(String, String, Vec<String>, String)> {
    let libs = crate::models::library::get_all_libraries(pool).await.ok()?;
    for lib in libs {
        for lp in &lib.paths {
            let normalized = if lp.ends_with('/') { lp.clone() } else { format!("{lp}/") };
            if file_path.starts_with(&normalized) || file_path == lp.as_str() {
                return Some((
                    lib.id.to_string(),
                    lib.collection_type.clone(),
                    lib.paths.clone(),
                    lp.clone(),
                ));
            }
        }
    }
    None
}

pub async fn handle_file_change_events(pool: &PgPool, cache: &CacheService, events: Vec<FileChangeEvent>) {
    let mappings = get_path_mappings(pool).await;

    for event in events {
        let mapped_source = apply_path_mappings(&event.source_file, &mappings);
        let mapped_dest = event.destination_file.as_ref().map(|d| apply_path_mappings(d, &mappings));

        let result = match event.action.to_lowercase().as_str() {
            "create" | "add" | "modify" | "change" => {
                handle_create(pool, &mapped_source, event.is_dir).await
            }
            "delete" | "remove" => {
                handle_delete(pool, &mapped_source, event.is_dir).await
            }
            "rename" | "move" => {
                if let Some(ref dest) = mapped_dest {
                    handle_rename(pool, &mapped_source, dest, event.is_dir).await
                } else {
                    Ok(())
                }
            }
            _ => {
                tracing::warn!("[Webhook] Unknown action: {}", event.action);
                Ok(())
            }
        };

        if let Err(e) = result {
            tracing::error!("[Webhook] Error processing event: {e}");
        }
    }

    // Invalidate caches
    cache.del("views:all").await;
    cache.del_pattern("latest:*").await;
}

async fn handle_create(pool: &PgPool, file_path: &str, is_dir: bool) -> Result<(), String> {
    let lib = find_library_for_path(pool, file_path).await;
    let (lib_id, collection_type, _paths, _matched) = match lib {
        Some(l) => l,
        None => {
            tracing::debug!("[Webhook] No matching library for: {file_path}");
            return Ok(());
        }
    };

    if collection_type == "movies" {
        handle_movie_create(pool, file_path, is_dir, &lib_id).await
    } else {
        // TV show create is complex — for now just log
        tracing::info!("[Webhook] TV show file change: {file_path}");
        Ok(())
    }
}

async fn handle_movie_create(pool: &PgPool, file_path: &str, is_dir: bool, lib_id: &str) -> Result<(), String> {
    if !is_dir {
        let ext = Path::new(file_path).extension().and_then(|e| e.to_str()).unwrap_or("");
        if !is_video_ext(&format!(".{ext}")) {
            return Ok(());
        }
        // Check if already exists
        let existing: Option<(uuid::Uuid,)> = sqlx::query_as("SELECT id FROM items WHERE file_path = $1")
            .bind(file_path).fetch_optional(pool).await.map_err(|e| e.to_string())?;
        if existing.is_some() {
            return Ok(());
        }

        let basename = Path::new(file_path).file_stem().and_then(|s| s.to_str()).unwrap_or("");
        let parsed = parse_movie_name(basename);
        let mi = read_mediainfo_json(file_path);
        let runtime: Option<i64> = mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64());

        sqlx::query(
            "INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container)
             VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7)
             ON CONFLICT DO NOTHING"
        )
        .bind(lib_id).bind(&parsed.name).bind(parsed.name.to_lowercase())
        .bind(parsed.year).bind(runtime).bind(file_path).bind(ext)
        .execute(pool).await.map_err(|e| e.to_string())?;

        tracing::info!("[Webhook] Movie file added: {}", parsed.name);
    }
    Ok(())
}

async fn handle_delete(pool: &PgPool, file_path: &str, is_dir: bool) -> Result<(), String> {
    if is_dir {
        let result = sqlx::query("DELETE FROM items WHERE file_path LIKE $1")
            .bind(format!("{file_path}%"))
            .execute(pool)
            .await
            .map_err(|e| e.to_string())?;
        if result.rows_affected() > 0 {
            tracing::info!("[Webhook] Deleted {} items from directory: {file_path}", result.rows_affected());
            cleanup_empty_parents(pool).await?;
        }
    } else {
        let result = sqlx::query("DELETE FROM items WHERE file_path = $1")
            .bind(file_path)
            .execute(pool)
            .await
            .map_err(|e| e.to_string())?;
        if result.rows_affected() > 0 {
            tracing::info!("[Webhook] Deleted item: {file_path}");
            cleanup_empty_parents(pool).await?;
        }
    }
    Ok(())
}

async fn handle_rename(pool: &PgPool, old_path: &str, new_path: &str, is_dir: bool) -> Result<(), String> {
    if is_dir {
        use sqlx::Row;
        let rows = sqlx::query("SELECT id, file_path FROM items WHERE file_path LIKE $1")
            .bind(format!("{old_path}%"))
            .fetch_all(pool)
            .await
            .map_err(|e| e.to_string())?;
        for row in &rows {
            let id: uuid::Uuid = row.try_get("id").unwrap_or_default();
            let fp: String = row.try_get("file_path").unwrap_or_default();
            let updated = format!("{new_path}{}", &fp[old_path.len()..]);
            sqlx::query("UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2::uuid")
                .bind(&updated).bind(id).execute(pool).await.map_err(|e| e.to_string())?;
            sqlx::query("UPDATE media_versions SET file_path = $1 WHERE file_path = $2")
                .bind(&updated).bind(&fp).execute(pool).await.map_err(|e| e.to_string())?;
        }
        if !rows.is_empty() {
            tracing::info!("[Webhook] Renamed {} items: {old_path} -> {new_path}", rows.len());
        }
    } else {
        let result = sqlx::query("UPDATE items SET file_path = $1, updated_at = NOW() WHERE file_path = $2")
            .bind(new_path).bind(old_path).execute(pool).await.map_err(|e| e.to_string())?;
        if result.rows_affected() > 0 {
            sqlx::query("UPDATE media_versions SET file_path = $1 WHERE file_path = $2")
                .bind(new_path).bind(old_path).execute(pool).await.map_err(|e| e.to_string())?;
            tracing::info!("[Webhook] Renamed: {old_path} -> {new_path}");
        } else {
            // Treat as new file
            let ext = Path::new(new_path).extension().and_then(|e| e.to_str()).unwrap_or("");
            if is_video_ext(&format!(".{ext}")) {
                handle_create(pool, new_path, false).await?;
            }
        }
    }
    Ok(())
}

async fn cleanup_empty_parents(pool: &PgPool) -> Result<(), String> {
    sqlx::query(
        "DELETE FROM items WHERE type = 'Season' AND id NOT IN (
            SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Episode'
        ) AND type = 'Season'"
    ).execute(pool).await.map_err(|e| e.to_string())?;

    sqlx::query(
        "DELETE FROM items WHERE type = 'Series' AND id NOT IN (
            SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Season'
        ) AND type = 'Series'"
    ).execute(pool).await.map_err(|e| e.to_string())?;

    Ok(())
}
