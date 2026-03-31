use notify::Watcher;
use sqlx::PgPool;
use std::path::Path;
use std::sync::Arc;
use tokio::sync::Mutex;

use crate::services::cache::CacheService;

pub struct FileWatcher {
    running: Arc<Mutex<bool>>,
    stop_tx: Arc<Mutex<Option<tokio::sync::oneshot::Sender<()>>>>,
}

impl FileWatcher {
    pub fn new() -> Self {
        Self {
            running: Arc::new(Mutex::new(false)),
            stop_tx: Arc::new(Mutex::new(None)),
        }
    }

    pub async fn start(&self, pool: &PgPool, cache: &CacheService) {
        // Check if enabled
        let enabled: Option<String> =
            sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'file_watcher_enabled'")
                .fetch_optional(pool)
                .await
                .ok()
                .flatten();

        if enabled.as_deref() == Some("false") {
            tracing::info!("[FileWatcher] Disabled by config");
            return;
        }

        let libs = crate::models::library::get_all_libraries(pool).await.unwrap_or_default();
        if libs.is_empty() {
            tracing::info!("[FileWatcher] No libraries to watch");
            return;
        }

        // Collect all library paths with their library info
        let mut watch_paths: Vec<(String, String, String)> = Vec::new(); // (path, library_id, collection_type)
        for lib in &libs {
            for p in &lib.paths {
                if p.is_empty() || !Path::new(p).exists() {
                    continue;
                }
                if is_remote_mount(p) {
                    tracing::info!("[FileWatcher] Skipping remote mount: {p}");
                    continue;
                }
                watch_paths.push((p.clone(), lib.id.to_string(), lib.collection_type.clone()));
            }
        }

        if watch_paths.is_empty() {
            tracing::info!("[FileWatcher] No local paths to watch");
            return;
        }

        let (stop_tx, stop_rx) = tokio::sync::oneshot::channel::<()>();
        *self.stop_tx.lock().await = Some(stop_tx);
        *self.running.lock().await = true;

        let pool = pool.clone();
        let cache = cache.clone();

        tokio::spawn(async move {
            run_watcher(pool, cache, watch_paths, stop_rx).await;
        });
    }

    pub async fn stop(&self) {
        if let Some(tx) = self.stop_tx.lock().await.take() {
            let _ = tx.send(());
        }
        *self.running.lock().await = false;
        tracing::info!("[FileWatcher] Stopped");
    }

    pub async fn restart(&self, pool: &PgPool, cache: &CacheService) {
        self.stop().await;
        self.start(pool, cache).await;
    }
}

