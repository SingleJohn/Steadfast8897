use regex::Regex;
use sqlx::PgPool;
use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use crate::services::scan_progress::ScanProgressTracker;
use crate::services::cache::CacheService;

const VIDEO_EXTENSIONS: &[&str] = &[
    ".mp4", ".mkv", ".avi", ".wmv", ".flv", ".webm", ".m4v", ".mov", ".ts",
    ".mpg", ".mpeg", ".iso", ".bdmv", ".m2ts", ".vob", ".rmvb", ".rm",
    ".3gp", ".ogv", ".strm",
];

fn is_video_ext(ext: &str) -> bool {
    VIDEO_EXTENSIONS.contains(&ext.to_lowercase().as_str())
}

// ============ NFO Parser ============

#[derive(Debug, Clone, Default)]
pub struct NfoData {
    pub title: Option<String>,
    pub originaltitle: Option<String>,
    pub plot: Option<String>,
    pub year: Option<i32>,
    pub rating: Option<f64>,
    pub tmdbid: Option<i32>,
    pub imdbid: Option<String>,
    pub genres: Vec<String>,
    pub actors: Vec<NfoActor>,
    pub directors: Vec<String>,
    pub premiered: Option<String>,
    pub tagline: Option<String>,
}

#[derive(Debug, Clone)]
pub struct NfoActor {
    pub name: String,
    pub role: String,
    pub tmdbid: Option<i32>,
    pub image_url: Option<String>,
}

pub fn parse_nfo(nfo_path: &str) -> Option<NfoData> {
    let mut xml = std::fs::read_to_string(nfo_path).ok()?;
    // Remove BOM
    if xml.starts_with('\u{FEFF}') {
        xml = xml[3..].to_string();
    }

    let mut result = NfoData::default();

    let tag = |name: &str| -> Option<String> {
        // Try CDATA first, then plain
        let cdata_re = Regex::new(&format!(r"(?is)<{name}><!\[CDATA\[([\s\S]*?)\]\]></{name}>")).ok()?;
        if let Some(caps) = cdata_re.captures(&xml) {
            return Some(caps[1].trim().to_string());
        }
        let plain_re = Regex::new(&format!(r"(?i)<{name}>([^<]*)</{name}>")).ok()?;
        plain_re.captures(&xml).map(|c| c[1].trim().to_string())
    };

    result.title = tag("title");
    result.originaltitle = tag("originaltitle");
    result.plot = tag("plot");
    result.tagline = tag("tagline");
    result.year = tag("year").and_then(|s| s.parse().ok());
    result.rating = tag("rating").and_then(|s| s.parse().ok());
    result.tmdbid = tag("tmdbid").and_then(|s| s.parse().ok());
    result.imdbid = tag("imdbid");
    result.premiered = tag("premiered");

    // Genres
    let genre_re = Regex::new(r"(?i)<genre>([^<]*)</genre>").unwrap();
    for caps in genre_re.captures_iter(&xml) {
        let g = caps[1].trim().to_string();
        if !g.is_empty() {
            result.genres.push(g);
        }
    }

    // Actors
    let actor_re = Regex::new(r"(?is)<actor>([\s\S]*?)</actor>").unwrap();
    let name_re = Regex::new(r"(?i)<name>([^<]*)</name>").unwrap();
    let role_re = Regex::new(r"(?i)<role>([^<]*)</role>").unwrap();
    let type_re = Regex::new(r"(?i)<type>([^<]*)</type>").unwrap();
    let tmdb_re = Regex::new(r"(?i)<tmdbid>([^<]*)</tmdbid>").unwrap();

    for caps in actor_re.captures_iter(&xml) {
        let block = &caps[1];
        let name = name_re.captures(block).map(|c| c[1].trim().to_string());
        let role = role_re.captures(block).map(|c| c[1].trim().to_string()).unwrap_or_default();
        let atype = type_re.captures(block).map(|c| c[1].trim().to_string()).unwrap_or_else(|| "Actor".into());
        let tmdbid = tmdb_re.captures(block).and_then(|c| c[1].trim().parse().ok());

        if let Some(name) = name {
            if atype == "Director" {
                result.directors.push(name);
            } else {
                result.actors.push(NfoActor { name, role, tmdbid, image_url: None });
            }
        }
    }

    // Director tags
    let dir_re = Regex::new(r"(?i)<director>([^<]*)</director>").unwrap();
    for caps in dir_re.captures_iter(&xml) {
        let d = caps[1].trim().to_string();
        if !d.is_empty() && !result.directors.contains(&d) {
            result.directors.push(d);
        }
    }

    Some(result)
}

// ============ Apply NFO data to DB ============

