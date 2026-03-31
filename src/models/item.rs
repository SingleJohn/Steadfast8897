use chrono::NaiveDateTime;
use serde::Serialize;
use sqlx::{PgPool, Row, postgres::PgRow};
use uuid::Uuid;
use std::collections::HashMap;

use crate::error::AppResult;
use crate::services::cache::CacheService;

// --- Query builder types ---

#[derive(Debug, Default)]
pub struct ItemQueryOptions {
    pub parent_id: Option<String>,
    pub include_item_types: Option<Vec<String>>,
    pub sort_by: Option<String>,
    pub sort_order: Option<String>,
    pub limit: Option<i64>,
    pub start_index: Option<i64>,
    pub recursive: bool,
    pub library_id: Option<String>,
    pub search_term: Option<String>,
    pub filters: Option<Vec<String>>,
    pub user_id: Option<String>,
    pub genre_ids: Option<Vec<String>>,
    pub years: Option<Vec<i32>>,
}

pub struct QueryResult {
    pub items: Vec<PgRow>,
    pub total_count: i64,
}

pub async fn get_item_by_id(
    pool: &PgPool,
    cache: &CacheService,
    id: &str,
) -> AppResult<Option<PgRow>> {
    let row = sqlx::query("SELECT * FROM items WHERE id = $1::uuid")
        .bind(id)
        .fetch_optional(pool)
        .await?;
    Ok(row)
}

/// Resolve any ID format (UUID string or integer emby_id) to a database row.
/// Tries UUID first, falls back to emby_id integer lookup.
pub async fn get_item_by_any_id(
    pool: &PgPool,
    id: &str,
) -> AppResult<Option<PgRow>> {
    // Try UUID first
    if Uuid::parse_str(id).is_ok() {
        return Ok(sqlx::query("SELECT * FROM items WHERE id = $1::uuid")
            .bind(id)
            .fetch_optional(pool)
            .await?);
    }
    // Try integer emby_id
    if let Ok(emby_id) = id.parse::<i32>() {
        return Ok(sqlx::query("SELECT * FROM items WHERE emby_id = $1")
            .bind(emby_id)
            .fetch_optional(pool)
            .await?);
    }
    Ok(None)
}

/// Resolve an ID string to UUID string. Returns the original if already UUID,
/// or looks up emby_id and returns the UUID.
pub async fn resolve_to_uuid(pool: &PgPool, id: &str) -> AppResult<Option<String>> {
    if Uuid::parse_str(id).is_ok() {
        return Ok(Some(id.to_string()));
    }
    if let Ok(emby_id) = id.parse::<i32>() {
        let row: Option<(Uuid,)> = sqlx::query_as("SELECT id FROM items WHERE emby_id = $1")
            .bind(emby_id)
            .fetch_optional(pool)
            .await?;
        return Ok(row.map(|(id,)| id.to_string()));
    }
    Ok(None)
}

/// Get emby_id for a UUID
pub async fn get_emby_id(pool: &PgPool, uuid: &str) -> Option<i32> {
    sqlx::query_scalar::<_, i32>("SELECT emby_id FROM items WHERE id = $1::uuid")
        .bind(uuid)
        .fetch_optional(pool)
        .await
        .ok()
        .flatten()
}

