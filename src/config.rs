use std::fs;
use std::path::{Path, PathBuf};
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct AppConfig {
    pub port: u16,
    pub frontend_port: u16,
    pub server_name: String,
    pub server_id: String,
    pub version: String,

    pub db_host: String,
    pub db_port: u16,
    pub db_name: String,
    pub db_user: String,
    pub db_password: String,
    pub db_pool_max: u32,
    pub db_pool_min: u32,

    pub redis_host: String,
    pub redis_port: u16,
    pub redis_password: Option<String>,

    pub media_movies_path: String,
    pub media_tvshows_path: String,

    pub data_dir: PathBuf,
    pub cache_dir: PathBuf,
}

impl AppConfig {
    pub fn from_env() -> Self {
        dotenvy::dotenv().ok();

        let data_dir = PathBuf::from(
            std::env::var("DATA_DIR").unwrap_or_else(|_| "data".to_string()),
        );
        fs::create_dir_all(&data_dir).ok();

        let cache_dir = data_dir.join("cache");
        fs::create_dir_all(&cache_dir).ok();

        let server_id = load_or_create_server_id(&data_dir);

        Self {
            port: env_or("PORT", "8961").parse().unwrap_or(8961),
            frontend_port: env_or("FRONTEND_PORT", "3001").parse().unwrap_or(3001),
            server_name: env_or("SERVER_NAME", "FYMS"),
            server_id,
            version: "4.7.0.0".to_string(),

            db_host: env_or("DB_HOST", "localhost"),
            db_port: env_or("DB_PORT", "5432").parse().unwrap_or(5432),
            db_name: env_or("DB_NAME", "media_server"),
            db_user: env_or("DB_USER", "postgres"),
            db_password: env_or("DB_PASSWORD", "postgres"),
            db_pool_max: env_or("DB_POOL_MAX", "400").parse().unwrap_or(400),
            db_pool_min: env_or("DB_POOL_MIN", "20").parse().unwrap_or(20),

            redis_host: env_or("REDIS_HOST", "127.0.0.1"),
            redis_port: env_or("REDIS_PORT", "6379").parse().unwrap_or(6379),
            redis_password: std::env::var("REDIS_PASSWORD").ok().filter(|s| !s.is_empty()),

            media_movies_path: env_or("MEDIA_MOVIES_PATH", ""),
            media_tvshows_path: env_or("MEDIA_TVSHOWS_PATH", ""),

            data_dir,
            cache_dir,
        }
    }

    pub fn database_url(&self) -> String {
        format!(
            "postgres://{}:{}@{}:{}/{}",
            self.db_user, self.db_password, self.db_host, self.db_port, self.db_name
        )
    }
}

fn env_or(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

fn load_or_create_server_id(data_dir: &Path) -> String {
    let id_path = data_dir.join("server-id");
    if let Ok(id) = fs::read_to_string(&id_path) {
        let id = id.trim().to_string();
        if !id.is_empty() {
            return id;
        }
    }
    let id = Uuid::new_v4().to_string().replace('-', "");
    fs::write(&id_path, &id).ok();
    id
}
