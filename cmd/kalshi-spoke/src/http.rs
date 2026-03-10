use std::sync::Arc;

use anyhow::{Context, Result};
use axum::{
    extract::State,
    routing::{get, post},
    Json, Router,
};
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

async fn status(State(state): State<Arc<AppState>>) -> Json<StatusResponse> {
    Json(build_status_response(state).await)
}