pub async fn query_items(pool: &PgPool, options: &ItemQueryOptions) -> AppResult<QueryResult> {
    let mut conditions: Vec<String> = Vec::new();
    let mut params: Vec<String> = Vec::new();
    let mut param_index: usize = 1;

    // For recursive queries with parentId, treat as libraryId
    if let Some(ref pid) = options.parent_id {
        if options.recursive {
            conditions.push(format!("i.library_id = ${param_index}::uuid"));
        } else {
            conditions.push(format!("i.parent_id = ${param_index}::uuid"));
        }
        params.push(pid.clone());
        param_index += 1;
    }

    if let Some(ref lid) = options.library_id {
        conditions.push(format!("i.library_id = ${param_index}::uuid"));
        params.push(lid.clone());
        param_index += 1;
    }

    if let Some(ref types) = options.include_item_types {
        if !types.is_empty() {
            let placeholders: Vec<String> = types
                .iter()
                .map(|_| {
                    let p = format!("${param_index}");
                    param_index += 1;
                    p
                })
                .collect();
            conditions.push(format!("i.type IN ({})", placeholders.join(",")));
            params.extend(types.clone());
        }
    }

    if let Some(ref term) = options.search_term {
        conditions.push(format!("i.name ILIKE ${param_index}"));
        params.push(format!("%{term}%"));
        param_index += 1;
    }

    // Genre filter
    let mut genre_join = String::new();
    if let Some(ref gids) = options.genre_ids {
        if !gids.is_empty() {
            genre_join = "JOIN item_genres ig_filter ON i.id = ig_filter.item_id".to_string();
            let placeholders: Vec<String> = gids
                .iter()
                .map(|_| {
                    let p = format!("${param_index}");
                    param_index += 1;
                    p
                })
                .collect();
            conditions.push(format!(
                "ig_filter.genre_id IN ({})",
                placeholders.join(",")
            ));
            params.extend(gids.clone());
        }
    }

    // Year filter
    if let Some(ref yrs) = options.years {
        if !yrs.is_empty() {
            let placeholders: Vec<String> = yrs
                .iter()
                .map(|_| {
                    let p = format!("${param_index}");
                    param_index += 1;
                    p
                })
                .collect();
            let year_placeholders: Vec<String> = placeholders.iter().map(|p| format!("{}::int", p)).collect();
            conditions.push(format!(
                "i.production_year IN ({})",
                year_placeholders.join(",")
            ));
            for y in yrs {
                params.push(y.to_string());
            }
        }
    }

    // User data JOIN
    let mut user_join = String::new();
    if let Some(ref uid) = options.user_id {
        user_join = format!(
            "LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = ${param_index}::uuid"
        );
        params.push(uid.clone());
        param_index += 1;
    }

    // Filters
    if let Some(ref filters) = options.filters {
        if filters.contains(&"IsResumable".to_string()) && options.user_id.is_some() {
            conditions.push("uid.playback_position_ticks > 0".to_string());
        }
        if filters.contains(&"IsFavorite".to_string()) && options.user_id.is_some() {
            conditions.push("uid.is_favorite = TRUE".to_string());
        }
        if filters.contains(&"IsUnplayed".to_string()) && options.user_id.is_some() {
            conditions.push("(uid.played IS NULL OR uid.played = FALSE)".to_string());
        }
        if filters.contains(&"IsPlayed".to_string()) && options.user_id.is_some() {
            conditions.push("uid.played = TRUE".to_string());
        }
    }

    let where_clause = if conditions.is_empty() {
        String::new()
    } else {
        format!("WHERE {}", conditions.join(" AND "))
    };

    // Count query
    let count_sql = format!(
        "SELECT COUNT(DISTINCT i.id) as total FROM items i {genre_join} {user_join} {where_clause}"
    );
    let total_count = execute_count_query(pool, &count_sql, &params).await?;

    // Sort
    let order_by = build_order_by(options);

    // User columns
    let user_columns = if options.user_id.is_some() {
        "uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date"
    } else {
        "NULL::bigint as playback_position_ticks, 0::int as play_count, FALSE as is_favorite, FALSE as played, NULL::timestamp as last_played_date"
    };

    // JOIN series for image fallback: Episode uses series_id, Season uses parent_id
    let series_join = "LEFT JOIN items series_fallback ON series_fallback.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)";
    let series_cols = "series_fallback.primary_image_tag as series_primary_image_tag, \
        series_fallback.backdrop_image_tag as series_backdrop_image_tag, \
        series_fallback.id as series_fallback_id";

    let need_distinct = if !genre_join.is_empty() { "DISTINCT" } else { "" };
    let is_random = order_by.contains("RANDOM()");

    let mut item_sql = if !genre_join.is_empty() && is_random {
        format!(
            "SELECT * FROM (SELECT DISTINCT i.*, {user_columns}, {series_cols} FROM items i {genre_join} {user_join} {series_join} {where_clause}) sub"
        )
    } else {
        let actual_order = if is_random { "i.id".to_string() } else { order_by };
        format!(
            "SELECT {need_distinct} i.*, {user_columns}, {series_cols} FROM items i {genre_join} {user_join} {series_join} {where_clause} ORDER BY {actual_order}"
        )
    };

    let mut item_params = params.clone();

    // For Random sort: use random OFFSET instead of ORDER BY RANDOM() (O(1) vs O(N))
    if is_random && total_count > 0 {
        let lim = options.limit.unwrap_or(1);
        let max_offset = (total_count - lim).max(0);
        let random_offset = if max_offset > 0 {
            use rand::Rng;
            rand::rng().random_range(0..max_offset as u64)
        } else {
            0
        };
        item_sql.push_str(&format!(" LIMIT ${param_index}::bigint"));
        item_params.push(lim.to_string());
        param_index += 1;
        item_sql.push_str(&format!(" OFFSET ${param_index}::bigint"));
        item_params.push(random_offset.to_string());
        param_index += 1;
    } else {
        if let Some(lim) = options.limit {
            item_sql.push_str(&format!(" LIMIT ${param_index}::bigint"));
            item_params.push(lim.to_string());
            param_index += 1;
        }
        if let Some(off) = options.start_index {
            item_sql.push_str(&format!(" OFFSET ${param_index}::bigint"));
            item_params.push(off.to_string());
            param_index += 1;
        }
    }

    let rows = execute_item_query(pool, &item_sql, &item_params).await?;

    Ok(QueryResult {
        items: rows,
        total_count,
    })
}

