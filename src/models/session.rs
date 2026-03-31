use chrono::NaiveDateTime;
use sqlx::PgPool;
use uuid::Uuid;

use crate::error::AppResult;

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct AccessToken {
    pub token: String,
    pub user_id: Uuid,
    pub device_id: String,
    pub device_name: String,
    pub app_name: String,
    pub app_version: String,
    pub created_at: NaiveDateTime,
}

pub async fn create_access_token(
    pool: &PgPool,
    user_id: &Uuid,
    device_id: &str,
    device_name: &str,
    app_name: &str,
    app_version: &str,
) -> AppResult<String> {
    let token = Uuid::new_v4().to_string().replace('-', "");
    sqlx::query(
        "INSERT INTO access_tokens (token, user_id, device_id, device_name, app_name, app_version) VALUES ($1, $2, $3, $4, $5, $6)",
    )
    .bind(&token)
    .bind(user_id)
    .bind(device_id)
    .bind(device_name)
    .bind(app_name)
    .bind(app_version)
    .execute(pool)
    .await?;
    Ok(token)
}

pub async fn find_by_token(pool: &PgPool, token: &str) -> AppResult<Option<AccessToken>> {
    let row = sqlx::query_as::<_, AccessToken>("SELECT * FROM access_tokens WHERE token = $1")
        .bind(token)
        .fetch_optional(pool)
        .await?;
    Ok(row)
}

pub async fn delete_token(pool: &PgPool, token: &str) -> AppResult<()> {
    sqlx::query("DELETE FROM access_tokens WHERE token = $1")
        .bind(token)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn delete_user_tokens(pool: &PgPool, user_id: &Uuid) -> AppResult<()> {
    sqlx::query("DELETE FROM access_tokens WHERE user_id = $1")
        .bind(user_id)
        .execute(pool)
        .await?;
    Ok(())
}
