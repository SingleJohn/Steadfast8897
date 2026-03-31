use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::IntoResponse;
use axum::routing::post;
use axum::{Json, Router};
use serde_json::json;
use sqlx::Row;
use std::sync::Arc;

use crate::error::AppResult;
use crate::state::AppState;

async fn cloud_drive_webhook(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<serde_json::Value>,
) -> AppResult<impl IntoResponse> {
    // Validate shared secret
    let secret = headers
        .get("x-webhook-secret")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");

    let expected: String = sqlx::query_scalar("SELECT value FROM system_config WHERE key = 'webhook_secret'")
        .fetch_optional(&state.db)
        .await?
        .unwrap_or_default();

    if !expected.is_empty() && secret != expected {
        return Ok((
            StatusCode::UNAUTHORIZED,
            Json(json!({ "message": "Invalid webhook secret" })),
        ));
    }

    // Parse events
    let events: Vec<serde_json::Value> = if let Some(data) = body.get("data").and_then(|d| d.as_array()) {
        data.clone()
    } else if body.get("action").is_some() {
        vec![json!({
            "action": body.get("action"),
            "is_dir": body.get("is_dir"),
            "source_file": body.get("source_file"),
            "destination_file": body.get("destination_file"),
        })]
    } else {
        return Ok((
            StatusCode::OK,
            Json(json!({ "message": "No events to process" })),
        ));
    };

    if events.is_empty() {
        return Ok((
            StatusCode::OK,
            Json(json!({ "message": "No events to process" })),
        ));
    }

    tracing::info!(
        "[Webhook] Received {} file change event(s) from CloudDrive2",
        events.len()
    );

    // TODO: Phase 6 — route events to incremental scan handler
    // For now, just log and accept

    Ok((
        StatusCode::OK,
        Json(json!({ "message": format!("Accepted {} event(s)", events.len()) })),
    ))
}

pub fn router() -> Router<Arc<AppState>> {
    Router::new().route("/Library/Webhook/CloudDrive", post(cloud_drive_webhook))
}