fn build_order_by(options: &ItemQueryOptions) -> String {
    if let Some(ref sort_by) = options.sort_by {
        let sort_dir = if options.sort_order.as_deref() == Some("Descending") {
            "DESC"
        } else {
            "ASC"
        };
        let fields: Vec<&str> = sort_by.split(',').map(|s| s.trim()).filter(|s| !s.is_empty()).collect();
        let mapped: Vec<String> = fields
            .iter()
            .filter_map(|f| {
                let col = match *f {
                    "SortName" => "i.sort_name",
                    "DateCreated" => "i.created_at",
                    "PremiereDate" => "i.premiere_date",
                    "ProductionYear" => "i.production_year",
                    "CommunityRating" => "i.community_rating",
                    "Runtime" => "i.runtime_ticks",
                    "Random" => "RANDOM()", // Note: slow for large tables, but acceptable for LIMIT 1
                    "DatePlayed" => "uid.last_played_date",
                    _ => return None,
                };
                Some(format!("{col} {sort_dir}"))
            })
            .collect();
        if !mapped.is_empty() {
            return mapped.join(", ");
        }
    }
    "i.sort_name ASC".to_string()
}

// Execute a count query with string params (they'll be cast to appropriate types by PG)
async fn execute_count_query(pool: &PgPool, sql: &str, params: &[String]) -> AppResult<i64> {
    let mut query = sqlx::query_scalar::<_, i64>(sql);
    for p in params {
        query = query.bind(p);
    }
    let count = query.fetch_one(pool).await?;
    Ok(count)
}

// Execute item query with string params
async fn execute_item_query(pool: &PgPool, sql: &str, params: &[String]) -> AppResult<Vec<PgRow>> {
    let mut query = sqlx::query(sql);
    for p in params {
        query = query.bind(p);
    }
    let rows = query.fetch_all(pool).await?;
    Ok(rows)
}

// --- Helper functions to extract fields from PgRow ---

pub fn row_to_item_fields(row: &PgRow) -> crate::dto::format::ItemRow {
    use sqlx::Row;
    crate::dto::format::ItemRow {
        id: row.try_get::<Uuid, _>("id").map(|u| u.to_string()).unwrap_or_default(),
        name: row.try_get::<String, _>("name").unwrap_or_default(),
        item_type: row.try_get::<String, _>("type").unwrap_or_default(),
        sort_name: row.try_get("sort_name").ok(),
        collection_type: row.try_get("collection_type").ok(),
        overview: row.try_get("overview").ok(),
        production_year: row.try_get("production_year").ok(),
        premiere_date: row.try_get("premiere_date").ok(),
        community_rating: row.try_get::<f32, _>("community_rating").ok().map(|v| v as f64),
        official_rating: row.try_get("official_rating").ok(),
        runtime_ticks: row.try_get::<i64, _>("runtime_ticks").ok(),
        index_number: row.try_get("index_number").ok(),
        parent_index_number: row.try_get("parent_index_number").ok(),
        parent_id: row.try_get::<Uuid, _>("parent_id").ok().map(|u| u.to_string()),
        series_id: row.try_get::<Uuid, _>("series_id").ok().map(|u| u.to_string()),
        series_name: row.try_get("series_name").ok(),
        season_id: row.try_get::<Uuid, _>("season_id").ok().map(|u| u.to_string()),
        container: row.try_get("container").ok(),
        file_path: row.try_get("file_path").ok(),
        resolved_path: row.try_get("resolved_path").ok(),
        provider_ids: row.try_get("provider_ids").ok(),
        primary_image_tag: row.try_get("primary_image_tag").ok(),
        backdrop_image_tag: row.try_get("backdrop_image_tag").ok(),
        series_primary_image_tag: row.try_get("series_primary_image_tag").ok(),
        series_backdrop_image_tag: row.try_get("series_backdrop_image_tag").ok(),
        series_fallback_id: row.try_get::<Uuid, _>("series_fallback_id").ok().map(|u| u.to_string()),
        child_count: row.try_get("child_count").ok(),
        recursive_item_count: row.try_get("recursive_item_count").ok(),
    }
}

