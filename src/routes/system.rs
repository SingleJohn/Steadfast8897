use axum::body::Body;
use axum::extract::{Path, Query, State};
use axum::http::header;
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use std::sync::Arc;

use crate::auth::RequireAdmin;
use crate::models::user::PolicyUpdate;
use crate::error::{AppError, AppResult};
use crate::state::AppState;

fn get_local_ip() -> String {
    local_ip_address::local_ip()
        .map(|ip| ip.to_string())
        .unwrap_or_else(|_| "127.0.0.1".to_string())
}

fn system_info(state: &AppState, is_public: bool) -> serde_json::Value {
    let mut info = json!({
        "ServerName": state.config.server_name,
        "Version": state.config.version,
        "Id": state.config.server_id,
        "OperatingSystem": std::env::consts::OS,
        "ProductName": "FYMS",
        "StartupWizardCompleted": true,
        "LocalAddress": format!("http://{}:{}", get_local_ip(), state.config.port),
        "CanSelfRestart": true,
    });

    if !is_public {
        let obj = info.as_object_mut().unwrap();
        obj.insert(
            "OperatingSystemDisplayName".into(),
            json!(format!("{} {}", std::env::consts::OS, std::env::consts::ARCH)),
        );
        obj.insert("HasPendingRestart".into(), json!(false));
        obj.insert("IsShuttingDown".into(), json!(false));
        obj.insert("CanLaunchWebBrowser".into(), json!(false));
        obj.insert("HasUpdateAvailable".into(), json!(false));
        obj.insert("TranscodingTempPath".into(), json!(""));
        obj.insert("LogPath".into(), json!(""));
        obj.insert("InternalMetadataPath".into(), json!(""));
        obj.insert(
            "CachePath".into(),
            json!(state.config.cache_dir.to_string_lossy()),
        );
    }

    info
}

async fn get_system_info(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    Json(system_info(&state, false))
}

async fn get_system_info_public(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    Json(system_info(&state, true))
}

async fn ping() -> impl IntoResponse {
    "FYMS"
}

async fn restart(RequireAdmin(_, _): RequireAdmin) -> impl IntoResponse {
    tracing::info!("Server restart requested...");
    tokio::spawn(async {
        tokio::time::sleep(std::time::Duration::from_millis(500)).await;
        std::process::exit(0);
    });
    axum::http::StatusCode::NO_CONTENT
}

async fn shutdown(RequireAdmin(_, _): RequireAdmin) -> impl IntoResponse {
    tracing::info!("Server shutdown requested...");
    tokio::spawn(async {
        tokio::time::sleep(std::time::Duration::from_millis(500)).await;
        std::process::exit(0);
    });
    axum::http::StatusCode::NO_CONTENT
}

