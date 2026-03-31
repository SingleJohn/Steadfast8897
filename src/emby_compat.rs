use axum::body::Body;
use axum::http::Request;
use axum::middleware::Next;
use axum::response::Response;

/// Middleware: strip /emby/ prefix from requests (Infuse, VidHub, etc.)
pub async fn strip_emby_prefix(mut req: Request<Body>, next: Next) -> Response {
    let path = req.uri().path();
    if path.starts_with("/emby/") {
        let new_path = &path[5..]; // strip "/emby" keeping the /
        let new_uri = if let Some(query) = req.uri().query() {
            format!("{new_path}?{query}")
        } else {
            new_path.to_string()
        };
        if let Ok(uri) = new_uri.parse() {
            *req.uri_mut() = uri;
        }
    } else if path == "/emby" {
        if let Ok(uri) = "/".parse() {
            *req.uri_mut() = uri;
        }
    }
    next.run(req).await
}