pub async fn apply_nfo_data(pool: &PgPool, item_id: &str, nfo: &NfoData) -> Result<(), sqlx::Error> {
    // Update item fields individually (simpler than dynamic SQL)
    if let Some(ref plot) = nfo.plot {
        sqlx::query("UPDATE items SET overview = $1, updated_at = NOW() WHERE id = $2::uuid")
            .bind(plot).bind(item_id).execute(pool).await?;
    }
    if let Some(rating) = nfo.rating {
        if rating > 1.0 {
            sqlx::query("UPDATE items SET community_rating = $1, updated_at = NOW() WHERE id = $2::uuid")
                .bind(rating as f32).bind(item_id).execute(pool).await?;
        }
    }
    if let Some(tmdbid) = nfo.tmdbid {
        sqlx::query("UPDATE items SET tmdb_id = $1, updated_at = NOW() WHERE id = $2::uuid")
            .bind(tmdbid).bind(item_id).execute(pool).await?;
    }
    if let Some(ref imdbid) = nfo.imdbid {
        sqlx::query("UPDATE items SET imdb_id = $1, updated_at = NOW() WHERE id = $2::uuid")
            .bind(imdbid).bind(item_id).execute(pool).await?;
    }
    if let Some(ref premiered) = nfo.premiered {
        sqlx::query("UPDATE items SET premiere_date = $1::date, updated_at = NOW() WHERE id = $2::uuid")
            .bind(premiered).bind(item_id).execute(pool).await?;
    }
    if let Some(year) = nfo.year {
        sqlx::query("UPDATE items SET production_year = $1, updated_at = NOW() WHERE id = $2::uuid")
            .bind(year).bind(item_id).execute(pool).await?;
    }
    if let Some(ref title) = nfo.title {
        sqlx::query("UPDATE items SET name = $1, sort_name = $2, updated_at = NOW() WHERE id = $3::uuid")
            .bind(title).bind(title.to_lowercase()).bind(item_id).execute(pool).await?;
    }
    if let Some(ref tagline) = nfo.tagline {
        sqlx::query("UPDATE items SET tagline = $1, updated_at = NOW() WHERE id = $2::uuid")
            .bind(tagline).bind(item_id).execute(pool).await?;
    }

    // Genres
    if !nfo.genres.is_empty() {
        sqlx::query("DELETE FROM item_genres WHERE item_id = $1::uuid")
            .bind(item_id).execute(pool).await?;
        for genre in &nfo.genres {
            sqlx::query("INSERT INTO genres (name) VALUES ($1) ON CONFLICT (name) DO NOTHING")
                .bind(genre).execute(pool).await?;
            let gr: Option<(uuid::Uuid,)> = sqlx::query_as("SELECT id FROM genres WHERE name = $1")
                .bind(genre).fetch_optional(pool).await?;
            if let Some((gid,)) = gr {
                sqlx::query("INSERT INTO item_genres (item_id, genre_id) VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING")
                    .bind(item_id).bind(gid).execute(pool).await?;
            }
        }
    }

    // Cast
    if !nfo.actors.is_empty() || !nfo.directors.is_empty() {
        sqlx::query("DELETE FROM cast_members WHERE item_id = $1::uuid")
            .bind(item_id).execute(pool).await?;
        for dir in &nfo.directors {
            sqlx::query("INSERT INTO cast_members (item_id, name, character, role, order_index) VALUES ($1::uuid, $2, '', 'Director', 0)")
                .bind(item_id).bind(dir).execute(pool).await?;
        }
        for (i, a) in nfo.actors.iter().take(20).enumerate() {
            sqlx::query("INSERT INTO cast_members (item_id, name, character, role, order_index, tmdb_id, image_url) VALUES ($1::uuid, $2, $3, 'Actor', $4, $5, $6)")
                .bind(item_id).bind(&a.name).bind(&a.role).bind(i as i32).bind(a.tmdbid).bind(&a.image_url)
                .execute(pool).await?;
        }
    }

    Ok(())
}

// ============ Filename Parsing ============

pub struct ParsedMovie {
    pub name: String,
    pub year: Option<i32>,
}

static MOVIE_RE1: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^(.+?)\s*\((\d{4})\)").unwrap());
static MOVIE_RE2: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^\[(.+?)\s+(\d{4})\]").unwrap());
static MOVIE_RE3: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^(.+?)\s+(\d{4})(?:\s|$|\.)").unwrap());

pub fn parse_movie_name(name: &str) -> ParsedMovie {
    // "Name (2022)" pattern
    if let Some(caps) = MOVIE_RE1.captures(name) {
        return ParsedMovie {
            name: caps[1].trim().to_string(),
            year: caps[2].parse().ok(),
        };
    }
    // "[Name 2022]..." pattern
    if let Some(caps) = MOVIE_RE2.captures(name) {
        return ParsedMovie {
            name: caps[1].trim().to_string(),
            year: caps[2].parse().ok(),
        };
    }
    // "Name 2022" pattern
    if let Some(caps) = MOVIE_RE3.captures(name) {
        return ParsedMovie {
            name: caps[1].trim().to_string(),
            year: caps[2].parse().ok(),
        };
    }
    ParsedMovie {
        name: name.to_string(),
        year: None,
    }
}

pub struct ParsedEpisode {
    pub season: i32,
    pub episode: Option<i32>,
    pub title: Option<String>,
}

static EP_RE1: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?i)[Ss](\d+)[Ee](\d+)").unwrap());
static EP_RE2: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(\d+)x(\d+)").unwrap());
static EP_RE3: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?i)[Ee](\d+)").unwrap());

pub fn parse_episode_info(filename: &str) -> Option<ParsedEpisode> {
    // S01E01 pattern
    if let Some(caps) = EP_RE1.captures(filename) {
        return Some(ParsedEpisode {
            season: caps[1].parse().unwrap_or(1),
            episode: caps[2].parse().ok(),
            title: None,
        });
    }
    // 1x01 pattern
    if let Some(caps) = EP_RE2.captures(filename) {
        return Some(ParsedEpisode {
            season: caps[1].parse().unwrap_or(1),
            episode: caps[2].parse().ok(),
            title: None,
        });
    }
    // E01 pattern (assume season 1)
    if let Some(caps) = EP_RE3.captures(filename) {
        return Some(ParsedEpisode {
            season: 1,
            episode: caps[1].parse().ok(),
            title: None,
        });
    }
    None
}

// ============ Utility functions ============

/// Cache of directory entries: (filename_lowercase, full_path)
pub type DirCache = Vec<(String, String)>;

/// Read a directory once and cache all entries as (lowercase_name, full_path)
pub fn cache_dir(dir: &str) -> DirCache {
    let mut result = Vec::new();
    if let Ok(entries) = std::fs::read_dir(dir) {
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_lowercase();
            let path = entry.path().to_string_lossy().to_string();
            result.push((name, path));
        }
    }
    result
}

pub fn find_image(dir: &str, prefixes: &[&str]) -> Option<String> {
    find_image_cached(&cache_dir(dir), prefixes)
}