async fn get_configuration(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<Json<serde_json::Value>> {
    let rows: Vec<(String, String)> =
        sqlx::query_as("SELECT key, value FROM system_config")
            .fetch_all(&state.db)
            .await?;
    let mut cfg = serde_json::Map::new();
    for (k, v) in rows {
        cfg.insert(k, json!(v));
    }
    Ok(Json(serde_json::Value::Object(cfg)))
}

async fn post_configuration(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(updates): Json<serde_json::Value>,
) -> AppResult<impl IntoResponse> {
    if let Some(obj) = updates.as_object() {
        for (key, value) in obj {
            let val_str = match value {
                serde_json::Value::String(s) => s.clone(),
                other => other.to_string(),
            };
            sqlx::query(
                "INSERT INTO system_config (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = $2",
            )
            .bind(key)
            .bind(&val_str)
            .execute(&state.db)
            .await?;
        }
    }
    Ok(axum::http::StatusCode::NO_CONTENT)
}

async fn config_page() -> impl IntoResponse {
    Json(json!([]))
}

async fn branding() -> impl IntoResponse {
    Json(json!({
        "LoginDisclaimer": "",
        "CustomCss": "",
        "SplashscreenEnabled": false,
    }))
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct LogQuery {
    level: Option<String>,
    limit: Option<usize>,
}

async fn get_logs(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<LogQuery>,
) -> impl IntoResponse {
    let level = q.level.as_deref();
    let limit = q.limit.unwrap_or(500);
    let entries = state.log_buffer.get(level, limit);
    Json(json!({ "entries": entries, "total": entries.len() }))
}

// ========== Backup & Restore ==========

const BACKUP_DIR: &str = "data/backups";

fn tables_for_category(cat: &str) -> Vec<&'static str> {
    match cat {
        "settings" => vec!["system_config"],
        "users" => vec!["users", "user_policies", "api_keys", "access_tokens", "user_library_access"],
        "libraries" => vec!["libraries"],
        "media" => vec!["genres", "items", "item_genres", "cast_members", "media_versions", "media_streams", "user_item_data"],
        "activity" => vec!["playback_activity"],
        _ => vec![],
    }
}

const ALL_CATEGORIES: &[&str] = &["settings", "users", "libraries", "media", "activity"];

async fn export_table(pool: &sqlx::PgPool, table: &str) -> AppResult<Vec<serde_json::Value>> {
    let sql = format!("SELECT row_to_json(t) FROM {table} t");
    let rows: Vec<(serde_json::Value,)> = sqlx::query_as(&sql).fetch_all(pool).await?;
    Ok(rows.into_iter().map(|(v,)| v).collect())
}

#[derive(Deserialize)]
struct BackupRequest {
    categories: Vec<String>,
}

async fn create_backup(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<BackupRequest>,
) -> AppResult<impl IntoResponse> {
    let categories: Vec<&str> = if body.categories.contains(&"all".to_string()) {
        ALL_CATEGORIES.to_vec()
    } else {
        body.categories.iter().map(|s| s.as_str()).collect()
    };

    let mut data = serde_json::Map::new();
    for cat in &categories {
        for table in tables_for_category(cat) {
            let rows = export_table(&state.db, table).await?;
            data.insert(table.to_string(), json!(rows));
        }
    }

    let backup = json!({
        "version": "1.0",
        "created_at": chrono::Utc::now().to_rfc3339(),
        "categories": categories,
        "data": data,
    });

    std::fs::create_dir_all(BACKUP_DIR).ok();
    let filename = format!("backup_{}.json", chrono::Local::now().format("%Y%m%d_%H%M%S"));
    let filepath = format!("{BACKUP_DIR}/{filename}");
    let content = serde_json::to_string_pretty(&backup).map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;
    std::fs::write(&filepath, &content).map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;

    let size = content.len();
    tracing::info!("[Backup] Created {filename} ({} KB, categories: {:?})", size / 1024, categories);

    Ok(Json(json!({
        "filename": filename,
        "size": size,
        "categories": categories,
    })))
}

async fn list_backups(
    RequireAdmin(_, _): RequireAdmin,
) -> AppResult<impl IntoResponse> {
    std::fs::create_dir_all(BACKUP_DIR).ok();
    let mut backups = Vec::new();

    if let Ok(entries) = std::fs::read_dir(BACKUP_DIR) {
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            if !name.ends_with(".json") { continue; }
            let meta = entry.metadata().ok();
            let size = meta.as_ref().map(|m| m.len()).unwrap_or(0);

            // Read only the first 1KB to extract categories (avoid loading entire backup)
            let mut categories: Vec<String> = Vec::new();
            if let Ok(mut file) = std::fs::File::open(entry.path()) {
                use std::io::Read;
                let mut buf = vec![0u8; 1024];
                let n = file.read(&mut buf).unwrap_or(0);
                let header = String::from_utf8_lossy(&buf[..n]);
                // Parse categories from the header snippet
                if let Some(start) = header.find("\"categories\"") {
                    if let Some(arr_start) = header[start..].find('[') {
                        if let Some(arr_end) = header[start + arr_start..].find(']') {
                            let arr_str = &header[start + arr_start..start + arr_start + arr_end + 1];
                            if let Ok(cats) = serde_json::from_str::<Vec<String>>(arr_str) {
                                categories = cats;
                            }
                        }
                    }
                }
            }

            let created = meta.and_then(|m| m.modified().ok())
                .map(|t| {
                    let dt: chrono::DateTime<chrono::Utc> = t.into();
                    dt.to_rfc3339()
                });

            backups.push(json!({
                "filename": name,
                "size": size,
                "categories": categories,
                "created_at": created,
            }));
        }
    }

    backups.sort_by(|a, b| b["created_at"].as_str().cmp(&a["created_at"].as_str()));
    Ok(Json(json!(backups)))
}

