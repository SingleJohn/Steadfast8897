use sqlx::{PgPool, Row};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tokio::sync::Mutex;

use crate::services::metadata::probe_file;
use crate::services::scanner::resolve_strm_path;

#[derive(Debug, Clone, serde::Serialize)]
pub struct ProbeProgress {
    pub status: String,
    pub total_items: i64,
    pub processed_items: i64,
    pub success_items: i64,
    pub failed_items: i64,
    pub current_item: Option<String>,
    pub percentage: i32,
    pub threads: i32,
    pub error: Option<String>,
}

#[derive(Clone)]
pub struct ProbeTask {
    progress: Arc<Mutex<ProbeProgress>>,
    stop_flag: Arc<AtomicBool>,
}

impl ProbeTask {
    pub fn new() -> Self {
        Self {
            progress: Arc::new(Mutex::new(ProbeProgress {
                status: "idle".into(),
                total_items: 0,
                processed_items: 0,
                success_items: 0,
                failed_items: 0,
                current_item: None,
                percentage: 0,
                threads: 5,
                error: None,
            })),
            stop_flag: Arc::new(AtomicBool::new(false)),
        }
    }

    pub async fn get_progress(&self) -> ProbeProgress {
        self.progress.lock().await.clone()
    }

    pub async fn stop(&self) {
        let mut p = self.progress.lock().await;
        if p.status == "running" {
            self.stop_flag.store(true, Ordering::SeqCst);
            p.status = "stopping".into();
            tracing::info!("[Probe] Stop requested");
        }
    }

    pub async fn start(&self, pool: PgPool, threads: i32) -> Result<(), String> {
        {
            let p = self.progress.lock().await;
            if p.status == "running" || p.status == "stopping" {
                return Err("Probe task is already running".into());
            }
        }

        let threads = threads.max(1).min(20);
        self.stop_flag.store(false, Ordering::SeqCst);

        // Get items missing mediainfo
        let rows: Vec<(uuid::Uuid, uuid::Uuid, String, String)> = sqlx::query_as(
            "SELECT mv.id, mv.item_id, mv.file_path, mv.name FROM media_versions mv WHERE mv.mediainfo IS NULL ORDER BY mv.id",
        )
        .fetch_all(&pool)
        .await
        .map_err(|e| e.to_string())?;

        if rows.is_empty() {
            let mut p = self.progress.lock().await;
            *p = ProbeProgress {
                status: "completed".into(),
                total_items: 0, processed_items: 0, success_items: 0, failed_items: 0,
                current_item: None, percentage: 100, threads, error: None,
            };
            return Ok(());
        }

        // Get path mappings
        let mappings = get_probe_path_mappings(&pool).await;

        {
            let mut p = self.progress.lock().await;
            *p = ProbeProgress {
                status: "running".into(),
                total_items: rows.len() as i64,
                processed_items: 0, success_items: 0, failed_items: 0,
                current_item: None, percentage: 0, threads, error: None,
            };
        }

        tracing::info!("[Probe] Starting: {} items, {} threads", rows.len(), threads);

        let progress = self.progress.clone();
        let stop_flag = self.stop_flag.clone();

        tokio::spawn(async move {
            let sem = Arc::new(tokio::sync::Semaphore::new(threads as usize));
            let mut handles = Vec::new();

            for (mv_id, item_id, file_path, name) in rows {
                if stop_flag.load(Ordering::SeqCst) {
                    break;
                }

                let permit = sem.clone().acquire_owned().await.unwrap();
                let pool = pool.clone();
                let mappings = mappings.clone();
                let progress = progress.clone();
                let stop_flag = stop_flag.clone();

                handles.push(tokio::spawn(async move {
                    let _permit = permit;
                    if stop_flag.load(Ordering::SeqCst) {
                        return;
                    }

                    let result = probe_one_item(&pool, &mv_id.to_string(), &item_id.to_string(), &file_path, &name, &mappings).await;

                    let mut p = progress.lock().await;
                    p.processed_items += 1;
                    match result {
                        Ok(_) => p.success_items += 1,
                        Err(_) => p.failed_items += 1,
                    }
                    p.percentage = ((p.processed_items as f64 / p.total_items as f64) * 100.0) as i32;
                    p.current_item = Some(name);
                }));
            }

            for h in handles {
                let _ = h.await;
            }

            let mut p = progress.lock().await;
            if stop_flag.load(Ordering::SeqCst) {
                p.status = "idle".into();
                tracing::info!("[Probe] Stopped. {}/{} processed", p.processed_items, p.total_items);
            } else {
                p.status = "completed".into();
                tracing::info!("[Probe] Completed. {} success, {} failed", p.success_items, p.failed_items);
            }
        });

        Ok(())
    }
}

