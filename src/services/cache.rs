use moka::future::Cache;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;

#[derive(Clone)]
pub struct CacheService {
    mem_cache: Cache<String, String>,
    redis: Arc<RwLock<Option<redis::aio::ConnectionManager>>>,
}

impl CacheService {
    pub async fn new(redis_host: &str, redis_port: u16, redis_password: Option<&str>) -> Self {
        let mem_cache = Cache::builder()
            .max_capacity(5000)
            .time_to_live(Duration::from_secs(600))
            .build();

        let redis_conn = Self::try_connect_redis(redis_host, redis_port, redis_password).await;

        Self {
            mem_cache,
            redis: Arc::new(RwLock::new(redis_conn)),
        }
    }

    async fn try_connect_redis(host: &str, port: u16, password: Option<&str>) -> Option<redis::aio::ConnectionManager> {
        let url = match password {
            Some(pw) => format!("redis://:{pw}@{host}:{port}"),
            None => format!("redis://{host}:{port}"),
        };
        match redis::Client::open(url) {
            Ok(client) => match client.get_connection_manager().await {
                Ok(mgr) => {
                    tracing::info!("Redis cache connected");
                    Some(mgr)
                }
                Err(e) => {
                    tracing::warn!("Redis unavailable: {e}, using in-memory cache");
                    None
                }
            },
            Err(e) => {
                tracing::warn!("Redis unavailable: {e}, using in-memory cache");
                None
            }
        }
    }

    pub async fn get(&self, key: &str) -> Option<String> {
        // Try Redis first
        {
            let guard = self.redis.read().await;
            if let Some(ref mgr) = *guard {
                let mut conn = mgr.clone();
                if let Ok(val) = redis::cmd("GET")
                    .arg(key)
                    .query_async::<Option<String>>(&mut conn)
                    .await
                {
                    if val.is_some() {
                        return val;
                    }
                }
            }
        }
        // Fallback to memory
        self.mem_cache.get(key).await
    }

    pub async fn set(&self, key: &str, value: &str, ttl_seconds: u64) {
        // Try Redis
        {
            let guard = self.redis.read().await;
            if let Some(ref mgr) = *guard {
                let mut conn = mgr.clone();
                let _: Result<(), _> = redis::cmd("SETEX")
                    .arg(key)
                    .arg(ttl_seconds)
                    .arg(value)
                    .query_async(&mut conn)
                    .await;
            }
        }
        // Always write to memory cache too
        self.mem_cache
            .insert(key.to_string(), value.to_string())
            .await;
    }

    pub async fn del(&self, key: &str) {
        {
            let guard = self.redis.read().await;
            if let Some(ref mgr) = *guard {
                let mut conn = mgr.clone();
                let _: Result<(), _> = redis::cmd("DEL")
                    .arg(key)
                    .query_async(&mut conn)
                    .await;
            }
        }
        self.mem_cache.invalidate(key).await;
    }

    pub async fn del_pattern(&self, pattern: &str) {
        // Redis: KEYS + DEL
        {
            let guard = self.redis.read().await;
            if let Some(ref mgr) = *guard {
                let mut conn = mgr.clone();
                if let Ok(keys) = redis::cmd("KEYS")
                    .arg(pattern)
                    .query_async::<Vec<String>>(&mut conn)
                    .await
                {
                    for key in &keys {
                        let _: Result<(), _> = redis::cmd("DEL")
                            .arg(key)
                            .query_async(&mut conn)
                            .await;
                    }
                }
            }
        }
        // Memory: prefix match
        let prefix = pattern.replace('*', "");
        let _ = self.mem_cache.invalidate_entries_if(move |k: &String, _v| {
            k.starts_with(&prefix)
        });
    }

    pub async fn get_json<T: serde::de::DeserializeOwned>(&self, key: &str) -> Option<T> {
        let raw = self.get(key).await?;
        serde_json::from_str(&raw).ok()
    }

    pub async fn set_json<T: serde::Serialize>(&self, key: &str, value: &T, ttl_seconds: u64) {
        if let Ok(json) = serde_json::to_string(value) {
            self.set(key, &json, ttl_seconds).await;
        }
    }
}