pub fn find_image_cached(dir_cache: &DirCache, prefixes: &[&str]) -> Option<String> {
    for (name, path) in dir_cache {
        let ext = Path::new(name.as_str()).extension().and_then(|e| e.to_str()).unwrap_or("");
        if !["jpg", "jpeg", "png", "webp"].contains(&ext) {
            continue;
        }
        let stem = Path::new(name.as_str()).file_stem().and_then(|s| s.to_str()).unwrap_or("");
        for prefix in prefixes {
            // Match: "poster.jpg" (starts_with)
            // "电影名-poster.jpg" (ends_with -prefix)
            // "电影名poster.jpg" (ends_with prefix, no dash)
            if stem.starts_with(prefix) || stem.ends_with(prefix) {
                return Some(path.clone());
            }
        }
    }
    None
}

pub fn find_nfo(dir: &str) -> Option<String> {
    find_nfo_cached(&cache_dir(dir))
}

pub fn find_nfo_cached(dir_cache: &DirCache) -> Option<String> {
    for (name, path) in dir_cache {
        if name.ends_with(".nfo") {
            return Some(path.clone());
        }
    }
    None
}

pub fn generate_image_tag(file_path: &str) -> Option<String> {
    let metadata = std::fs::metadata(file_path).ok()?;
    let mtime = metadata.modified().ok()?.duration_since(std::time::UNIX_EPOCH).ok()?.as_secs();
    let input = format!("{file_path}:{mtime}");
    let digest = md5::compute(input.as_bytes());
    Some(format!("{:x}", digest))
}

pub fn read_mediainfo_json(strm_file_path: &str) -> Option<serde_json::Value> {
    let path = Path::new(strm_file_path);
    let stem = path.file_stem()?.to_str()?;
    let dir = path.parent()?;
    let json_path = dir.join(format!("{stem}-mediainfo.json"));
    read_mediainfo_json_from_path(&json_path.to_string_lossy())
}

/// Read mediainfo JSON using a pre-cached directory listing to avoid extra exists() calls
pub fn read_mediainfo_json_cached(file_path: &str, dir_cache: &DirCache) -> Option<serde_json::Value> {
    let path = Path::new(file_path);
    let stem = path.file_stem()?.to_str()?;
    let json_name = format!("{stem}-mediainfo.json").to_lowercase();
    // Try exact match first
    if let Some(json_path) = dir_cache.iter().find(|(name, _)| *name == json_name).map(|(_, p)| p.clone()) {
        return read_mediainfo_json_from_path(&json_path);
    }
    // Fallback: find any file ending in -mediainfo.json in the directory
    if let Some(json_path) = dir_cache.iter().find(|(name, _)| name.ends_with("-mediainfo.json")).map(|(_, p)| p.clone()) {
        return read_mediainfo_json_from_path(&json_path);
    }
    None
}

fn read_mediainfo_json_from_path(json_path: &str) -> Option<serde_json::Value> {
    let data = std::fs::read_to_string(json_path).ok()?;
    let parsed: serde_json::Value = serde_json::from_str(&data).ok()?;
    // Array format: take first item's MediaSourceInfo
    if let Some(arr) = parsed.as_array() {
        let entry = arr.first()?;
        return entry.get("MediaSourceInfo").cloned().or(Some(entry.clone()));
    }
    parsed.get("MediaSourceInfo").cloned().or(Some(parsed))
}

pub fn resolve_strm_path(file_path: &str) -> Option<String> {
    if !file_path.ends_with(".strm") {
        return None;
    }
    let content = std::fs::read_to_string(file_path).ok()?;
    let line = content.lines().next()?.trim();
    if line.is_empty() || line.starts_with('#') {
        return None;
    }
    let mut resolved = line.to_string();
    if !resolved.starts_with("http") && resolved.starts_with('/') {
        if !Path::new(&resolved).exists() {
            let mnt = format!("/mnt{resolved}");
            if Path::new(&mnt).exists() {
                resolved = mnt;
            } else {
                let fixed = resolved.replacen("/CloudNAS", "/mnt/CloudNAS", 1);
                if fixed != resolved && Path::new(&fixed).exists() {
                    resolved = fixed;
                }
            }
        }
    }
    Some(resolved)
}

pub fn extract_show_name_from_episodes(show_path: &str) -> Option<String> {
    let entries = std::fs::read_dir(show_path).ok()?;
    for entry in entries.flatten() {
        if !entry.file_type().ok()?.is_file() {
            continue;
        }
        let name = entry.file_name().to_string_lossy().to_string();
        let ext = Path::new(&name).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
        if !is_video_ext(&format!(".{ext}")) {
            continue;
        }
        // Try to extract show name before SxxExx
        if let Some(caps) = EP_RE1.captures(&name) {
            let before = &name[..caps.get(0)?.start()];
            let cleaned = before.replace('.', " ").replace('-', " ").trim().to_string();
            if !cleaned.is_empty() {
                return Some(cleaned);
            }
        }
    }
    None
}

// ============ Scan Libraries ============

pub async fn scan_all_libraries(pool: &PgPool, cache: &CacheService, tracker: &ScanProgressTracker) {
    let libraries = match crate::models::library::get_all_libraries(pool).await {
        Ok(libs) => libs,
        Err(e) => {
            tracing::error!("Failed to get libraries: {e}");
            return;
        }
    };

    // Scan all libraries concurrently
    let mut handles = Vec::new();
    for lib in &libraries {
        let pool = pool.clone();
        let cache = cache.clone();
        let tracker = tracker.clone();
        let lib_id = lib.id.to_string();
        let ctype = lib.collection_type.clone();
        let paths = lib.paths.clone();
        let name = lib.name.clone();
        handles.push(tokio::spawn(async move {
            scan_library(&pool, &cache, &tracker, &lib_id, &ctype, &paths, &name).await;
        }));
    }
    for h in handles {
        let _ = h.await;
    }
}

