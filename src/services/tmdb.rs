// TMDB metadata scraping service
// Uses reqwest with optional proxy (HTTP/SOCKS5) support
// Supports multiple API keys with round-robin rotation and 429 retry

use reqwest::Client;
use sqlx::{PgPool, Row};
use std::path::Path;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::Duration;

use crate::services::scanner::{apply_nfo_data, NfoData, NfoActor, generate_image_tag};

const TMDB_BASE: &str = "https://api.themoviedb.org/3";
const TMDB_IMAGE_BASE: &str = "https://image.tmdb.org/t/p";

pub struct TmdbClient {
    client: Client,
    api_keys: Vec<String>,
    key_index: AtomicUsize,
    language: String,
}

impl TmdbClient {
    pub async fn from_config(pool: &PgPool) -> Option<Self> {
        let raw_key: Option<String> = sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'tmdb_api_key'")
            .fetch_optional(pool).await.ok().flatten();
        let raw_key = raw_key.filter(|k| !k.is_empty())?;

        // Support comma-separated multiple API keys
        let api_keys: Vec<String> = raw_key.split(',')
            .map(|k| k.trim().to_string())
            .filter(|k| !k.is_empty())
            .collect();
        if api_keys.is_empty() {
            return None;
        }

        tracing::info!("[TMDB] Loaded {} API key(s)", api_keys.len());

        let language: String = sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'tmdb_language'")
            .fetch_optional(pool).await.ok().flatten().unwrap_or_else(|| "zh-CN".into());

        let proxy_url: Option<String> = sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'tmdb_proxy'")
            .fetch_optional(pool).await.ok().flatten().filter(|p: &String| !p.is_empty());

        let mut builder = Client::builder()
            .timeout(Duration::from_secs(15))
            .connect_timeout(Duration::from_secs(10));

        if let Some(ref proxy) = proxy_url {
            if let Ok(p) = reqwest::Proxy::all(proxy) {
                builder = builder.proxy(p);
            }
        }

        let client = builder.build().ok()?;

        Some(Self {
            client,
            api_keys,
            key_index: AtomicUsize::new(0),
            language,
        })
    }

    fn clone_with_language(&self, lang: &str) -> Self {
        Self {
            client: self.client.clone(),
            api_keys: self.api_keys.clone(),
            key_index: AtomicUsize::new(self.key_index.load(Ordering::Relaxed)),
            language: lang.to_string(),
        }
    }

    /// Round-robin select next API key
    fn next_key(&self) -> &str {
        let idx = self.key_index.fetch_add(1, Ordering::Relaxed);
        &self.api_keys[idx % self.api_keys.len()]
    }

    /// Send a GET request with automatic 429 retry using next API key
    async fn tmdb_get(&self, url_template: &str) -> Option<serde_json::Value> {
        let max_retries = self.api_keys.len();
        for attempt in 0..=max_retries {
            let key = self.next_key();
            let url = url_template.replace("{API_KEY}", key);

            let resp = match self.client.get(&url).send().await {
                Ok(r) => r,
                Err(e) => {
                    tracing::debug!("[TMDB] Request error: {e}");
                    return None;
                }
            };

            if resp.status() == reqwest::StatusCode::TOO_MANY_REQUESTS {
                if attempt < max_retries {
                    tracing::debug!("[TMDB] 429 rate limited on key ...{}, rotating to next key", &key[key.len().saturating_sub(6)..]);
                    tokio::time::sleep(Duration::from_millis(500)).await;
                    continue;
                }
                tracing::warn!("[TMDB] All {} keys rate limited", self.api_keys.len());
                return None;
            }

            if !resp.status().is_success() {
                tracing::debug!("[TMDB] HTTP {}", resp.status());
                return None;
            }

            return resp.json().await.ok();
        }
        None
    }

    pub async fn search_movie(&self, name: &str, year: Option<i32>) -> Option<serde_json::Value> {
        let mut url = format!("{TMDB_BASE}/search/movie?api_key={{API_KEY}}&language={}&query={}",
            self.language, urlencoding::encode(name));
        if let Some(y) = year {
            url.push_str(&format!("&year={y}"));
        }
        let json = self.tmdb_get(&url).await?;
        json.get("results")?.as_array()?.first().cloned()
    }

    pub async fn search_tv(&self, name: &str) -> Option<serde_json::Value> {
        let url = format!("{TMDB_BASE}/search/tv?api_key={{API_KEY}}&language={}&query={}",
            self.language, urlencoding::encode(name));
        let json = self.tmdb_get(&url).await?;
        json.get("results")?.as_array()?.first().cloned()
    }

    pub async fn get_movie_details(&self, tmdb_id: i64) -> Option<serde_json::Value> {
        let url = format!("{TMDB_BASE}/movie/{tmdb_id}?api_key={{API_KEY}}&language={}&append_to_response=credits",
            self.language);
        self.tmdb_get(&url).await
    }

