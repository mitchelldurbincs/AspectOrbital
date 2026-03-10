use std::sync::Arc;

use anyhow::{Context, Result};
use axum::{
    extract::State,
    http::{header::AUTHORIZATION, HeaderMap, StatusCode},
    routing::{get, post},
    Json, Router,
};
use subtle::ConstantTimeEq;
use tokio::{net::TcpListener, sync::watch};
use tracing::info;

use crate::{
    app::AppState,
    commands::{control_command, control_commands},
    models::StatusResponse,
    summaries::build_status_response,
};

pub(crate) async fn run_http_server(
    addr: String,
    app_state: Arc<AppState>,
    mut shutdown: watch::Receiver<bool>,
) -> Result<()> {
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/status", get(status))
        .route("/control/commands", get(control_commands))
        .route("/control/command", post(control_command))
        .with_state(app_state);

    let listener = TcpListener::bind(&addr)
        .await
        .with_context(|| format!("failed to bind Kalshi spoke HTTP server on {}", addr))?;
    info!("kalshi-spoke HTTP API listening on {}", addr);

    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            let _ = shutdown.changed().await;
        })
        .await
        .context("http server exited unexpectedly")?;

    Ok(())
}

async fn healthz() -> Json<serde_json::Value> {
    Json(serde_json::json!({ "status": "ok" }))
}

async fn status(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
) -> std::result::Result<Json<StatusResponse>, (StatusCode, String)> {
    authorize(&headers, state.spoke_command_auth_token.as_str())?;
    Ok(Json(build_status_response(state).await))
}

pub(crate) fn authorize(
    headers: &HeaderMap,
    expected_token: &str,
) -> std::result::Result<(), (StatusCode, String)> {
    let Some(value) = headers.get(AUTHORIZATION) else {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    };
    let Ok(raw) = value.to_str() else {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    };

    let mut parts = raw.split_whitespace();
    let Some(scheme) = parts.next() else {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    };
    let Some(token) = parts.next() else {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    };
    if parts.next().is_some() || !scheme.eq_ignore_ascii_case("Bearer") {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    }

    let expected = expected_token.trim();
    if expected.is_empty()
        || token.trim().is_empty()
        || token.as_bytes().ct_eq(expected.as_bytes()).unwrap_u8() != 1
    {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized".to_string()));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use axum::http::{header::AUTHORIZATION, HeaderMap, HeaderValue, StatusCode};

    use super::authorize;

    #[test]
    fn authorize_rejects_missing_bearer_token() {
        let headers = HeaderMap::new();
        let err = authorize(&headers, "test-token").expect_err("auth should fail");
        assert_eq!(err.0, StatusCode::UNAUTHORIZED);
    }

    #[test]
    fn authorize_accepts_matching_bearer_token() {
        let mut headers = HeaderMap::new();
        headers.insert(AUTHORIZATION, HeaderValue::from_static("Bearer test-token"));
        authorize(&headers, "test-token").expect("auth should succeed");
    }
}