pub async fn scan_library(
    pool: &PgPool,
    cache: &CacheService,
    tracker: &ScanProgressTracker,
    library_id: &str,
    collection_type: &str,
    paths: &[String],
    library_name: &str,
) {
    if tracker.is_scanning(library_id) {
        tracing::warn!("Library {library_name} is already scanning");
        return;
    }

    tracing::info!("[Scan] Starting scan: {library_name} ({collection_type})");

    // Count entries first
    let total = count_media_entries(paths);
    tracker.start_scan(library_id, library_name, total as i64);

    let pool = pool.clone();
    let cache = cache.clone();
    let tracker = tracker.clone();
    let library_id = library_id.to_string();
    let collection_type = collection_type.to_string();
    let paths = paths.to_vec();
    let library_name = library_name.to_string();

    tokio::spawn(async move {
        let result = if collection_type == "tvshows" {
            scan_tvshow_library_inner(&pool, &library_id, &paths, &library_name, &tracker).await
        } else {
            scan_movie_library_inner(&pool, &library_id, &paths, &library_name, &tracker).await
        };

        match result {
            Ok(_) => {
                tracing::info!("[Scan] Completed: {library_name}");

                // Clean up: remove items whose files no longer exist
                cleanup_missing_items(&pool, &library_id).await;

                tracker.complete_scan(&library_id, &cache);

                // Backfill media_versions for items that don't have them yet
                let pool3 = pool.clone();
                tokio::spawn(async move {
                    backfill_media_versions(&pool3).await;
                });

                // Auto-scrape newly added items (no overview = freshly scanned)
                let auto_enabled: Option<String> =
                    sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'auto_scrape_enabled'")
                        .fetch_optional(&pool).await.ok().flatten();

                if auto_enabled.as_deref() == Some("true") {
                    let lib_id = library_id.clone();
                    let pool2 = pool.clone();
                    tokio::spawn(async move {
                        auto_scrape_new_items(&pool2, &lib_id).await;
                    });
                }
            }
            Err(e) => {
                tracing::error!("[Scan] Failed: {library_name}: {e}");
                tracker.fail_scan(&library_id, &e.to_string());
            }
        }
    });
}

/// Remove items from DB whose file_path no longer exists on filesystem.
/// Also removes Series/Seasons that have no remaining episodes.
async fn cleanup_missing_items(pool: &PgPool, library_id: &str) {
    // 1. Check Movie/Episode items with file_path
    let rows: Vec<(uuid::Uuid, String, String)> = sqlx::query_as(
        "SELECT id, type, file_path FROM items WHERE library_id = $1::uuid AND file_path IS NOT NULL AND type IN ('Movie', 'Episode')"
    ).bind(library_id).fetch_all(pool).await.unwrap_or_default();

    let mut removed = 0i64;
    for (id, item_type, file_path) in &rows {
        if !Path::new(file_path).exists() {
            // Delete item (CASCADE will remove media_versions, user_item_data, etc.)
            sqlx::query("DELETE FROM items WHERE id = $1")
                .bind(id).execute(pool).await.ok();
            removed += 1;
        }
    }

    if removed > 0 {
        tracing::info!("[Cleanup] Removed {removed} items with missing files in library {library_id}");

        // 2. Remove empty Seasons (no episodes left)
        let empty_seasons: Vec<(uuid::Uuid,)> = sqlx::query_as(
            "SELECT s.id FROM items s WHERE s.library_id = $1::uuid AND s.type = 'Season' \
             AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = s.id AND e.type = 'Episode')"
        ).bind(library_id).fetch_all(pool).await.unwrap_or_default();

        for (sid,) in &empty_seasons {
            sqlx::query("DELETE FROM items WHERE id = $1").bind(sid).execute(pool).await.ok();
        }
        if !empty_seasons.is_empty() {
            tracing::info!("[Cleanup] Removed {} empty seasons", empty_seasons.len());
        }

        // 3. Remove empty Series (no seasons/episodes left)
        let empty_series: Vec<(uuid::Uuid,)> = sqlx::query_as(
            "SELECT s.id FROM items s WHERE s.library_id = $1::uuid AND s.type = 'Series' \
             AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = s.id)"
        ).bind(library_id).fetch_all(pool).await.unwrap_or_default();

        for (sid,) in &empty_series {
            sqlx::query("DELETE FROM items WHERE id = $1").bind(sid).execute(pool).await.ok();
        }
        if !empty_series.is_empty() {
            tracing::info!("[Cleanup] Removed {} empty series", empty_series.len());
        }
    }
}

async fn auto_scrape_new_items(pool: &PgPool, library_id: &str) {
    // Find newly added Movie/Series without metadata in this library
    let rows: Vec<(uuid::Uuid, String)> = sqlx::query_as(
        "SELECT id, name FROM items WHERE library_id = $1::uuid AND type IN ('Movie', 'Series') AND (overview IS NULL OR overview = '') ORDER BY created_at DESC LIMIT 50"
    ).bind(library_id).fetch_all(pool).await.unwrap_or_default();

    if rows.is_empty() { return; }

    tracing::info!("[AutoScrape] Scraping {} new items in library {library_id}", rows.len());

    let client = match crate::services::tmdb::TmdbClient::from_config(pool).await {
        Some(c) => c,
        None => {
            tracing::warn!("[AutoScrape] TMDB API key not configured, skipping");
            return;
        }
    };

    let mut success = 0;
    let mut failed = 0;
    for (id, name) in &rows {
        match crate::services::tmdb::scrape_item_with_client(pool, &id.to_string(), &client).await {
            Ok(_) => { success += 1; }
            Err(e) => {
                failed += 1;
                tracing::debug!("[AutoScrape] Failed {name}: {e}");
            }
        }
        tokio::time::sleep(std::time::Duration::from_millis(300)).await;
    }

    tracing::info!("[AutoScrape] Done: {success} success, {failed} failed");
}