async fn download_backup(
    RequireAdmin(_, _): RequireAdmin,
    Path(filename): Path<String>,
) -> AppResult<Response> {
    if filename.contains("..") || filename.contains('/') {
        return Err(AppError::BadRequest("Invalid filename".into()));
    }
    let filepath = format!("{BACKUP_DIR}/{filename}");
    let data = tokio::fs::read(&filepath).await.map_err(|_| AppError::NotFound)?;

    Ok(Response::builder()
        .status(200)
        .header(header::CONTENT_TYPE, "application/json")
        .header(header::CONTENT_DISPOSITION, format!("attachment; filename=\"{filename}\""))
        .header(header::CONTENT_LENGTH, data.len())
        .body(Body::from(data))
        .unwrap())
}

async fn delete_backup(
    RequireAdmin(_, _): RequireAdmin,
    Path(filename): Path<String>,
) -> AppResult<impl IntoResponse> {
    if filename.contains("..") || filename.contains('/') {
        return Err(AppError::BadRequest("Invalid filename".into()));
    }
    let filepath = format!("{BACKUP_DIR}/{filename}");
    std::fs::remove_file(&filepath).map_err(|_| AppError::NotFound)?;
    tracing::info!("[Backup] Deleted {filename}");
    Ok(axum::http::StatusCode::NO_CONTENT)
}

#[derive(Deserialize)]
struct RestoreRequest {
    filename: String,
    categories: Vec<String>,
}

async fn restore_backup(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<RestoreRequest>,
) -> AppResult<impl IntoResponse> {
    if body.filename.contains("..") || body.filename.contains('/') {
        return Err(AppError::BadRequest("Invalid filename".into()));
    }
    let filepath = format!("{BACKUP_DIR}/{}", body.filename);
    let content = std::fs::read_to_string(&filepath).map_err(|_| AppError::NotFound)?;
    let backup: serde_json::Value = serde_json::from_str(&content)
        .map_err(|e| AppError::BadRequest(format!("Invalid backup file: {e}")))?;

    let data = backup.get("data").and_then(|d| d.as_object())
        .ok_or(AppError::BadRequest("No data in backup".into()))?;

    let categories: Vec<&str> = if body.categories.contains(&"all".to_string()) {
        ALL_CATEGORIES.to_vec()
    } else {
        body.categories.iter().map(|s| s.as_str()).collect()
    };

    // Restore order matters: settings → users → libraries → media → activity
    let ordered_cats = ["settings", "users", "libraries", "media", "activity"];

    let mut tx = state.db.begin().await?;
    let mut restored_tables = Vec::new();

    for cat in &ordered_cats {
        if !categories.contains(cat) { continue; }
        // For media, truncate in reverse dependency order
        let tables = tables_for_category(cat);
        let reverse: Vec<&str> = tables.iter().rev().copied().collect();
        for table in &reverse {
            sqlx::query(&format!("TRUNCATE {table} CASCADE")).execute(&mut *tx).await?;
        }

        for table in &tables {
            if let Some(rows) = data.get(*table).and_then(|v| v.as_array()) {
                for row in rows {
                    // Use json_populate_record for automatic type casting
                    let row_json = serde_json::to_string(row).unwrap_or_default();
                    let sql = format!(
                        "INSERT INTO {table} SELECT * FROM json_populate_record(NULL::{table}, $1::json) ON CONFLICT DO NOTHING"
                    );
                    sqlx::query(&sql).bind(&row_json).execute(&mut *tx).await?;
                }
                restored_tables.push(*table);
            }
        }
    }

    tx.commit().await?;

    // Clear all caches
    state.cache.del_pattern("*").await;

    tracing::info!("[Restore] Restored from {} (tables: {:?})", body.filename, restored_tables);

    Ok(Json(json!({
        "success": true,
        "restored_tables": restored_tables,
    })))
}

