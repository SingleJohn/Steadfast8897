use chrono::NaiveDateTime;
use sqlx::PgPool;
use uuid::Uuid;

use crate::error::AppResult;

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct Library {
    pub id: Uuid,
    pub name: String,
    pub collection_type: String,
    pub paths: Vec<String>,
    pub created_at: NaiveDateTime,
    pub primary_image_path: Option<String>,
    pub primary_image_tag: Option<String>,
}

pub async fn get_all_libraries(pool: &PgPool) -> AppResult<Vec<Library>> {
    let libs = sqlx::query_as::<_, Library>("SELECT * FROM libraries ORDER BY name")
        .fetch_all(pool)
        .await?;
    Ok(libs)
}

pub async fn get_library_by_id(pool: &PgPool, id: &Uuid) -> AppResult<Option<Library>> {
    let lib = sqlx::query_as::<_, Library>("SELECT * FROM libraries WHERE id = $1")
        .bind(id)
        .fetch_optional(pool)
        .await?;
    Ok(lib)
}

pub async fn create_library(
    pool: &PgPool,
    name: &str,
    collection_type: &str,
    paths: &[String],
) -> AppResult<Library> {
    let lib = sqlx::query_as::<_, Library>(
        "INSERT INTO libraries (name, collection_type, paths) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(name)
    .bind(collection_type)
    .bind(paths)
    .fetch_one(pool)
    .await?;
    Ok(lib)
}

pub async fn update_library(
    pool: &PgPool,
    id: &Uuid,
    name: Option<&str>,
) -> AppResult<Option<Library>> {
    if let Some(n) = name {
        sqlx::query("UPDATE libraries SET name = $1 WHERE id = $2")
            .bind(n)
            .bind(id)
            .execute(pool)
            .await?;
    }
    get_library_by_id(pool, id).await
}

pub async fn delete_library(pool: &PgPool, id: &Uuid) -> AppResult<()> {
    // Cascade: delete items first
    sqlx::query("DELETE FROM items WHERE library_id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    sqlx::query("DELETE FROM libraries WHERE id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn add_library_path(pool: &PgPool, id: &Uuid, path: &str) -> AppResult<()> {
    sqlx::query("UPDATE libraries SET paths = array_append(paths, $1) WHERE id = $2")
        .bind(path)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn update_library_image(pool: &PgPool, id: &Uuid, image_path: &str, image_tag: &str) -> AppResult<()> {
    sqlx::query("UPDATE libraries SET primary_image_path = $1, primary_image_tag = $2 WHERE id = $3")
        .bind(image_path)
        .bind(image_tag)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn delete_library_image(pool: &PgPool, id: &Uuid) -> AppResult<()> {
    sqlx::query("UPDATE libraries SET primary_image_path = NULL, primary_image_tag = NULL WHERE id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn remove_library_path(pool: &PgPool, id: &Uuid, path: &str) -> AppResult<()> {
    sqlx::query("UPDATE libraries SET paths = array_remove(paths, $1) WHERE id = $2")
        .bind(path)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}