/// Backfill media_versions for all Movie/Episode items that don't have them.
/// Reads companion -mediainfo.json files in parallel.
async fn backfill_media_versions(pool: &PgPool) {
    // Find items without media_versions
    let rows: Vec<(uuid::Uuid, String, String)> = sqlx::query_as(
        "SELECT i.id, i.file_path, i.container FROM items i \
         WHERE i.type IN ('Movie', 'Episode') AND i.file_path IS NOT NULL \
         AND NOT EXISTS (SELECT 1 FROM media_versions mv WHERE mv.item_id = i.id) \
         ORDER BY i.created_at DESC"
    ).fetch_all(pool).await.unwrap_or_default();

    if rows.is_empty() { return; }
    tracing::info!("[Backfill] Creating media_versions for {} items...", rows.len());

    let sem = std::sync::Arc::new(tokio::sync::Semaphore::new(5));
    let count = std::sync::Arc::new(std::sync::atomic::AtomicI64::new(0));
    let mut handles = Vec::with_capacity(rows.len());

    for (item_id, file_path, container) in rows {
        let pool = pool.clone();
        let sem = sem.clone();
        let count = count.clone();
        handles.push(tokio::spawn(async move {
            let _permit = sem.acquire().await.unwrap();
            let ext = Path::new(&file_path).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
            let vf_container = if ext == "strm" {
                resolve_strm_path(&file_path)
                    .and_then(|rp| Path::new(&rp).extension().and_then(|e| e.to_str()).map(|s| s.to_string()))
                    .unwrap_or_else(|| container.clone())
            } else if !container.is_empty() {
                container.clone()
            } else {
                ext.clone()
            };
            let name = Path::new(&file_path).file_stem().and_then(|s| s.to_str()).unwrap_or("Unknown").to_string();
            let mi = read_mediainfo_json(&file_path);
            sqlx::query(
                "INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size) \
                 VALUES ($1, $2, $3, $4, TRUE, $5, $6, $7, $8) ON CONFLICT DO NOTHING"
            )
            .bind(item_id).bind(&name).bind(&file_path).bind(&vf_container)
            .bind(mi.as_ref().cloned())
            .bind(mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64()))
            .bind(mi.as_ref().and_then(|m| m.get("Bitrate")).and_then(|v| v.as_i64()))
            .bind(mi.as_ref().and_then(|m| m.get("Size")).and_then(|v| v.as_i64()))
            .execute(&pool).await.ok();
            count.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        }));
    }

    for h in handles { let _ = h.await; }
    let total = count.load(std::sync::atomic::Ordering::Relaxed);
    tracing::info!("[Backfill] media_versions created for {total} items");
}

fn count_media_entries(paths: &[String]) -> usize {
    fn count_recursive(dir: &str) -> usize {
        let mut count = 0;
        if let Ok(entries) = std::fs::read_dir(dir) {
            for entry in entries.flatten() {
                let name = entry.file_name().to_string_lossy().to_string();
                if name.starts_with('.') || name.starts_with('@') { continue; }
                if entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                    // Check if it has video files (movie/show dir) or recurse
                    let has_video = std::fs::read_dir(entry.path()).ok()
                        .map(|es| es.flatten().any(|e| {
                            let n = e.file_name().to_string_lossy().to_lowercase();
                            let ext = std::path::Path::new(&*n).extension().and_then(|e| e.to_str()).unwrap_or("");
                            is_video_ext(&format!(".{ext}"))
                        }))
                        .unwrap_or(false);
                    if has_video {
                        count += 1;
                    } else {
                        count += count_recursive(&entry.path().to_string_lossy());
                    }
                } else {
                    let ext = std::path::Path::new(&name).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
                    if is_video_ext(&format!(".{ext}")) {
                        count += 1;
                    }
                }
            }
        }
        count
    }
    let mut total = 0;
    for p in paths {
        total += count_recursive(p);
    }
    total
}

async fn scan_movie_library_inner(
    pool: &PgPool,
    library_id: &str,
    paths: &[String],
    library_name: &str,
    tracker: &ScanProgressTracker,
) -> Result<(), String> {
    use std::sync::atomic::{AtomicI64, Ordering};
    use std::sync::Arc;

    // Pre-load existing file paths to skip already-scanned items
    let existing: HashSet<String> = sqlx::query_scalar::<_, String>(
        "SELECT file_path FROM items WHERE library_id = $1::uuid AND type = 'Movie'"
    ).bind(library_id).fetch_all(pool).await.map_err(|e| e.to_string())?
     .into_iter().collect();

    // Recursively collect movie entries from any depth
    struct MovieEntry {
        name: String,
        full_path: PathBuf,
        is_dir: bool,
    }

    fn looks_like_season_dir(name: &str) -> bool {
        let lower = name.to_lowercase();
        lower.starts_with("season") || lower.starts_with("s0") || lower.starts_with("s1")
            || lower.starts_with("s2") || lower.starts_with("s3") || lower.starts_with("s4")
            || lower.starts_with("s5") || lower.starts_with("s6") || lower.starts_with("s7")
            || lower.starts_with("s8") || lower.starts_with("s9")
            || (lower.contains("第") && lower.contains("季"))
            || lower == "specials" || lower == "extras"
    }

    fn looks_like_show_dir(path: &Path) -> bool {
        // A directory that contains Season subdirs is a show, not a movie
        if let Ok(entries) = std::fs::read_dir(path) {
            for entry in entries.flatten() {
                if entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                    let name = entry.file_name().to_string_lossy().to_string();
                    if looks_like_season_dir(&name) {
                        return true;
                    }
                }
            }
        }
        false
    }

    fn collect_movie_entries(dir: &str, results: &mut Vec<MovieEntry>) {
        let entries = match std::fs::read_dir(dir) {
            Ok(e) => e,
            Err(_) => return,
        };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            if name.starts_with('.') || name.starts_with('@') { continue; }
            let is_dir = entry.file_type().map(|t| t.is_dir()).unwrap_or(false);
            if is_dir {
                // Skip Season-like directories (they belong to TV shows)
                if looks_like_season_dir(&name) { continue; }
                // Skip directories that contain Season subdirs (show root)
                if looks_like_show_dir(&entry.path()) { continue; }

                // Check if this directory contains video files
                let has_video = std::fs::read_dir(entry.path()).ok()
                    .map(|entries| entries.flatten().any(|e| {
                        let n = e.file_name().to_string_lossy().to_lowercase();
                        let ext = Path::new(&*n).extension().and_then(|e| e.to_str()).unwrap_or("");
                        is_video_ext(&format!(".{ext}"))
                    }))
                    .unwrap_or(false);
                if has_video {
                    // This is a movie folder
                    results.push(MovieEntry { name, full_path: entry.path(), is_dir: true });
                } else {
                    // No video files, recurse deeper
                    collect_movie_entries(&entry.path().to_string_lossy(), results);
                }
            } else {
                // Loose file
                let ext = Path::new(&name).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
                if is_video_ext(&format!(".{ext}")) {
                    results.push(MovieEntry { name, full_path: entry.path(), is_dir: false });
                }
            }
        }
    }

    let mut all_entries = Vec::new();
    for lib_path in paths {
        collect_movie_entries(lib_path, &mut all_entries);
    }

    let processed = Arc::new(AtomicI64::new(0));
    let total = all_entries.len();
    let sem = Arc::new(tokio::sync::Semaphore::new(5)); // concurrency limit

    let mut handles = Vec::with_capacity(total);

    for entry in all_entries {
        let pool = pool.clone();
        let lib_id = library_id.to_string();
        let existing = existing.clone();
        let tracker = tracker.clone();
        let processed = processed.clone();
        let sem = sem.clone();

        handles.push(tokio::spawn(async move {
            let _permit = sem.acquire().await.unwrap();
            let result = scan_one_movie(&pool, &lib_id, &entry.name, &entry.full_path, entry.is_dir, &existing).await;
            let p = processed.fetch_add(1, Ordering::Relaxed) + 1;
            tracker.update_scan(&lib_id, p, Some(&entry.name));
            result
        }));
    }

    for h in handles {
        if let Err(e) = h.await.unwrap_or(Ok(())) {
            tracing::warn!("[Scan] Movie error: {e}");
        }
    }

    Ok(())
}