// ========== Emby Migration ==========

// Emby migration: frontend parses SQLite via sql.js, sends JSON

#[derive(Deserialize)]
struct EmbyMigrateRequest {
    users: Vec<EmbyUserData>,
    policy: Option<PolicyUpdate>,
}

#[derive(Deserialize)]
struct EmbyUserData {
    name: String,
    password: String,
}

async fn emby_migrate(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Json(body): Json<EmbyMigrateRequest>,
) -> AppResult<impl IntoResponse> {
    let total = body.users.len();
    let mut imported = 0i64;
    let mut skipped = 0i64;
    let mut errors = Vec::new();

    for eu in &body.users {
        if eu.name.is_empty() { skipped += 1; continue; }

        let exists: Option<(uuid::Uuid,)> = sqlx::query_as(
            "SELECT id FROM users WHERE name = $1"
        ).bind(&eu.name).fetch_optional(&state.db).await
            .map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;

        if exists.is_some() { skipped += 1; continue; }

        let placeholder_hash = "$2b$10$placeholder.not.a.valid.bcrypt.hash.000000000000000000000";
        let emby_hash = if eu.password.is_empty() { None } else { Some(eu.password.as_str()) };

        let row: Option<(uuid::Uuid,)> = sqlx::query_as(
            "INSERT INTO users (name, password_hash, emby_password_hash, is_admin) VALUES ($1, $2, $3, FALSE) ON CONFLICT (name) DO NOTHING RETURNING id"
        )
        .bind(&eu.name)
        .bind(placeholder_hash)
        .bind(emby_hash)
        .fetch_optional(&state.db).await
        .map_err(|e| AppError::Internal(anyhow::anyhow!("{e}")))?;

        if let Some((user_id,)) = row {
            if let Some(ref p) = body.policy {
                if let Err(e) = crate::models::user::upsert_user_policy(&state.db, &user_id, p).await {
                    errors.push(format!("{}: policy error: {e}", eu.name));
                }
            } else {
                sqlx::query("INSERT INTO user_policies (user_id) VALUES ($1) ON CONFLICT DO NOTHING")
                    .bind(user_id).execute(&state.db).await.ok();
            }
            imported += 1;
        } else {
            skipped += 1;
        }
    }

    tracing::info!("[EmbyMigrate] Total: {total}, Imported: {imported}, Skipped: {skipped}, Errors: {}", errors.len());

    Ok(Json(json!({
        "total": total,
        "imported": imported,
        "skipped": skipped,
        "errors": errors,
    })))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        .route("/System/Info", get(get_system_info))
        .route("/System/Info/Public", get(get_system_info_public))
        .route("/System/Ping", get(ping).post(ping))
        .route("/System/Restart", post(restart))
        .route("/System/Shutdown", post(shutdown))
        .route(
            "/System/Configuration",
            get(get_configuration).post(post_configuration),
        )
        .route("/web/ConfigurationPage", get(config_page))
        .route("/Branding/Configuration", get(branding))
        .route("/System/Logs", get(get_logs))
        .route("/System/Backup", post(create_backup))
        .route("/System/Backups", get(list_backups))
        .route("/System/Backups/{filename}", get(download_backup).delete(delete_backup))
        .route("/System/Restore", post(restore_backup))
        .route("/System/EmbyMigrate", post(emby_migrate))
}
