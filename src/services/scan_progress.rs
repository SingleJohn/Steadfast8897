use dashmap::DashMap;
use std::sync::Arc;

use crate::services::cache::CacheService;

#[derive(Debug, Clone, serde::Serialize)]
pub struct ScanProgress {
    pub library_id: String,
    pub library_name: String,
    pub status: String, // "scanning" | "completed" | "failed"
    pub total_items: i64,
    pub processed_items: i64,
    pub current_item: Option<String>,
    pub started_at: i64,
    pub completed_at: Option<i64>,
    pub percentage: i32,
    pub error: Option<String>,
}

#[derive(Clone)]
pub struct ScanProgressTracker {
    progress: Arc<DashMap<String, ScanProgress>>,
}

impl ScanProgressTracker {
    pub fn new() -> Self {
        Self {
            progress: Arc::new(DashMap::new()),
        }
    }

    pub fn start_scan(&self, library_id: &str, library_name: &str, total_items: i64) {
        self.progress.insert(
            library_id.to_string(),
            ScanProgress {
                library_id: library_id.to_string(),
                library_name: library_name.to_string(),
                status: "scanning".to_string(),
                total_items,
                processed_items: 0,
                current_item: None,
                started_at: chrono::Utc::now().timestamp_millis(),
                completed_at: None,
                percentage: 0,
                error: None,
            },
        );
    }

    pub fn update_scan(&self, library_id: &str, processed_items: i64, current_item: Option<&str>) {
        if let Some(mut p) = self.progress.get_mut(library_id) {
            p.processed_items = processed_items;
            p.current_item = current_item.map(|s| s.to_string());
            p.percentage = if p.total_items > 0 {
                ((processed_items as f64 / p.total_items as f64) * 100.0).round() as i32
            } else {
                0
            };
        }
    }

    pub fn complete_scan(&self, library_id: &str, cache: &CacheService) {
        if let Some(mut p) = self.progress.get_mut(library_id) {
            p.status = "completed".to_string();
            p.percentage = 100;
            p.processed_items = p.total_items;
            p.current_item = None;
            p.completed_at = Some(chrono::Utc::now().timestamp_millis());
        }

        // Invalidate caches
        let cache = cache.clone();
        let lid = library_id.to_string();
        tokio::spawn(async move {
            cache.del("views:all").await;
            cache.del_pattern(&format!("latest:{lid}:*")).await;
        });

        // Remove after 60 seconds
        let progress = self.progress.clone();
        let lid = library_id.to_string();
        tokio::spawn(async move {
            tokio::time::sleep(std::time::Duration::from_secs(60)).await;
            if let Some(p) = progress.get(&lid) {
                if p.status == "completed" {
                    drop(p);
                    progress.remove(&lid);
                }
            }
        });
    }

    pub fn fail_scan(&self, library_id: &str, error: &str) {
        if let Some(mut p) = self.progress.get_mut(library_id) {
            p.status = "failed".to_string();
            p.error = Some(error.to_string());
            p.completed_at = Some(chrono::Utc::now().timestamp_millis());
        }

        let progress = self.progress.clone();
        let lid = library_id.to_string();
        tokio::spawn(async move {
            tokio::time::sleep(std::time::Duration::from_secs(60)).await;
            if let Some(p) = progress.get(&lid) {
                if p.status == "failed" {
                    drop(p);
                    progress.remove(&lid);
                }
            }
        });
    }

    pub fn get_all(&self) -> Vec<ScanProgress> {
        self.progress.iter().map(|e| e.value().clone()).collect()
    }

    pub fn get(&self, library_id: &str) -> Option<ScanProgress> {
        self.progress.get(library_id).map(|e| e.clone())
    }

    pub fn is_scanning(&self, library_id: &str) -> bool {
        self.progress
            .get(library_id)
            .map(|p| p.status == "scanning")
            .unwrap_or(false)
    }

    pub fn is_any_scanning(&self) -> bool {
        self.progress.iter().any(|e| e.status == "scanning")
    }
}
