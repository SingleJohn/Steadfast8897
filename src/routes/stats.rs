use axum::extract::{Path, Query, State};
use axum::response::IntoResponse;
use axum::routing::get;
use axum::{Json, Router};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use std::sync::Arc;

use crate::auth::RequireAdmin;
use crate::error::AppResult;
use crate::state::AppState;

#[derive(Deserialize, Default)]
#[serde(default)]
struct StatsQuery {
    days: Option<String>,
    filter: Option<String>,
    limit: Option<String>,
    #[serde(alias = "endDate")]
    end_date: Option<String>,
}

fn get_days(q: &StatsQuery) -> i32 {
    q.days.as_ref().and_then(|s| s.parse().ok()).unwrap_or(30)
}

// User activity summary
async fn user_activity(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<StatsQuery>,
) -> AppResult<impl IntoResponse> {
    let days = get_days(&q);
    let rows = sqlx::query(
        "SELECT pa.user_id, u.name as user_name,
            MAX(pa.date_created) as last_seen,
            MAX(pa.item_name) as last_item_name,
            MAX(pa.client_name) as last_client_name,
            COUNT(*) as total_plays,
            COALESCE(SUM(pa.play_duration), 0) as total_duration
        FROM playback_activity pa
        LEFT JOIN users u ON pa.user_id = u.id
        WHERE pa.date_created >= NOW() - INTERVAL '1 day' * $1
        GROUP BY pa.user_id, u.name
        ORDER BY last_seen DESC",
    )
    .bind(days)
    .fetch_all(&state.db)
    .await?;

    let result: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "user_id": r.try_get::<uuid::Uuid, _>("user_id").map(|u| u.to_string()).unwrap_or_default(),
                "user_name": r.try_get::<String, _>("user_name").unwrap_or_else(|_| "Unknown".into()),
                "has_image": false,
                "last_seen": r.try_get::<chrono::NaiveDateTime, _>("last_seen")
                    .ok().map(|d| d.and_utc().to_rfc3339()),
                "item_name": r.try_get::<String, _>("last_item_name").unwrap_or_default(),
                "client_name": r.try_get::<String, _>("last_client_name").unwrap_or_default(),
                "total_plays": r.try_get::<i64, _>("total_plays").unwrap_or(0),
                "total_play_time": r.try_get::<i64, _>("total_duration").unwrap_or(0),
            })
        })
        .collect();

    Ok(Json(json!(result)))
}

// Play activity daily
async fn play_activity(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<StatsQuery>,
) -> AppResult<impl IntoResponse> {
    let days = get_days(&q);
    let rows = sqlx::query(
        "SELECT date_created::date as date, COUNT(*) as count,
            COALESCE(SUM(play_duration), 0) as total_duration
        FROM playback_activity
        WHERE date_created >= NOW() - INTERVAL '1 day' * $1
        GROUP BY date_created::date
        ORDER BY date ASC",
    )
    .bind(days)
    .fetch_all(&state.db)
    .await?;

    let result: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "date": r.try_get::<chrono::NaiveDate, _>("date")
                    .map(|d| d.to_string()).unwrap_or_default(),
                "count": r.try_get::<i64, _>("count").unwrap_or(0),
                "total_duration": r.try_get::<i64, _>("total_duration").unwrap_or(0),
            })
        })
        .collect();

    Ok(Json(json!(result)))
}

// Hourly report
async fn hourly_report(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<StatsQuery>,
) -> AppResult<impl IntoResponse> {
    let days = get_days(&q);
    let rows = sqlx::query(
        "SELECT EXTRACT(DOW FROM date_created) as day_of_week,
            EXTRACT(HOUR FROM date_created) as hour,
            COUNT(*) as count
        FROM playback_activity
        WHERE date_created >= NOW() - INTERVAL '1 day' * $1
        GROUP BY day_of_week, hour
        ORDER BY day_of_week, hour",
    )
    .bind(days)
    .fetch_all(&state.db)
    .await?;

    let result: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "DayOfWeek": r.try_get::<f64, _>("day_of_week").unwrap_or(0.0) as i32,
                "Hour": r.try_get::<f64, _>("hour").unwrap_or(0.0) as i32,
                "Count": r.try_get::<i64, _>("count").unwrap_or(0),
            })
        })
        .collect();

    Ok(Json(json!(result)))
}