    pub async fn get_tv_details(&self, tmdb_id: i64) -> Option<serde_json::Value> {
        let url = format!("{TMDB_BASE}/tv/{tmdb_id}?api_key={{API_KEY}}&language={}&append_to_response=credits",
            self.language);
        self.tmdb_get(&url).await
    }

    pub async fn get_season_images(&self, tmdb_id: i64, season_num: i32) -> Option<String> {
        let url = format!("{TMDB_BASE}/tv/{tmdb_id}/season/{season_num}?api_key={{API_KEY}}&language={}", self.language);
        let json = self.tmdb_get(&url).await?;
        json.get("poster_path").and_then(|p| p.as_str()).map(|s| s.to_string())
    }

    pub async fn download_image(&self, img_path: &str, save_path: &str, size: &str) -> bool {
        let url = format!("{TMDB_IMAGE_BASE}/{size}{img_path}");
        let resp = match self.client.get(&url).send().await {
            Ok(r) => r,
            Err(_) => return false,
        };
        let bytes = match resp.bytes().await {
            Ok(b) => b,
            Err(_) => return false,
        };
        if let Some(parent) = Path::new(save_path).parent() {
            std::fs::create_dir_all(parent).ok();
        }
        std::fs::write(save_path, &bytes).is_ok()
    }
}

// Scrape a single item
pub async fn scrape_item(pool: &PgPool, item_id: &str) -> Result<serde_json::Value, String> {
    let client = TmdbClient::from_config(pool).await
        .ok_or("TMDB API key not configured")?;
    scrape_item_with_client(pool, item_id, &client).await
}