pub fn row_to_user_data(row: &PgRow) -> Option<crate::dto::format::UserDataRow> {
    use sqlx::Row;
    let position: Option<i64> = row.try_get("playback_position_ticks").ok();
    // If we got a position column, we have user data
    Some(crate::dto::format::UserDataRow {
        playback_position_ticks: position,
        play_count: row.try_get("play_count").ok(),
        is_favorite: row.try_get("is_favorite").ok(),
        played: row.try_get("played").ok(),
        last_played_date: row.try_get("last_played_date").ok(),
    })
}

// --- Simple queries ---

pub async fn get_child_count(pool: &PgPool, parent_id: &str) -> AppResult<i64> {
    let count: (i64,) =
        sqlx::query_as("SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid")
            .bind(parent_id)
            .fetch_one(pool)
            .await?;
    Ok(count.0)
}

pub async fn get_recursive_item_count(pool: &PgPool, parent_id: &str) -> AppResult<i64> {
    let count: (i64,) = sqlx::query_as(
        "WITH RECURSIVE children AS (
            SELECT id FROM items WHERE parent_id = $1::uuid
            UNION ALL
            SELECT i.id FROM items i JOIN children c ON i.parent_id = c.id
        ) SELECT COUNT(*) FROM children",
    )
    .bind(parent_id)
    .fetch_one(pool)
    .await?;
    Ok(count.0)
}

pub async fn get_latest_items(
    pool: &PgPool,
    cache: &CacheService,
    library_id: &str,
    limit: i64,
) -> AppResult<Vec<PgRow>> {
    // Determine collection type to pick top-level item type
    let lib_type: Option<String> =
        sqlx::query_scalar("SELECT collection_type FROM libraries WHERE id = $1::uuid")
            .bind(library_id)
            .fetch_optional(pool)
            .await?;

    let item_type = if lib_type.as_deref() == Some("tvshows") {
        "Series"
    } else {
        "Movie"
    };

    // Sort by updated_at DESC — for Series, updated_at is bumped when new episodes are added
    let rows = sqlx::query(
        "SELECT * FROM items WHERE library_id = $1::uuid AND type = $2 ORDER BY updated_at DESC LIMIT $3::bigint",
    )
    .bind(library_id)
    .bind(item_type)
    .bind(limit)
    .fetch_all(pool)
    .await?;

    Ok(rows)
}

pub async fn get_latest_batch(
    pool: &PgPool,
    cache: &CacheService,
    library_ids: &[String],
    limit: i64,
) -> AppResult<HashMap<String, Vec<PgRow>>> {
    // Get all library types in one query
    let lib_types: HashMap<String, String> = {
        let rows: Vec<(uuid::Uuid, String)> = sqlx::query_as(
            "SELECT id, collection_type FROM libraries"
        ).fetch_all(pool).await?;
        rows.into_iter().map(|(id, ct)| (id.to_string(), ct)).collect()
    };

    // Query all libraries concurrently
    let mut handles = Vec::with_capacity(library_ids.len());
    for lid in library_ids {
        let pool = pool.clone();
        let lid = lid.clone();
        let item_type = if lib_types.get(&lid).map(|s| s.as_str()) == Some("tvshows") {
            "Series"
        } else {
            "Movie"
        };
        let item_type = item_type.to_string();
        handles.push(tokio::spawn(async move {
            let rows = sqlx::query(
                "SELECT * FROM items WHERE library_id = $1::uuid AND type = $2 ORDER BY updated_at DESC LIMIT $3::bigint",
            )
            .bind(&lid)
            .bind(&item_type)
            .bind(limit)
            .fetch_all(&pool)
            .await;
            (lid, rows)
        }));
    }

    let mut result = HashMap::new();
    for h in handles {
        let (lid, rows) = h.await.map_err(|e| crate::error::AppError::Internal(anyhow::anyhow!("{e}")))?;
        result.insert(lid, rows?);
    }

    Ok(result)
}

// --- Media streams ---

pub async fn get_media_streams(pool: &PgPool, item_id: &str) -> AppResult<Vec<PgRow>> {
    let rows = sqlx::query("SELECT * FROM media_streams WHERE item_id = $1::uuid ORDER BY stream_index")
        .bind(item_id)
        .fetch_all(pool)
        .await?;
    Ok(rows)
}