// Breakdown report
async fn breakdown_report(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Path(report_type): Path<String>,
    Query(q): Query<StatsQuery>,
) -> AppResult<impl IntoResponse> {
    let days = get_days(&q);

    let (group_col, label_col, need_join) = match report_type.as_str() {
        "UserId" => ("pa.user_id", "u.name", true),
        "ItemType" => ("pa.item_type", "pa.item_type", false),
        "ClientName" => ("pa.client_name", "pa.client_name", false),
        "DeviceName" => ("pa.device_name", "pa.device_name", false),
        "PlaybackMethod" => ("pa.play_method", "pa.play_method", false),
        _ => ("pa.item_type", "pa.item_type", false),
    };

    let join = if need_join { "LEFT JOIN users u ON pa.user_id = u.id" } else { "" };
    let sql = format!(
        "SELECT {label_col} as label, COUNT(*) as count,
            COALESCE(SUM(pa.play_duration), 0) as total_duration
        FROM playback_activity pa {join}
        WHERE pa.date_created >= NOW() - INTERVAL '1 day' * $1
        GROUP BY {group_col}, {label_col}
        ORDER BY count DESC"
    );

    let rows = sqlx::query(&sql).bind(days).fetch_all(&state.db).await?;

    let result: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "label": r.try_get::<String, _>("label").unwrap_or_else(|_| "Unknown".into()),
                "count": r.try_get::<i64, _>("count").unwrap_or(0),
                "total_duration": r.try_get::<i64, _>("total_duration").unwrap_or(0),
            })
        })
        .collect();

    Ok(Json(json!(result)))
}

// Recent playback
async fn recent_playback(
    State(state): State<Arc<AppState>>,
    RequireAdmin(_, _): RequireAdmin,
    Query(q): Query<StatsQuery>,
) -> AppResult<impl IntoResponse> {
    let limit: i64 = q.limit.as_ref().and_then(|s| s.parse().ok()).unwrap_or(50).min(200);

    let rows = sqlx::query(
        "SELECT pa.date_created, pa.item_name, pa.item_type, pa.series_name,
            pa.client_name, pa.device_name, pa.client_ip, pa.play_duration,
            u.name as user_name
        FROM playback_activity pa
        LEFT JOIN users u ON pa.user_id = u.id
        ORDER BY pa.date_created DESC
        LIMIT $1",
    )
    .bind(limit)
    .fetch_all(&state.db)
    .await?;

    let result: Vec<serde_json::Value> = rows
        .iter()
        .map(|r| {
            json!({
                "date": r.try_get::<chrono::NaiveDateTime, _>("date_created")
                    .ok().map(|d| d.and_utc().to_rfc3339()),
                "user_name": r.try_get::<String, _>("user_name").unwrap_or_else(|_| "Unknown".into()),
                "item_name": r.try_get::<String, _>("item_name").unwrap_or_default(),
                "item_type": r.try_get::<String, _>("item_type").unwrap_or_default(),
                "series_name": r.try_get::<String, _>("series_name").ok(),
                "client_name": r.try_get::<String, _>("client_name").unwrap_or_default(),
                "device_name": r.try_get::<String, _>("device_name").unwrap_or_default(),
                "client_ip": r.try_get::<String, _>("client_ip").unwrap_or_default(),
                "play_duration": r.try_get::<i32, _>("play_duration").unwrap_or(0),
            })
        })
        .collect();

    Ok(Json(json!(result)))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new()
        .route("/user_usage_stats/user_activity", get(user_activity))
        .route("/user_usage_stats/PlayActivity", get(play_activity))
        .route("/user_usage_stats/HourlyReport", get(hourly_report))
        .route("/user_usage_stats/{type}/BreakdownReport", get(breakdown_report))
        .route("/user_usage_stats/RecentPlayback", get(recent_playback))
}
