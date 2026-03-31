use chrono::{DateTime, Utc};
use dashmap::DashMap;
use serde::Serialize;
use std::sync::Arc;

#[derive(Debug, Clone, Serialize)]
pub struct ActiveSession {
    pub user_id: String,
    pub user_name: String,
    pub device_id: String,
    pub device_name: String,
    pub app_name: String,
    pub app_version: String,
    pub client_ip: String,
    pub last_activity: DateTime<Utc>,
    pub now_playing: Option<NowPlaying>,
}

#[derive(Debug, Clone, Serialize)]
pub struct NowPlaying {
    pub item_id: String,
    pub item_name: String,
    pub item_type: String,
    pub series_name: Option<String>,
    pub runtime_ticks: Option<i64>,
    pub position_ticks: i64,
    pub is_paused: bool,
    pub season_index: Option<i32>,
    pub episode_index: Option<i32>,
    pub primary_image_item_id: Option<String>,
}

#[derive(Clone)]
pub struct SessionManager {
    sessions: Arc<DashMap<String, ActiveSession>>,
}

impl SessionManager {
    pub fn new() -> Self {
        let mgr = Self {
            sessions: Arc::new(DashMap::new()),
        };
        // Spawn cleanup task
        let sessions = mgr.sessions.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(std::time::Duration::from_secs(30));
            loop {
                interval.tick().await;
                let cutoff = Utc::now() - chrono::Duration::minutes(10);
                sessions.retain(|_, v| v.last_activity > cutoff);
            }
        });
        mgr
    }

    pub fn update_session(
        &self,
        user_id: &str,
        user_name: &str,
        device_id: &str,
        device_name: &str,
        app_name: &str,
        app_version: &str,
        client_ip: &str,
    ) {
        let key = format!("{user_id}:{device_id}");
        self.sessions
            .entry(key)
            .and_modify(|s| {
                s.user_name = user_name.to_string();
                s.device_name = device_name.to_string();
                s.app_name = app_name.to_string();
                if !app_version.is_empty() { s.app_version = app_version.to_string(); }
                if !client_ip.is_empty() { s.client_ip = client_ip.to_string(); }
                s.last_activity = Utc::now();
            })
            .or_insert_with(|| ActiveSession {
                user_id: user_id.to_string(),
                user_name: user_name.to_string(),
                device_id: device_id.to_string(),
                device_name: device_name.to_string(),
                app_name: app_name.to_string(),
                app_version: app_version.to_string(),
                client_ip: client_ip.to_string(),
                last_activity: Utc::now(),
                now_playing: None,
            });
    }

    pub fn set_now_playing(
        &self,
        user_id: &str,
        device_id: &str,
        item_id: &str,
        item_name: &str,
        item_type: &str,
        series_name: Option<&str>,
        runtime_ticks: Option<i64>,
        position_ticks: i64,
        is_paused: bool,
        season_index: Option<i32>,
        episode_index: Option<i32>,
        primary_image_item_id: Option<&str>,
    ) {
        let key = format!("{user_id}:{device_id}");
        if let Some(mut session) = self.sessions.get_mut(&key) {
            session.now_playing = Some(NowPlaying {
                item_id: item_id.to_string(),
                item_name: item_name.to_string(),
                item_type: item_type.to_string(),
                series_name: series_name.map(|s| s.to_string()),
                runtime_ticks,
                position_ticks,
                is_paused,
                season_index,
                episode_index,
                primary_image_item_id: primary_image_item_id.map(|s| s.to_string()),
            });
            session.last_activity = Utc::now();
        }
    }

    pub fn clear_now_playing(&self, user_id: &str, device_id: &str) {
        let key = format!("{user_id}:{device_id}");
        if let Some(mut session) = self.sessions.get_mut(&key) {
            session.now_playing = None;
        }
    }

    pub fn get_active_sessions(&self) -> Vec<ActiveSession> {
        let cutoff = Utc::now() - chrono::Duration::minutes(10);
        self.sessions
            .iter()
            .filter(|entry| entry.value().last_activity > cutoff)
            .map(|entry| entry.value().clone())
            .collect()
    }
}