pub async fn get_user_item_data(
    pool: &PgPool,
    user_id: &str,
    item_id: &str,
) -> AppResult<Option<PgRow>> {
    let row = sqlx::query(
        "SELECT * FROM user_item_data WHERE user_id = $1::uuid AND item_id = $2::uuid",
    )
    .bind(user_id)
    .bind(item_id)
    .fetch_optional(pool)
    .await?;
    Ok(row)
}

pub async fn upsert_user_item_data(
    pool: &PgPool,
    user_id: &str,
    item_id: &str,
    position: Option<i64>,
    play_count: Option<i32>,
    is_favorite: Option<bool>,
    played: Option<bool>,
) -> AppResult<()> {
    sqlx::query(
        "INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
         VALUES ($1::uuid, $2::uuid, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, false), COALESCE($6, false), NOW())
         ON CONFLICT (user_id, item_id) DO UPDATE SET
           playback_position_ticks = COALESCE($3, user_item_data.playback_position_ticks),
           play_count = COALESCE($4, user_item_data.play_count),
           is_favorite = COALESCE($5, user_item_data.is_favorite),
           played = COALESCE($6, user_item_data.played),
           last_played_date = NOW()"
    )
    .bind(user_id)
    .bind(item_id)
    .bind(position)
    .bind(play_count)
    .bind(is_favorite)
    .bind(played)
    .execute(pool)
    .await?;
    Ok(())
}

// --- Genres & Cast ---

pub async fn get_item_genres(pool: &PgPool, item_id: &str) -> AppResult<Vec<(String, String)>> {
    let rows: Vec<(Uuid, String)> = sqlx::query_as(
        "SELECT g.id, g.name FROM genres g JOIN item_genres ig ON g.id = ig.genre_id WHERE ig.item_id = $1::uuid ORDER BY g.name",
    )
    .bind(item_id)
    .fetch_all(pool)
    .await?;
    Ok(rows.into_iter().map(|(id, name)| (id.to_string(), name)).collect())
}

pub async fn get_item_cast(pool: &PgPool, item_id: &str) -> AppResult<Vec<serde_json::Value>> {
    let rows = sqlx::query(
        "SELECT * FROM cast_members WHERE item_id = $1::uuid ORDER BY role, order_index",
    )
    .bind(item_id)
    .fetch_all(pool)
    .await?;

    Ok(rows
        .iter()
        .map(|r| {
            let name: String = r.try_get("name").unwrap_or_default();
            let character: Option<String> = r.try_get("character").ok();
            let role: String = r.try_get("role").unwrap_or_default();
            let tmdb_id: Option<i32> = r.try_get("tmdb_id").ok();
            let id: Uuid = r.try_get("id").unwrap_or_default();
            let image_url: Option<String> = r.try_get("image_url").ok();
            let order_index: Option<i32> = r.try_get("order_index").ok();

            let mut val = serde_json::json!({
                "Name": name,
                "Role": character.unwrap_or_default(),
                "Type": role,
                "Id": id.to_string(),
            });
            if let Some(ref url) = image_url {
                if !url.is_empty() {
                    val["PrimaryImageTag"] = serde_json::json!(id.to_string());
                    val["HasPrimaryImage"] = serde_json::json!(true);
                }
            }
            if let Some(oi) = order_index {
                val["OrderIndex"] = serde_json::json!(oi);
            }
            val
        })
        .collect())
}

pub async fn get_all_genres_with_counts(pool: &PgPool) -> AppResult<Vec<(String, String, i64)>> {
    let rows: Vec<(Uuid, String, i64)> = sqlx::query_as(
        "SELECT g.id, g.name, COUNT(ig.item_id) as item_count
         FROM genres g LEFT JOIN item_genres ig ON g.id = ig.genre_id
         GROUP BY g.id, g.name ORDER BY g.name",
    )
    .fetch_all(pool)
    .await?;
    Ok(rows.into_iter().map(|(id, name, count)| (id.to_string(), name, count)).collect())
}

pub async fn get_all_persons(pool: &PgPool, limit: i64) -> AppResult<Vec<PgRow>> {
    let rows = sqlx::query(
        "SELECT DISTINCT name, role, image_url, tmdb_id,
         COUNT(DISTINCT item_id) as item_count
         FROM cast_members GROUP BY name, role, image_url, tmdb_id
         ORDER BY item_count DESC LIMIT $1",
    )
    .bind(limit)
    .fetch_all(pool)
    .await?;
    Ok(rows)
}