async fn run_watcher(
    pool: PgPool,
    cache: CacheService,
    watch_paths: Vec<(String, String, String)>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    let (tx, rx) = std::sync::mpsc::channel();

    let mut watcher = match notify::recommended_watcher(move |res: Result<notify::Event, notify::Error>| {
        if let Ok(event) = res {
            let _ = tx.send(event);
        }
    }) {
        Ok(w) => w,
        Err(e) => {
            tracing::error!("[FileWatcher] Failed to create watcher: {e}");
            return;
        }
    };

    // Watch all paths recursively
    let mut watched = 0;
    for (path, _, _) in &watch_paths {
        if let Err(e) = watcher.watch(Path::new(path), notify::RecursiveMode::Recursive) {
            tracing::warn!("[FileWatcher] Cannot watch {path}: {e}");
        } else {
            watched += 1;
        }
    }

    tracing::info!("[FileWatcher] Watching {watched} paths for changes");

    // Build a lookup: path prefix → (library_id, collection_type)
    let path_map: Vec<(String, String, String)> = watch_paths;

    // Process events in a loop
    loop {
        tokio::select! {
            _ = &mut stop_rx => {
                tracing::info!("[FileWatcher] Stop signal received");
                break;
            }
            _ = tokio::time::sleep(std::time::Duration::from_millis(500)) => {
                // Drain all pending events
                let mut events: Vec<notify::Event> = Vec::new();
                while let Ok(event) = rx.try_recv() {
                    events.push(event);
                }
                if events.is_empty() { continue; }

                // Deduplicate paths
                let mut created_paths = std::collections::HashSet::new();
                let mut removed_paths = std::collections::HashSet::new();

                for event in &events {
                    match event.kind {
                        notify::EventKind::Create(_) => {
                            for p in &event.paths {
                                created_paths.insert(p.to_string_lossy().to_string());
                            }
                        }
                        notify::EventKind::Remove(_) => {
                            for p in &event.paths {
                                removed_paths.insert(p.to_string_lossy().to_string());
                            }
                        }
                        notify::EventKind::Modify(notify::event::ModifyKind::Name(_)) => {
                            // Rename: treat as remove old + create new
                            for p in &event.paths {
                                let ps = p.to_string_lossy().to_string();
                                if p.exists() {
                                    created_paths.insert(ps);
                                } else {
                                    removed_paths.insert(ps);
                                }
                            }
                        }
                        _ => {}
                    }
                }

                // Handle removals: delete from DB
                for removed in &removed_paths {
                    handle_file_removed(&pool, removed).await;
                }

                // Handle creations: trigger scan for the relevant library
                let mut libs_to_scan = std::collections::HashSet::new();
                for created in &created_paths {
                    for (prefix, lib_id, _ctype) in &path_map {
                        if created.starts_with(prefix) {
                            libs_to_scan.insert(lib_id.clone());
                            break;
                        }
                    }
                }

                if !libs_to_scan.is_empty() {
                    for lib_id in &libs_to_scan {
                        // Find library info
                        if let Some((_, lid, ctype)) = path_map.iter().find(|(_, id, _)| id == lib_id) {
                            let lib = crate::models::library::get_all_libraries(&pool).await.unwrap_or_default()
                                .into_iter().find(|l| l.id.to_string() == *lid);
                            if let Some(lib) = lib {
                                tracing::info!("[FileWatcher] Changes detected, rescanning: {}", lib.name);
                                let tracker = crate::services::scan_progress::ScanProgressTracker::new();
                                crate::services::scanner::scan_library(
                                    &pool, &cache, &tracker,
                                    &lib.id.to_string(), &lib.collection_type, &lib.paths, &lib.name,
                                ).await;
                            }
                        }
                    }
                }
            }
        }
    }
}

async fn handle_file_removed(pool: &PgPool, file_path: &str) {
    // Check if this path is a known item
    let row: Option<(uuid::Uuid, String)> = sqlx::query_as(
        "SELECT id, type FROM items WHERE file_path = $1 LIMIT 1"
    ).bind(file_path).fetch_optional(pool).await.ok().flatten();

    if let Some((id, item_type)) = row {
        sqlx::query("DELETE FROM items WHERE id = $1").bind(id).execute(pool).await.ok();
        tracing::info!("[FileWatcher] Removed {item_type} from DB: {file_path}");

        // Cleanup empty parents
        if item_type == "Episode" {
            // Remove empty seasons
            sqlx::query(
                "DELETE FROM items WHERE type = 'Season' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)"
            ).execute(pool).await.ok();
            // Remove empty series
            sqlx::query(
                "DELETE FROM items WHERE type = 'Series' AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)"
            ).execute(pool).await.ok();
        }
    }
}

fn is_remote_mount(dir_path: &str) -> bool {
    let output = std::process::Command::new("df")
        .args(["-T", dir_path])
        .output();

    if let Ok(out) = output {
        let text = String::from_utf8_lossy(&out.stdout);
        if let Some(line) = text.lines().nth(1) {
            let parts: Vec<&str> = line.split_whitespace().collect();
            if let Some(fs_type) = parts.get(1) {
                let ft = fs_type.to_lowercase();
                return ft.starts_with("fuse")
                    || ["nfs", "nfs4", "cifs", "smb", "smbfs", "9p", "sshfs"]
                        .contains(&ft.as_str());
            }
        }
    }
    false
}
