#[cfg(not(target_env = "msvc"))]
use tikv_jemallocator::Jemalloc;

#[cfg(not(target_env = "msvc"))]
#[global_allocator]
static GLOBAL: Jemalloc = Jemalloc;

mod auth;
mod config;
mod db;
mod dto;
mod emby_compat;
mod error;
mod models;
mod routes;
mod services;
mod state;
mod utils;

use axum::Router;
use axum::middleware;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use tower_http::cors::CorsLayer;
use tower_http::services::{ServeDir, ServeFile};
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

use crate::config::AppConfig;
use crate::services::cache::CacheService;
use crate::services::log_buffer::{LogBuffer, BufferLayer};
use crate::services::progress_buffer::ProgressBuffer;
use crate::services::session_manager::SessionManager;
use crate::state::AppState;

#[tokio::main]
async fn main() {
    // Initialize log buffer (memory ring buffer for API)
    let log_buffer = Arc::new(LogBuffer::new(2000));

    // File appender (daily rotation) — ensure dir exists and is writable
    let log_dir = "data/logs";
    if let Err(e) = std::fs::create_dir_all(log_dir) {
        eprintln!("[WARN] Cannot create log dir {log_dir}: {e}");
    }
    // Test write permission before initializing appender
    let can_write_logs = std::fs::write(
        format!("{log_dir}/.write_test"), ""
    ).is_ok();
    if can_write_logs {
        std::fs::remove_file(format!("{log_dir}/.write_test")).ok();
    }
    let file_appender = if can_write_logs {
        Some(tracing_appender::rolling::daily(log_dir, "fyms.log"))
    } else {
        eprintln!("[WARN] Log dir {log_dir} not writable, file logging disabled");
        None
    };

    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| "fyms_rs=info,tower_http=info".into());

    // Layers: console + optional file + memory buffer
    let file_layer = file_appender.map(|fa| {
        tracing_subscriber::fmt::layer().with_ansi(false).with_writer(fa)
    });

    tracing_subscriber::registry()
        .with(env_filter)
        .with(tracing_subscriber::fmt::layer().with_writer(std::io::stdout))
        .with(file_layer)
        .with(BufferLayer::new(log_buffer.clone()))
        .init();

    // Clean old log files on startup
    if can_write_logs { cleanup_old_logs(log_dir, 7); }

    // Load config
    let config = AppConfig::from_env();
    tracing::info!("FYMS starting on port {}", config.port);

    // Connect to database
    let pool = db::create_pool(&config)
        .await
        .expect("Failed to connect to database");

    // Run migrations
    let migrations_dir = PathBuf::from("migrations");
    if migrations_dir.exists() {
        db::run_migrations(&pool, &migrations_dir)
            .await
            .expect("Failed to run migrations");
    }

    // Initialize cache (Redis + memory)
    let cache = CacheService::new(&config.redis_host, config.redis_port, config.redis_password.as_deref()).await;

    // Initialize services
    let session_manager = SessionManager::new();
    let progress_buffer = ProgressBuffer::new(pool.clone());
    let scan_progress = services::scan_progress::ScanProgressTracker::new();
    let probe_task = services::probe_task::ProbeTask::new();
    let file_watcher = services::file_watcher::FileWatcher::new();

    // Build shared HTTP client with TMDB proxy if configured
    let proxy_url: Option<String> = sqlx::query_scalar(
        "SELECT value FROM system_config WHERE key = 'tmdb_proxy'"
    ).fetch_optional(&pool).await.ok().flatten()
     .filter(|p: &String| !p.is_empty());
    let mut http_builder = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .pool_max_idle_per_host(5);
    if let Some(ref proxy) = proxy_url {
        if let Ok(p) = reqwest::Proxy::all(proxy) {
            http_builder = http_builder.proxy(p);
        }
    }
    let http_client = http_builder.build().unwrap_or_default();

    let state = Arc::new(AppState {
        db: pool,
        cache,
        config: config.clone(),
        session_manager,
        progress_buffer,
        scan_progress,
        probe_task,
        file_watcher,
        log_buffer,
        scrape_task: services::tmdb::ScrapeTask::new(),
        http_client,
    });

    // Spawn stale playback session flusher
    routes::playback::spawn_stale_flusher(state.db.clone());

    // Start file watcher for local library paths
    state.file_watcher.start(&state.db, &state.cache).await;

    // Build API routes
    let api_routes = Router::new()
        .merge(routes::system::router())
        .merge(routes::users::router())
        .merge(routes::library::router())
        .merge(routes::playback::router())
        .merge(routes::videos::router())
        .merge(routes::images::router())
        .merge(routes::compat::router())
        .merge(routes::stats::router())
        .merge(routes::webhook::router());

    // Register routes at both / and /emby/
    let mut app = Router::new()
        .merge(api_routes.clone())
        .nest("/emby", api_routes);

    // Serve frontend static files (web/dist/)
    let web_dist = PathBuf::from("web/dist");
    if web_dist.exists() {
        let index_file = web_dist.join("index.html");
        app = app.fallback_service(
            ServeDir::new(&web_dist).fallback(ServeFile::new(&index_file)),
        );
    }

    let app = app
        .layer(CorsLayer::new()
            .allow_origin(tower_http::cors::Any)
            .allow_methods([
                axum::http::Method::GET,
                axum::http::Method::POST,
                axum::http::Method::DELETE,
                axum::http::Method::OPTIONS,
            ])
            .allow_headers([
                axum::http::header::CONTENT_TYPE,
                axum::http::header::AUTHORIZATION,
                axum::http::header::HeaderName::from_static("x-emby-token"),
                axum::http::header::HeaderName::from_static("x-emby-authorization"),
            ])
        )
        .layer(middleware::from_fn(request_logger))
        .with_state(state);

    // Start server
    let addr = SocketAddr::from(([0, 0, 0, 0], config.port));
    tracing::info!("FYMS started on http://{addr}");
    tracing::info!("Server ID: {}", config.server_id);

    // Warm up database cache in background — preload items table so first request is fast
    let warmup_pool = {
        // Clone db pool before state is moved into app
        // (state was already moved into app above, so we access it via the app's state)
        // Use a separate connection
        let db_url = format!(
            "postgres://{}:{}@{}:{}/{}",
            config.db_user, config.db_password, config.db_host, config.db_port, config.db_name
        );
        sqlx::PgPool::connect_lazy(&db_url).unwrap()
    };
    tokio::spawn(async move {
        let start = std::time::Instant::now();
        let _ = sqlx::query("SELECT id, name, sort_name, type, library_id, primary_image_tag, backdrop_image_tag, production_year, community_rating, runtime_ticks FROM items ORDER BY sort_name LIMIT 1")
            .fetch_optional(&warmup_pool).await;
        // Touch each library to warm index pages
        if let Ok(libs) = sqlx::query_scalar::<_, uuid::Uuid>("SELECT id FROM libraries").fetch_all(&warmup_pool).await {
            for lid in libs {
                let _ = sqlx::query("SELECT id FROM items WHERE library_id = $1 AND type IN ('Movie','Series') ORDER BY sort_name LIMIT 50")
                    .bind(lid).fetch_all(&warmup_pool).await;
            }
        }
        tracing::info!("DB cache warmup completed in {:?}", start.elapsed());
    });

    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}