async fn get_probe_path_mappings(pool: &PgPool) -> Vec<(String, String)> {
    let val: Option<String> = sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'probe_path_mappings'")
        .fetch_optional(pool)
        .await
        .ok()
        .flatten();

    if let Some(ref v) = val {
        if let Ok(arr) = serde_json::from_str::<Vec<serde_json::Value>>(v) {
            return arr
                .iter()
                .filter_map(|m| {
                    let from = m.get("from")?.as_str()?.to_string();
                    let to = m.get("to")?.as_str()?.to_string();
                    Some((from, to))
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

async fn probe_one_item(
    pool: &PgPool,
    mv_id: &str,
    item_id: &str,
    file_path: &str,
    name: &str,
    mappings: &[(String, String)],
) -> Result<(), String> {
    let real_path = if file_path.ends_with(".strm") {
        resolve_strm_path(file_path).ok_or_else(|| format!("Cannot resolve strm: {file_path}"))?
    } else {
        file_path.to_string()
    };

    let real_path = apply_path_mappings(&real_path, mappings);

    if !std::path::Path::new(&real_path).exists() {
        return Err(format!("File not found: {real_path}"));
    }

    let result = tokio::time::timeout(
        std::time::Duration::from_secs(30),
        probe_file(&real_path),
    )
    .await
    .map_err(|_| "Probe timeout".to_string())?
    .map_err(|e| e.to_string())?;

    // Build simplified mediainfo for DB
    let streams: Vec<serde_json::Value> = result.streams.iter().map(|s| {
        serde_json::json!({
            "Codec": s.codec,
            "Type": s.stream_type,
            "Index": s.index,
            "IsDefault": s.is_default,
            "IsForced": s.is_forced,
            "Width": s.width,
            "Height": s.height,
            "BitRate": s.bit_rate,
            "Channels": s.channels,
            "SampleRate": s.sample_rate,
            "Language": s.language,
            "Title": s.title,
            "DisplayTitle": s.display_title,
        })
    }).collect();

    let file_size: Option<i64> = std::fs::metadata(&real_path).ok().map(|m| m.len() as i64);
    let bitrate: Option<i64> = if let (Some(size), true) = (file_size, result.duration_ticks > 0) {
        let dur_sec = result.duration_ticks as f64 / 10_000_000.0;
        Some((size as f64 * 8.0 / dur_sec) as i64)
    } else {
        None
    };

    let db_info = serde_json::json!({
        "Name": name,
        "Size": file_size,
        "RunTimeTicks": result.duration_ticks,
        "Bitrate": bitrate,
        "Container": result.container,
        "MediaStreams": streams,
    });

    sqlx::query("UPDATE media_versions SET mediainfo = $1, runtime_ticks = $2, bitrate = $3, size = $4 WHERE id = $5::uuid")
        .bind(serde_json::to_string(&db_info).unwrap_or_default())
        .bind(result.duration_ticks)
        .bind(bitrate)
        .bind(file_size)
        .bind(mv_id)
        .execute(pool)
        .await
        .map_err(|e| e.to_string())?;

    // Update item runtime if not set
    sqlx::query("UPDATE items SET runtime_ticks = $1, updated_at = NOW() WHERE id = $2 AND (runtime_ticks IS NULL OR runtime_ticks = 0)")
        .bind(result.duration_ticks)
        .bind(item_id)
        .execute(pool)
        .await
        .map_err(|e| e.to_string())?;

    Ok(())
}

pub async fn get_missing_mediainfo_count(pool: &PgPool) -> Result<i64, sqlx::Error> {
    let count: (i64,) = sqlx::query_as("SELECT count(*) FROM media_versions WHERE mediainfo IS NULL")
        .fetch_one(pool)
        .await?;
    Ok(count.0)
}
