use axum::extract::FromRequestParts;
use axum::http::request::Parts;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use regex::Regex;
use std::sync::{Arc, LazyLock};

use crate::models::{session, user as user_model};
use crate::state::AppState;

static AUTH_HEADER_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"(?i)^(?:MediaBrowser|Emby)\s+(.+)$"#).unwrap());
static PAIR_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"(\w+)="([^"]*)""#).unwrap());

#[derive(Debug, Clone, Default)]
pub struct AuthInfo {
    pub user_id: Option<String>,
    pub client: Option<String>,
    pub device: Option<String>,
    pub device_id: Option<String>,
    pub version: Option<String>,
    pub token: Option<String>,
}

#[derive(Debug, Clone)]
pub struct AuthUser {
    pub id: String,
    pub name: String,
    pub is_admin: bool,
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct CachedAuth {
    id: String,
    name: String,
    is_admin: bool,
    device_id: Option<String>,
    device_name: Option<String>,
    app_name: Option<String>,
}

fn parse_auth_header(header: &str) -> AuthInfo {
    let mut info = AuthInfo::default();
    if let Some(caps) = AUTH_HEADER_RE.captures(header) {
        let pairs = &caps[1];
        for m in PAIR_RE.captures_iter(pairs) {
            let key = m[1].to_lowercase();
            let value = urlencoding::decode(&m[2])
                .unwrap_or_else(|_| m[2].to_string().into())
                .to_string();
            match key.as_str() {
                "userid" => info.user_id = Some(value),
                "client" => info.client = Some(value),
                "device" => info.device = Some(value),
                "deviceid" => info.device_id = Some(value),
                "version" => info.version = Some(value),
                "token" => info.token = Some(value),
                _ => {}
            }
        }
    }
    info
}

fn extract_token(parts: &Parts) -> (Option<String>, AuthInfo) {
    let mut auth_info = AuthInfo::default();
    let mut token: Option<String> = None;

    // Try Authorization / X-Emby-Authorization header
    let auth_header = parts
        .headers
        .get("authorization")
        .or_else(|| parts.headers.get("x-emby-authorization"))
        .and_then(|v| v.to_str().ok());

    if let Some(header) = auth_header {
        auth_info = parse_auth_header(header);
        token = auth_info.token.clone();
    }

    // Try X-Emby-Token / X-MediaBrowser-Token
    if token.is_none() {
        token = parts
            .headers
            .get("x-emby-token")
            .or_else(|| parts.headers.get("x-mediabrowser-token"))
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
    }

    // Try api_key / ApiKey query parameter
    if token.is_none() {
        if let Some(query) = parts.uri.query() {
            for pair in query.split('&') {
                if let Some((key, value)) = pair.split_once('=') {
                    if key.eq_ignore_ascii_case("api_key") || key == "ApiKey" {
                        token = Some(value.to_string());
                        break;
                    }
                }
            }
        }
    }

    (token, auth_info)
}

async fn validate_token(
    state: &AppState,
    token: &str,
    auth_info: &AuthInfo,
) -> Option<AuthUser> {
    let cache = &state.cache;
    let pool = &state.db;

    // Check auth cache
    let cache_key = format!("auth:{token}");
    if let Some(cached) = cache.get_json::<CachedAuth>(&cache_key).await {
        // Update session
        state.session_manager.update_session(
            &cached.id,
            &cached.name,
            auth_info.device_id.as_deref().unwrap_or(cached.device_id.as_deref().unwrap_or("unknown")),
            auth_info.device.as_deref().unwrap_or(cached.device_name.as_deref().unwrap_or("")),
            auth_info.client.as_deref().unwrap_or(cached.app_name.as_deref().unwrap_or("")),
            auth_info.version.as_deref().unwrap_or(""),
            "",
        );
        return Some(AuthUser {
            id: cached.id,
            name: cached.name,
            is_admin: cached.is_admin,
        });
    }

    // Check access_tokens table
    if let Ok(Some(access_token)) = session::find_by_token(pool, token).await {
        let user = user_model::find_user_by_id(pool, &access_token.user_id)
            .await
            .ok()
            .flatten()?;

        if user.is_disabled {
            return None;
        }

        let auth_user = AuthUser {
            id: user.id.to_string(),
            name: user.name.clone(),
            is_admin: user.is_admin,
        };

        // Cache for 5 minutes
        let cached = CachedAuth {
            id: user.id.to_string(),
            name: user.name,
            is_admin: user.is_admin,
            device_id: Some(access_token.device_id.clone()),
            device_name: Some(access_token.device_name.clone()),
            app_name: Some(access_token.app_name.clone()),
        };
        cache.set_json(&cache_key, &cached, 300).await;

        state.session_manager.update_session(
            &auth_user.id,
            &auth_user.name,
            auth_info.device_id.as_deref().unwrap_or(&access_token.device_id),
            auth_info.device.as_deref().unwrap_or(&access_token.device_name),
            auth_info.client.as_deref().unwrap_or(&access_token.app_name),
            auth_info.version.as_deref().unwrap_or(&access_token.app_version),
            "",
        );

        return Some(auth_user);
    }

    // Check api_keys table
    let api_cache_key = format!("apikey:{token}");
    let mut api_key_id: Option<String> = cache.get(&api_cache_key).await;

    if api_key_id.is_none() {
        if let Ok(row) =
            sqlx::query_scalar::<_, uuid::Uuid>("SELECT id FROM api_keys WHERE key = $1")
                .bind(token)
                .fetch_optional(pool)
                .await
        {
            if let Some(id) = row {
                let id_str = id.to_string();
                cache.set(&api_cache_key, &id_str, 600).await;
                api_key_id = Some(id_str);
            }
        }
    }

    if let Some(ref kid) = api_key_id {
        // Fire-and-forget: update last_used_at
        let pool2 = pool.clone();
        let kid2 = kid.clone();
        tokio::spawn(async move {
            let _ = sqlx::query("UPDATE api_keys SET last_used_at = NOW() WHERE id = $1::uuid")
                .bind(&kid2)
                .execute(&pool2)
                .await;
        });

        let api_user = AuthUser {
            id: format!("api-key-{kid}"),
            name: "API".to_string(),
            is_admin: true,
        };
        let cached = CachedAuth {
            id: api_user.id.clone(),
            name: api_user.name.clone(),
            is_admin: true,
            device_id: None,
            device_name: None,
            app_name: None,
        };
        cache.set_json(&cache_key, &cached, 600).await;
        return Some(api_user);
    }

    None
}

/// Extractor: requires authenticated user
pub struct RequireAuth(pub AuthUser, pub AuthInfo);

impl FromRequestParts<Arc<AppState>> for RequireAuth {
    type Rejection = Response;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &Arc<AppState>,
    ) -> Result<Self, Self::Rejection> {
        let (token, auth_info) = extract_token(parts);
        let token = token.ok_or_else(|| {
            (StatusCode::UNAUTHORIZED, Json(serde_json::json!({"message": "Unauthorized"}))).into_response()
        })?;

        let user = validate_token(state, &token, &auth_info)
            .await
            .ok_or_else(|| {
                (StatusCode::UNAUTHORIZED, Json(serde_json::json!({"message": "Invalid token"}))).into_response()
            })?;

        Ok(RequireAuth(user, auth_info))
    }
}

/// Extractor: optional authentication (parses auth info for all requests)
pub struct OptionalAuth(pub Option<AuthUser>, pub AuthInfo);

impl FromRequestParts<Arc<AppState>> for OptionalAuth {
    type Rejection = std::convert::Infallible;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &Arc<AppState>,
    ) -> Result<Self, Self::Rejection> {
        let (token, auth_info) = extract_token(parts);
        let user = if let Some(ref t) = token {
            validate_token(state, t, &auth_info).await
        } else {
            None
        };
        Ok(OptionalAuth(user, auth_info))
    }
}

/// Extractor: requires admin
pub struct RequireAdmin(pub AuthUser, pub AuthInfo);

impl FromRequestParts<Arc<AppState>> for RequireAdmin {
    type Rejection = Response;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &Arc<AppState>,
    ) -> Result<Self, Self::Rejection> {
        let RequireAuth(user, info) = RequireAuth::from_request_parts(parts, state).await?;
        if !user.is_admin {
            return Err(
                (StatusCode::FORBIDDEN, Json(serde_json::json!({"message": "Admin only"}))).into_response()
            );
        }
        Ok(RequireAdmin(user, info))
    }
}