async fn request_logger(
    req: axum::extract::Request,
    next: axum::middleware::Next,
) -> axum::response::Response {
    let method = req.method().clone();
    let uri = req.uri().path().to_string();
    let query = req.uri().query().map(|q| format!("?{q}")).unwrap_or_default();

    // Extract client IP from headers or connection
    let ip = req.headers()
        .get("x-forwarded-for")
        .and_then(|v| v.to_str().ok())
        .map(|s| s.split(',').next().unwrap_or("").trim().to_string())
        .or_else(|| req.headers().get("x-real-ip").and_then(|v| v.to_str().ok()).map(|s| s.to_string()))
        .unwrap_or_else(|| "-".to_string());

    // Skip noisy polling endpoints from logs
    let is_polling = uri.ends_with("/Scan/Progress")
        || uri.ends_with("/Probe/Progress")
        || uri.ends_with("/Sessions")
        || uri.ends_with("/Ping");

    let start = std::time::Instant::now();
    let resp = next.run(req).await;
    let elapsed = start.elapsed().as_millis();
    let status = resp.status().as_u16();

    if !is_polling {
        if status >= 500 {
            tracing::error!("{method} {uri}{query} → {status} ({elapsed}ms) ip={ip}");
        } else if status >= 400 {
            tracing::warn!("{method} {uri}{query} → {status} ({elapsed}ms) ip={ip}");
        } else {
            tracing::info!("{method} {uri}{query} → {status} ({elapsed}ms) ip={ip}");
        }
    }

    resp
}

fn cleanup_old_logs(dir: &str, retention_days: i64) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    let cutoff = chrono::Utc::now() - chrono::Duration::days(retention_days);
    for entry in entries.flatten() {
        let Ok(meta) = entry.metadata() else { continue };
        let Ok(modified) = meta.modified() else { continue };
        let modified: chrono::DateTime<chrono::Utc> = modified.into();
        if modified < cutoff {
            std::fs::remove_file(entry.path()).ok();
        }
    }
}
