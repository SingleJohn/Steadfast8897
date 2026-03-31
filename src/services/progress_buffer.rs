use dashmap::DashMap;
use sqlx::PgPool;
use std::sync::Arc;
use tokio::sync::Notify;

#[derive(Debug, Clone)]
pub struct ProgressEntry {
    pub user_id: String,
    pub item_id: String,
    pub position_ticks: i64,
    pub play_count: Option<i32>,
    pub is_favorite: Option<bool>,
    pub played: Option<bool>,
}

#[derive(Clone)]
pub struct ProgressBuffer {
    buffer: Arc<DashMap<String, ProgressEntry>>,
    shutdown: Arc<Notify>,
}

impl ProgressBuffer {
    pub fn new(pool: PgPool) -> Self {
        let buffer = Arc::new(DashMap::new());
        let shutdown = Arc::new(Notify::new());

        let pb = Self {
            buffer: buffer.clone(),
            shutdown: shutdown.clone(),
        };

        // Spawn flusher task
        tokio::spawn(Self::flusher(buffer, pool, shutdown));

        pb
    }

    async fn flusher(buffer: Arc<DashMap<String, ProgressEntry>>, pool: PgPool, shutdown: Arc<Notify>) {
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(3));
        loop {
            tokio::select! {
                _ = interval.tick() => {
                    Self::flush_once(&buffer, &pool).await;
                }
                _ = shutdown.notified() => {
                    Self::flush_once(&buffer, &pool).await;
                    break;
                }
            }
        }
    }

    async fn flush_once(buffer: &DashMap<String, ProgressEntry>, pool: &PgPool) {
        // Drain all entries
        let entries: Vec<ProgressEntry> = {
            let keys: Vec<String> = buffer.iter().map(|e| e.key().clone()).collect();
            let mut out = Vec::new();
            for key in keys {
                if let Some((_, entry)) = buffer.remove(&key) {
                    out.push(entry);
                }
            }
            out
        };

        if entries.is_empty() {
            return;
        }

        // Batch upsert in chunks of 50
        for chunk in entries.chunks(50) {
            for entry in chunk {
                let result = sqlx::query(
                    "INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
                     VALUES ($1::uuid, $2::uuid, $3, COALESCE($4, 0), COALESCE($5, false), COALESCE($6, false), NOW())
                     ON CONFLICT (user_id, item_id) DO UPDATE SET
                       playback_position_ticks = $3,
                       play_count = CASE WHEN $4 IS NOT NULL THEN $4 ELSE user_item_data.play_count END,
                       is_favorite = CASE WHEN $5 IS NOT NULL THEN $5 ELSE user_item_data.is_favorite END,
                       played = CASE WHEN $6 IS NOT NULL THEN $6 ELSE user_item_data.played END,
                       last_played_date = NOW()"
                )
                .bind(&entry.user_id)
                .bind(&entry.item_id)
                .bind(entry.position_ticks)
                .bind(entry.play_count)
                .bind(entry.is_favorite)
                .bind(entry.played)
                .execute(pool)
                .await;

                if let Err(e) = result {
                    tracing::error!("Progress flush error: {e}");
                }
            }
        }
    }

    pub fn buffer_progress(&self, entry: ProgressEntry) {
        let key = format!("{}:{}", entry.user_id, entry.item_id);
        self.buffer
            .entry(key)
            .and_modify(|existing| {
                existing.position_ticks = entry.position_ticks;
                if entry.play_count.is_some() {
                    existing.play_count = entry.play_count;
                }
                if entry.is_favorite.is_some() {
                    existing.is_favorite = entry.is_favorite;
                }
                if entry.played.is_some() {
                    existing.played = entry.played;
                }
            })
            .or_insert(entry);
    }

    pub fn shutdown(&self) {
        self.shutdown.notify_one();
    }
}