async fn scan_one_movie(
    pool: &PgPool,
    library_id: &str,
    name: &str,
    full_path: &Path,
    is_dir: bool,
    existing: &HashSet<String>,
) -> Result<(), String> {
    if is_dir {
        let ParsedMovie { name: movie_name, year } = parse_movie_name(name);

        // Single read_dir, cache all entries
        let dir_cache = cache_dir(&full_path.to_string_lossy());

        let mut video_files = Vec::new();
        for (fname, fpath) in &dir_cache {
            let ext = Path::new(fname.as_str()).extension().and_then(|e| e.to_str()).unwrap_or("");
            if is_video_ext(&format!(".{ext}")) {
                video_files.push((fname.clone(), fpath.clone()));
            }
        }
        if video_files.is_empty() { return Ok(()); }

        let (_, primary_path_str) = &video_files[0];
        let primary_file_lower = &video_files[0].0;
        let ext = Path::new(primary_file_lower.as_str()).extension().and_then(|e| e.to_str()).unwrap_or("mkv");

        // Early return: already exists, skip all I/O
        if existing.contains(primary_path_str) {
            return Ok(());
        }

        // New item: use dir_cache for all lookups (no extra read_dir calls)
        let poster = find_image_cached(&dir_cache, &["poster", "cover", "folder"]);
        let backdrop = find_image_cached(&dir_cache, &["fanart", "backdrop", "background"]);
        let mi = read_mediainfo_json_cached(primary_path_str, &dir_cache);
        let sort_name = movie_name.to_lowercase();
        let runtime: Option<i64> = mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64());
        let poster_tag = poster.as_ref().and_then(|p| generate_image_tag(p));
        let backdrop_tag = backdrop.as_ref().and_then(|p| generate_image_tag(p));

        let row: Option<(uuid::Uuid,)> = sqlx::query_as(
            "INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag)
             VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
             ON CONFLICT DO NOTHING RETURNING id"
        )
        .bind(library_id).bind(&movie_name).bind(&sort_name).bind(year)
        .bind(runtime).bind(primary_path_str).bind(ext)
        .bind(poster.as_deref()).bind(poster_tag.as_deref())
        .bind(backdrop.as_deref()).bind(backdrop_tag.as_deref())
        .fetch_optional(pool).await.map_err(|e| e.to_string())?;

        if let Some((id,)) = row {
            // NFO: use dir_cache
            if let Some(nfo_path) = find_nfo_cached(&dir_cache) {
                if let Some(nfo) = parse_nfo(&nfo_path) {
                    let _ = apply_nfo_data(pool, &id.to_string(), &nfo).await;
                }
            }
        }
    } else {
        let ext_str = Path::new(name).extension().and_then(|e| e.to_str()).unwrap_or("");
        if !is_video_ext(&format!(".{ext_str}")) { return Ok(()); }

        let full_path_str = full_path.to_string_lossy().to_string();

        // Early return: already exists, skip all I/O
        if existing.contains(&full_path_str) {
            return Ok(());
        }

        let basename = Path::new(name).file_stem().and_then(|s| s.to_str()).unwrap_or(name);
        let ParsedMovie { name: movie_name, year } = parse_movie_name(basename);
        // For loose files, parent dir cache can be shared but we read json from parent
        let parent_cache = full_path.parent().map(|p| cache_dir(&p.to_string_lossy())).unwrap_or_default();
        let mi = read_mediainfo_json_cached(&full_path_str, &parent_cache);
        let runtime: Option<i64> = mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64());

        sqlx::query(
            "INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container)
             VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7)
             ON CONFLICT DO NOTHING"
        )
        .bind(library_id).bind(&movie_name).bind(movie_name.to_lowercase())
        .bind(year).bind(runtime).bind(&full_path_str).bind(ext_str)
        .execute(pool).await.map_err(|e| e.to_string())?;
    }
    Ok(())
}