pub async fn scrape_item_with_client(pool: &PgPool, item_id: &str, client: &TmdbClient) -> Result<serde_json::Value, String> {

    let row = sqlx::query("SELECT * FROM items WHERE id = $1::uuid")
        .bind(item_id)
        .fetch_optional(pool)
        .await
        .map_err(|e| e.to_string())?
        .ok_or("Item not found")?;

    let item_type: String = row.try_get("type").unwrap_or_default();
    let name: String = row.try_get("name").unwrap_or_default();
    let year: Option<i32> = row.try_get("production_year").ok();

    let (details, tmdb_id) = if item_type == "Movie" {
        let search = client.search_movie(&name, year).await
            .ok_or("Movie not found on TMDB")?;
        let tid = search.get("id").and_then(|v| v.as_i64()).ok_or("No TMDB ID")?;
        let details = client.get_movie_details(tid).await
            .ok_or("Failed to get movie details")?;
        (details, tid)
    } else if item_type == "Series" {
        let search = client.search_tv(&name).await
            .ok_or("TV show not found on TMDB")?;
        let tid = search.get("id").and_then(|v| v.as_i64()).ok_or("No TMDB ID")?;
        let details = client.get_tv_details(tid).await
            .ok_or("Failed to get TV details")?;
        (details, tid)
    } else {
        return Err(format!("Cannot scrape type: {item_type}"));
    };

    // Extract metadata — fallback chain: zh-CN → en-US → Douban
    let mut overview = details.get("overview").and_then(|v| v.as_str())
        .filter(|s| !s.is_empty()).map(|s| s.to_string());
    if overview.is_none() && client.language != "en-US" {
        let en_client = client.clone_with_language("en-US");
        let en_details = if item_type == "Movie" {
            en_client.get_movie_details(tmdb_id).await
        } else {
            en_client.get_tv_details(tmdb_id).await
        };
        if let Some(en) = en_details {
            overview = en.get("overview").and_then(|v| v.as_str())
                .filter(|s| !s.is_empty()).map(|s| s.to_string());
        }
    }
    // Douban fallback for Chinese overview
    if overview.is_none() {
        overview = fetch_douban_overview(&client.client, &name).await;
    }
    let rating = details.get("vote_average").and_then(|v| v.as_f64());
    let genres: Vec<String> = details.get("genres")
        .and_then(|g| g.as_array())
        .map(|arr| arr.iter().filter_map(|g| g.get("name").and_then(|n| n.as_str()).map(|s| s.to_string())).collect())
        .unwrap_or_default();

    let actors: Vec<NfoActor> = details.get("credits")
        .and_then(|c| c.get("cast"))
        .and_then(|a| a.as_array())
        .map(|arr| arr.iter().take(20).filter_map(|a| {
            Some(NfoActor {
                name: a.get("name")?.as_str()?.to_string(),
                role: a.get("character").and_then(|c| c.as_str()).unwrap_or("").to_string(),
                tmdbid: a.get("id").and_then(|i| i.as_i64()).map(|i| i as i32),
                image_url: a.get("profile_path").and_then(|p| p.as_str())
                    .map(|p| format!("{TMDB_IMAGE_BASE}/w185{p}")),
            })
        }).collect())
        .unwrap_or_default();

    let directors: Vec<String> = details.get("credits")
        .and_then(|c| c.get("crew"))
        .and_then(|a| a.as_array())
        .map(|arr| arr.iter().filter(|c| c.get("job").and_then(|j| j.as_str()) == Some("Director"))
            .filter_map(|d| d.get("name").and_then(|n| n.as_str()).map(|s| s.to_string())).collect())
        .unwrap_or_default();

    let nfo = NfoData {
        title: details.get(if item_type == "Movie" { "title" } else { "name" })
            .and_then(|v| v.as_str()).map(|s| s.to_string()),
        plot: overview.clone(),
        year: if item_type == "Movie" {
            details.get("release_date").and_then(|d| d.as_str()).and_then(|d| d[..4].parse().ok())
        } else {
            details.get("first_air_date").and_then(|d| d.as_str()).and_then(|d| d.get(..4)).and_then(|y| y.parse().ok())
        },
        rating,
        tmdbid: Some(tmdb_id as i32),
        genres,
        actors,
        directors,
        premiered: details.get(if item_type == "Movie" { "release_date" } else { "first_air_date" })
            .and_then(|d| d.as_str()).map(|s| s.to_string()),
        ..Default::default()
    };

    apply_nfo_data(pool, item_id, &nfo).await.map_err(|e| e.to_string())?;

    // Update tmdb_id
    sqlx::query("UPDATE items SET tmdb_id = $1, updated_at = NOW() WHERE id = $2::uuid")
        .bind(tmdb_id as i32).bind(item_id)
        .execute(pool).await.map_err(|e| e.to_string())?;

    // Download poster
    if let Some(poster_path) = details.get("poster_path").and_then(|p| p.as_str()) {
        let save_dir = format!("data/metadata/{item_id}");
        let save_path = format!("{save_dir}/poster.jpg");
        if client.download_image(poster_path, &save_path, "w500").await {
            let tag = generate_image_tag(&save_path);
            sqlx::query("UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid")
                .bind(&save_path).bind(tag).bind(item_id)
                .execute(pool).await.map_err(|e| e.to_string())?;
        }
    }

    // Download backdrop
    if let Some(backdrop_path) = details.get("backdrop_path").and_then(|p| p.as_str()) {
        let save_dir = format!("data/metadata/{item_id}");
        let save_path = format!("{save_dir}/backdrop.jpg");
        if client.download_image(backdrop_path, &save_path, "w1280").await {
            let tag = generate_image_tag(&save_path);
            sqlx::query("UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid")
                .bind(&save_path).bind(tag).bind(item_id)
                .execute(pool).await.map_err(|e| e.to_string())?;
        }
    }

    // Scrape season posters for Series
    if item_type == "Series" {
        scrape_season_posters(pool, &client, item_id, tmdb_id).await;
    }

    Ok(serde_json::json!({
        "success": true,
        "tmdb_id": tmdb_id,
        "name": nfo.title,
    }))
}

