use sqlx::PgPool;

use crate::config::AppConfig;
use crate::services::cache::CacheService;
use crate::services::progress_buffer::ProgressBuffer;
use crate::services::session_manager::SessionManager;
use crate::services::scan_progress::ScanProgressTracker;
use crate::services::probe_task::ProbeTask;
use crate::services::file_watcher::FileWatcher;
use crate::services::log_buffer::LogBuffer;
use crate::services::tmdb::ScrapeTask;

pub struct AppState {
    pub db: PgPool,
    pub cache: CacheService,
    pub config: AppConfig,
    pub session_manager: SessionManager,
    pub progress_buffer: ProgressBuffer,
    pub scan_progress: ScanProgressTracker,
    pub probe_task: ProbeTask,
    pub file_watcher: FileWatcher,
    pub log_buffer: std::sync::Arc<LogBuffer>,
    pub scrape_task: ScrapeTask,
    pub http_client: reqwest::Client,
}
