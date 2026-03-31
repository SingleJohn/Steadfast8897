use sqlx::postgres::PgPoolOptions;
use sqlx::{Executor, PgPool};
use std::fs;
use std::path::Path;

use crate::config::AppConfig;

pub async fn create_pool(config: &AppConfig) -> Result<PgPool, sqlx::Error> {
    let pool = PgPoolOptions::new()
        .max_connections(config.db_pool_max)
        .min_connections(config.db_pool_min)
        .idle_timeout(std::time::Duration::from_secs(30))
        .acquire_timeout(std::time::Duration::from_secs(10))
        .connect(&config.database_url())
        .await?;

    Ok(pool)
}

pub async fn run_migrations(pool: &PgPool, migrations_dir: &Path) -> Result<(), sqlx::Error> {
    // Create migrations tracking table
    sqlx::query(
        "CREATE TABLE IF NOT EXISTS migrations (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL UNIQUE,
            applied_at TIMESTAMP NOT NULL DEFAULT NOW()
        )",
    )
    .execute(pool)
    .await?;

    // Read migration files
    let mut files: Vec<String> = fs::read_dir(migrations_dir)
        .map_err(|e| sqlx::Error::Configuration(format!("Cannot read migrations dir: {e}").into()))?
        .filter_map(|entry| {
            let entry = entry.ok()?;
            let name = entry.file_name().to_string_lossy().to_string();
            if name.ends_with(".sql") {
                Some(name)
            } else {
                None
            }
        })
        .collect();
    files.sort();

    // Get already applied migrations
    let applied: Vec<String> = sqlx::query_scalar("SELECT name FROM migrations")
        .fetch_all(pool)
        .await?;
    let applied_set: std::collections::HashSet<&str> =
        applied.iter().map(|s| s.as_str()).collect();

    for file in &files {
        if applied_set.contains(file.as_str()) {
            continue;
        }
        tracing::info!("Applying migration: {file}");
        let sql = fs::read_to_string(migrations_dir.join(file))
            .map_err(|e| sqlx::Error::Configuration(format!("Cannot read {file}: {e}").into()))?;

        let mut tx = pool.begin().await?;
        // Use raw_sql to execute multiple statements in one go
        (&mut *tx).execute(sqlx::raw_sql(&sql)).await?;
        sqlx::query("INSERT INTO migrations (name) VALUES ($1)")
            .bind(file)
            .execute(&mut *tx)
            .await?;
        tx.commit().await?;
        tracing::info!("  Applied: {file}");
    }

    tracing::info!("All migrations applied.");
    Ok(())
}