async fn scan_tvshow_library_inner(
    pool: &PgPool,
    library_id: &str,
    paths: &[String],
    _library_name: &str,
    tracker: &ScanProgressTracker,
) -> Result<(), String> {
    use std::sync::atomic::{AtomicI64, Ordering};
    use std::sync::Arc;

    static SEASON_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?i)[Ss](?:eason|taffel|aison|erie)?\s*(\d+)").unwrap());
    static SEASON_CN_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"第(\d+)季").unwrap());

    // Pre-load existing episode file_paths to skip
    let existing_eps: HashSet<String> = sqlx::query_scalar::<_, String>(
        "SELECT file_path FROM items WHERE library_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL"
    ).bind(library_id).fetch_all(pool).await.map_err(|e| e.to_string())?
     .into_iter().collect();

    // Recursively collect show directories from any depth
    // A "show directory" is one that contains Season subdirs or video files directly
    fn is_show_dir(path: &Path) -> bool {
        let entries = match std::fs::read_dir(path) { Ok(e) => e, Err(_) => return false };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            let is_dir = entry.file_type().map(|t| t.is_dir()).unwrap_or(false);
            if is_dir {
                let name_lower = name.to_lowercase();
                if name_lower.starts_with("season") || name_lower.starts_with("s0") || name_lower.starts_with("s1")
                    || name_lower.contains("第") && name_lower.contains("季") {
                    return true;
                }
            } else {
                let ext = Path::new(&name).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
                if is_video_ext(&format!(".{ext}")) {
                    return true;
                }
            }
        }
        false
    }

    fn collect_show_dirs(dir: &str, results: &mut Vec<(String, PathBuf)>) {
        let entries = match std::fs::read_dir(dir) { Ok(e) => e, Err(_) => return };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            if name.starts_with('.') || name.starts_with('@') { continue; }
            if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) { continue; }
            if is_show_dir(&entry.path()) {
                results.push((name, entry.path()));
            } else {
                // Not a show dir, recurse deeper
                collect_show_dirs(&entry.path().to_string_lossy(), results);
            }
        }
    }

    let mut show_dirs = Vec::new();
    for lib_path in paths {
        collect_show_dirs(lib_path, &mut show_dirs);
    }

    let processed = Arc::new(AtomicI64::new(0));
    let sem = Arc::new(tokio::sync::Semaphore::new(5)); // concurrency per show

    let mut handles = Vec::with_capacity(show_dirs.len());

    for (show_name_raw, show_path) in show_dirs {
        let pool = pool.clone();
        let lib_id = library_id.to_string();
        let existing_eps = existing_eps.clone();
        let tracker = tracker.clone();
        let processed = processed.clone();
        let sem = sem.clone();

        handles.push(tokio::spawn(async move {
            let _permit = sem.acquire().await.unwrap();

            let ParsedMovie { name: show_name, year: show_year } = parse_movie_name(&show_name_raw);

            // Single read_dir for show directory
            let show_cache = cache_dir(&show_path.to_string_lossy());

            // Read tvshow.nfo from cache
            let mut nfo_title = None;
            let tvshow_nfo_path = show_cache.iter().find(|(name, _)| *name == "tvshow.nfo").map(|(_, p)| p.clone());
            let nfo_data = if let Some(ref nfo_path) = tvshow_nfo_path {
                let nfo = parse_nfo(nfo_path);
                if let Some(ref n) = nfo { nfo_title = n.title.clone(); }
                nfo
            } else { None };

            let final_show_name = nfo_title.unwrap_or(show_name);
            let poster = find_image_cached(&show_cache, &["poster", "cover", "folder"]);
            let backdrop = find_image_cached(&show_cache, &["fanart", "backdrop", "background"]);
            let poster_tag = poster.as_ref().and_then(|p| generate_image_tag(p));
            let backdrop_tag = backdrop.as_ref().and_then(|p| generate_image_tag(p));

            // Upsert series with RETURNING
            let series_id: String = {
                let row: Option<(uuid::Uuid,)> = sqlx::query_as(
                    "INSERT INTO items (library_id, type, name, sort_name, production_year, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag)
                     VALUES ($1::uuid, 'Series', $2, $3, $4, $5, $6, $7, $8)
                     ON CONFLICT DO NOTHING RETURNING id"
                )
                .bind(&lib_id).bind(&final_show_name).bind(final_show_name.to_lowercase())
                .bind(show_year).bind(poster.as_deref()).bind(poster_tag.as_deref())
                .bind(backdrop.as_deref()).bind(backdrop_tag.as_deref())
                .fetch_optional(&pool).await.ok().flatten();

                match row {
                    Some((id,)) => id.to_string(),
                    None => {
                        // Already exists, fetch id
                        let existing: Option<(uuid::Uuid,)> = sqlx::query_as(
                            "SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Series' AND name = $2 LIMIT 1"
                        ).bind(&lib_id).bind(&final_show_name).fetch_optional(&pool).await.ok().flatten();
                        match existing {
                            Some((id,)) => id.to_string(),
                            None => return,
                        }
                    }
                }
            };

            if let Some(ref nfo) = nfo_data {
                let _ = apply_nfo_data(&pool, &series_id, nfo).await;
            }

            // Scan seasons
            let show_entries = match std::fs::read_dir(&show_path) { Ok(e) => e, Err(_) => return };
            for se in show_entries.flatten() {
                if !se.file_type().map(|t| t.is_dir()).unwrap_or(false) { continue; }
                let dir_name = se.file_name().to_string_lossy().to_string();
                let season_num = SEASON_RE.captures(&dir_name)
                    .or_else(|| SEASON_CN_RE.captures(&dir_name))
                    .and_then(|c| c[1].parse::<i32>().ok());
                let season_num = match season_num { Some(n) => n, None => continue };

                let season_path = se.path();
                // Single read_dir for season directory
                let season_cache = cache_dir(&season_path.to_string_lossy());
                let season_poster = find_image_cached(&season_cache, &["poster", "cover", "folder"]);
                let season_poster_tag = season_poster.as_ref().and_then(|p| generate_image_tag(p));

                // Upsert season with RETURNING
                let season_id: String = {
                    let row: Option<(uuid::Uuid,)> = sqlx::query_as(
                        "INSERT INTO items (library_id, parent_id, type, name, sort_name, index_number, series_id, series_name, primary_image_path, primary_image_tag)
                         VALUES ($1::uuid, $2::uuid, 'Season', $3, $4, $5, $6::uuid, $7, $8, $9)
                         ON CONFLICT DO NOTHING RETURNING id"
                    )
                    .bind(&lib_id).bind(&series_id)
                    .bind(format!("Season {season_num}")).bind(format!("season {:04}", season_num))
                    .bind(season_num).bind(&series_id).bind(&final_show_name)
                    .bind(season_poster.as_deref()).bind(season_poster_tag.as_deref())
                    .fetch_optional(&pool).await.ok().flatten();

                    match row {
                        Some((id,)) => id.to_string(),
                        None => {
                            let existing: Option<(uuid::Uuid,)> = sqlx::query_as(
                                "SELECT id FROM items WHERE parent_id = $1::uuid AND type = 'Season' AND index_number = $2 LIMIT 1"
                            ).bind(&series_id).bind(season_num).fetch_optional(&pool).await.ok().flatten();
                            match existing { Some((id,)) => id.to_string(), None => continue }
                        }
                    }
                };

                // Scan episodes — use season_cache instead of another read_dir
                let mut ep_groups: std::collections::BTreeMap<i32, Vec<(String, String, String)>> = std::collections::BTreeMap::new();
                for (fname, fpath) in &season_cache {
                    let ext = Path::new(fname.as_str()).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
                    if !is_video_ext(&format!(".{ext}")) { continue; }
                    let ep_info = parse_episode_info(fname);
                    let ep_num = ep_info.as_ref().and_then(|e| e.episode).unwrap_or(0);
                    ep_groups.entry(ep_num).or_default().push((fname.clone(), fpath.clone(), ext));
                }

                for (ep_num, files) in &ep_groups {
                    if files.is_empty() { continue; }
                    let (primary_name, primary_path_str, primary_ext) = &files[0];

                    // Early skip: already exists, check if we need to add more versions
                    let existing_item_id: Option<uuid::Uuid> = if existing_eps.contains(primary_path_str) {
                        sqlx::query_scalar("SELECT id FROM items WHERE file_path = $1 LIMIT 1")
                            .bind(primary_path_str)
                            .fetch_optional(&pool).await.ok().flatten()
                    } else {
                        None
                    };

                    let item_id: uuid::Uuid = if let Some(id) = existing_item_id {
                        id
                    } else if existing_eps.contains(primary_path_str) {
                        continue;
                    } else {
                        let ep_title = Path::new(primary_name.as_str()).file_stem().and_then(|s| s.to_str()).unwrap_or("Episode").to_string();
                        let mi = read_mediainfo_json_cached(primary_path_str, &season_cache);
                        let runtime: Option<i64> = mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64());

                        let row: Option<(uuid::Uuid,)> = sqlx::query_as(
                            "INSERT INTO items (library_id, parent_id, type, name, sort_name, index_number, parent_index_number, runtime_ticks, file_path, container, series_id, series_name, season_id)
                             VALUES ($1::uuid, $2::uuid, 'Episode', $3, $4, $5, $6, $7, $8, $9, $10::uuid, $11, $12::uuid)
                             ON CONFLICT DO NOTHING RETURNING id"
                        )
                        .bind(&lib_id).bind(&season_id).bind(&ep_title)
                        .bind(format!("episode {:04}", ep_num))
                        .bind(*ep_num).bind(season_num).bind(runtime)
                        .bind(primary_path_str).bind(primary_ext)
                        .bind(&series_id).bind(&final_show_name).bind(&season_id)
                        .fetch_optional(&pool).await.ok().flatten();

                        match row {
                            Some((id,)) => id,
                            None => continue,
                        }
                    };

                    // Create media_versions for ALL files of this episode
                    for (i, (fname, fpath, fext)) in files.iter().enumerate() {
                        let ver_name = Path::new(fname.as_str()).file_stem().and_then(|s| s.to_str()).unwrap_or("Unknown").to_string();
                        let mi = read_mediainfo_json_cached(fpath, &season_cache);
                        let is_primary = i == 0;
                        let resolved = if fext == "strm" { resolve_strm_path(fpath) } else { None };
                        let container = if fext == "strm" {
                            resolved.as_ref()
                                .and_then(|rp| Path::new(rp).extension().and_then(|e| e.to_str()).map(|s| s.to_string()))
                                .unwrap_or_else(|| fext.clone())
                        } else {
                            fext.clone()
                        };
                        sqlx::query(
                            "INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size)
                             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT DO NOTHING"
                        )
                        .bind(item_id).bind(&ver_name).bind(fpath).bind(&container)
                        .bind(is_primary)
                        .bind(mi.as_ref().cloned())
                        .bind(mi.as_ref().and_then(|m| m.get("RunTimeTicks")).and_then(|v| v.as_i64()))
                        .bind(mi.as_ref().and_then(|m| m.get("Bitrate")).and_then(|v| v.as_i64()))
                        .bind(mi.as_ref().and_then(|m| m.get("Size")).and_then(|v| v.as_i64()))
                        .execute(&pool).await.ok();
                    }
                }

                // Bump series updated_at once per season (not per episode)
                sqlx::query("UPDATE items SET updated_at = NOW() WHERE id = $1::uuid AND type = 'Series'")
                    .bind(&series_id).execute(&pool).await.ok();
            }

            let p = processed.fetch_add(1, Ordering::Relaxed) + 1;
            tracker.update_scan(&lib_id, p, Some(&show_name_raw));
        }));
    }

    for h in handles {
        let _ = h.await;
    }

    Ok(())
}