async fn scrape_season_posters(pool: &PgPool, client: &TmdbClient, series_id: &str, tmdb_id: i64) {
    // Get all seasons for this series
    let seasons: Vec<(uuid::Uuid, Option<i32>)> = sqlx::query_as(
        "SELECT id, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number"
    ).bind(series_id).fetch_all(pool).await.unwrap_or_default();

    for (season_id, season_num) in &seasons {
        let num = season_num.unwrap_or(1);
        // Skip if already has an image
        let existing: Option<String> = sqlx::query_scalar(
            "SELECT primary_image_tag FROM items WHERE id = $1"
        ).bind(season_id).fetch_optional(pool).await.ok().flatten();
        if existing.is_some() { continue; }

        if let Some(poster_path) = client.get_season_images(tmdb_id, num).await {
            let sid = season_id.to_string();
            let save_dir = format!("data/metadata/{sid}");
            let save_path = format!("{save_dir}/poster.jpg");
            if client.download_image(&poster_path, &save_path, "w500").await {
                let tag = generate_image_tag(&save_path);
                let _ = sqlx::query("UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3")
                    .bind(&save_path).bind(tag).bind(season_id)
                    .execute(pool).await;
            }
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }
}

// ========== Douban Fallback ==========

async fn fetch_douban_overview(client: &Client, name: &str) -> Option<String> {
    let url = format!("https://movie.douban.com/j/subject_suggest?q={}", urlencoding::encode(name));
    let resp = client.get(&url)
        .header("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
        .header("Referer", "https://movie.douban.com/")
        .send().await.ok()?;
    let results: Vec<serde_json::Value> = resp.json().await.ok()?;
    let subject_id = results.first()?.get("id")?.as_str()?;

    // Fetch subject detail page API
    let detail_url = format!("https://movie.douban.com/j/subject_abstract?subject_id={subject_id}");
    let detail_resp = client.get(&detail_url)
        .header("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
        .header("Referer", "https://movie.douban.com/")
        .send().await.ok()?;
    let detail: serde_json::Value = detail_resp.json().await.ok()?;
    let desc = detail.get("subject")?.get("short_description")?.as_str()?;
    if desc.is_empty() { return None; }
    tracing::debug!("[Douban] Got overview for '{name}' from Douban");
    Some(desc.to_string())
}

// ========== Scrape Task with Progress ==========

#[derive(Debug, Clone, serde::Serialize)]
pub struct ScrapeProgress {
    pub status: String,
    pub total_items: i64,
    pub processed_items: i64,
    pub success_items: i64,
    pub failed_items: i64,
    pub current_item: Option<String>,
    pub last_error: Option<String>,
    pub percentage: i32,
}

#[derive(Clone)]
pub struct ScrapeTask {
    progress: std::sync::Arc<tokio::sync::Mutex<ScrapeProgress>>,
    stop_flag: std::sync::Arc<std::sync::atomic::AtomicBool>,
}

impl ScrapeTask {
    pub fn new() -> Self {
        Self {
            progress: std::sync::Arc::new(tokio::sync::Mutex::new(ScrapeProgress {
                status: "idle".into(),
                total_items: 0, processed_items: 0, success_items: 0, failed_items: 0,
                current_item: None, last_error: None, percentage: 0,
            })),
            stop_flag: std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false)),
        }
    }

    pub async fn get_progress(&self) -> ScrapeProgress {
        self.progress.lock().await.clone()
    }

    pub async fn stop(&self) {
        let mut p = self.progress.lock().await;
        if p.status == "running" {
            self.stop_flag.store(true, std::sync::atomic::Ordering::SeqCst);
            p.status = "stopping".into();
        }
    }

    pub async fn start(&self, pool: sqlx::PgPool) -> Result<(), String> {
        {
            let p = self.progress.lock().await;
            if p.status == "running" || p.status == "stopping" {
                return Err("Already running".into());
            }
        }

        let rows: Vec<(uuid::Uuid, String, String, Option<i32>)> = sqlx::query_as(
            "SELECT id, type, name, production_year FROM items WHERE (overview IS NULL OR overview = '') AND type IN ('Movie', 'Series') ORDER BY created_at DESC"
        ).fetch_all(&pool).await.map_err(|e| e.to_string())?;

        let total = rows.len() as i64;
        {
            let mut p = self.progress.lock().await;
            *p = ScrapeProgress {
                status: "running".into(),
                total_items: total, processed_items: 0, success_items: 0, failed_items: 0,
                current_item: None, last_error: None, percentage: 0,
            };
        }
        self.stop_flag.store(false, std::sync::atomic::Ordering::SeqCst);

        let progress = self.progress.clone();
        let stop_flag = self.stop_flag.clone();

        tokio::spawn(async move {
            let client = match TmdbClient::from_config(&pool).await {
                Some(c) => c,
                None => {
                    let mut p = progress.lock().await;
                    p.status = "error".into();
                    p.last_error = Some("TMDB API key not configured".into());
                    return;
                }
            };

            for (id, _item_type, name, _year) in &rows {
                if stop_flag.load(std::sync::atomic::Ordering::SeqCst) {
                    let mut p = progress.lock().await;
                    p.status = "stopped".into();
                    p.current_item = None;
                    tracing::info!("[Scrape] Stopped by user");
                    return;
                }

                {
                    let mut p = progress.lock().await;
                    p.current_item = Some(name.clone());
                }

                match scrape_item_with_client(&pool, &id.to_string(), &client).await {
                    Ok(_) => {
                        let mut p = progress.lock().await;
                        p.success_items += 1;
                    }
                    Err(e) => {
                        let mut p = progress.lock().await;
                        p.failed_items += 1;
                        p.last_error = Some(format!("{name}: {e}"));
                        tracing::debug!("[Scrape] Failed {name}: {e}");
                    }
                }

                {
                    let mut p = progress.lock().await;
                    p.processed_items += 1;
                    p.percentage = if p.total_items > 0 { (p.processed_items * 100 / p.total_items) as i32 } else { 0 };
                }

                tokio::time::sleep(Duration::from_millis(300)).await;
            }

            let mut p = progress.lock().await;
            tracing::info!("[Scrape] Done: {}/{} success, {} failed", p.success_items, p.total_items, p.failed_items);
            p.status = "completed".into();
            p.current_item = None;
        });

        Ok(())
    }
}
