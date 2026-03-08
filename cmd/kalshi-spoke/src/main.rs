mod config;
mod discord;
mod kalshi;
mod state;

use std::{collections::BTreeMap, io::ErrorKind, str::FromStr, sync::Arc};

use anyhow::{Context, Result};
use axum::{extract::State, routing::get, Json, Router};
use chrono::{DateTime, Utc};
use config::{Config, PublicConfig};
use discord::DiscordClient;
use futures_util::{SinkExt, StreamExt};
use kalshi::KalshiClient;
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use serde_json::json;
use state::{PersistedState, StateStore};
use tokio::{net::TcpListener, sync::{watch, Mutex}, time::sleep};
use tokio_tungstenite::tungstenite::Message;
use tracing::{error, info, warn};

#[derive(Clone)]
struct AppState {
    started_at: DateTime<Utc>,
    config: PublicConfig,
    runtime: Arc<RuntimeState>,
    persisted: Arc<StateStore>,
}

struct RuntimeState {
    inner: Mutex<RuntimeSnapshot>,
}

#[derive(Debug, Clone, Serialize)]
struct RuntimeSnapshot {
    ws_connected: bool,
    last_ws_message_at: Option<DateTime<Utc>>,
    last_ws_error: Option<String>,
    markets: BTreeMap<String, RuntimeMarketSnapshot>,
}

#[derive(Debug, Clone, Default, Serialize)]
struct RuntimeMarketSnapshot {
    last_yes_bid_dollars: Option<String>,
    last_updated_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Serialize)]
struct StatusResponse {
    status: &'static str,
    started_at: DateTime<Utc>,
    config: PublicConfig,
    runtime: RuntimeSnapshot,
    persisted: PersistedState,
}

#[derive(Debug, Deserialize)]
struct TickerEnvelope {
    msg: TickerPayload,
}

#[derive(Debug, Deserialize)]
struct TickerPayload {
    market_ticker: String,
    #[serde(default)]
    yes_bid_dollars: Option<String>,
    #[serde(default)]
    yes_bid: Option<i64>,
}

#[derive(Debug, Deserialize)]
struct WsErrorEnvelope {
    #[serde(default)]
    msg: WsErrorMessage,
}

#[derive(Debug, Default, Deserialize)]
struct WsErrorMessage {
    #[serde(default)]
    code: Option<i64>,
    #[serde(default)]
    msg: Option<String>,
}

struct SellOutcome {
    summary: String,
    client_order_id: Option<String>,
}

impl Default for RuntimeSnapshot {
    fn default() -> Self {
        Self {
            ws_connected: false,
            last_ws_message_at: None,
            last_ws_error: None,
            markets: BTreeMap::new(),
        }
    }
}

impl RuntimeState {
    fn new() -> Self {
        Self {
            inner: Mutex::new(RuntimeSnapshot::default()),
        }
    }

    async fn snapshot(&self) -> RuntimeSnapshot {
        self.inner.lock().await.clone()
    }

    async fn set_ws_connected(&self, connected: bool) {
        self.inner.lock().await.ws_connected = connected;
    }

    async fn clear_ws_error(&self) {
        self.inner.lock().await.last_ws_error = None;
    }

    async fn record_ws_error(&self, message: impl Into<String>) {
        self.inner.lock().await.last_ws_error = Some(message.into());
    }

    async fn record_ws_message(&self) {
        self.inner.lock().await.last_ws_message_at = Some(Utc::now());
    }

    async fn record_ticker_price(&self, market_ticker: &str, yes_bid_dollars: String) {
        let mut guard = self.inner.lock().await;
        let market = guard.markets.entry(market_ticker.to_string()).or_default();
        market.last_yes_bid_dollars = Some(yes_bid_dollars);
        market.last_updated_at = Some(Utc::now());
    }
}

impl TickerPayload {
    fn yes_bid_decimal(&self) -> Result<Option<Decimal>> {
        if let Some(raw) = self.yes_bid_dollars.as_deref() {
            let parsed = Decimal::from_str(raw.trim()).with_context(|| {
                format!(
                    "failed to parse yes_bid_dollars {:?} for market {}",
                    raw, self.market_ticker
                )
            })?;
            return Ok(Some(parsed));
        }

        if let Some(cents) = self.yes_bid {
            if cents < 0 {
                return Ok(None);
            }

            return Ok(Some(Decimal::new(cents, 2)));
        }

        Ok(None)
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    load_dotenv();

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .with_target(false)
        .init();

    let config = Config::load().context("invalid configuration")?;
    let runtime = Arc::new(RuntimeState::new());
    let persisted = Arc::new(StateStore::load(&config.state_file)?);

    let http_client = reqwest::Client::builder()
        .timeout(config.http_timeout)
        .build()
        .context("failed to build HTTP client")?;

    let discord = Arc::new(DiscordClient::new(
        http_client.clone(),
        config.hub_notify_url.clone(),
        config.notify_channel.clone(),
        config.notify_severity.clone(),
    ));

    let app_state = Arc::new(AppState {
        started_at: Utc::now(),
        config: config.public(),
        runtime: runtime.clone(),
        persisted: persisted.clone(),
    });

    let (shutdown_tx, shutdown_rx) = watch::channel(false);

    let server_task = tokio::spawn(run_http_server(
        config.http_addr.clone(),
        app_state,
        shutdown_rx.clone(),
    ));

    let ws_task = if config.enabled {
        let kalshi = Arc::new(KalshiClient::new(
            http_client,
            config.kalshi_api_base_url.clone(),
            config.kalshi_ws_url.clone(),
            config.kalshi_access_key.clone(),
            &config.kalshi_private_key_path,
        )?);

        let online_message = format!(
            "kalshi-spoke online: tickers={} threshold={} auto_sell={} dry_run={} subaccount={}",
            config.market_tickers.join(","),
            config.trigger_threshold_string(),
            config.auto_sell_enabled,
            config.dry_run,
            config.subaccount
        );
        if let Err(err) = discord.notify(&online_message, Some("info")).await {
            warn!("startup discord notify failed: {err:#}");
        }

        let ws_config = Arc::new(config.clone());
        Some(tokio::spawn(async move {
            if let Err(err) =
                run_ws_loop(ws_config, kalshi, discord, persisted, runtime, shutdown_rx).await
            {
                error!("kalshi websocket loop exited with error: {err:#}");
            }
        }))
    } else {
        info!("KALSHI_SPOKE_ENABLED=false; websocket monitoring is disabled");
        None
    };

    tokio::signal::ctrl_c()
        .await
        .context("failed to listen for ctrl-c")?;
    info!("shutdown signal received");
    let _ = shutdown_tx.send(true);

    match server_task.await {
        Ok(Ok(())) => {}
        Ok(Err(err)) => error!("http server exited with error: {err:#}"),
        Err(err) => error!("http server task join error: {err}"),
    }

    if let Some(task) = ws_task {
        if let Err(err) = task.await {
            error!("ws task join error: {err}");
        }
    }

    Ok(())
}

fn load_dotenv() {
    for path in ["cmd/kalshi-spoke/.env", ".env"] {
        if let Err(err) = dotenvy::from_filename(path) {
            match err {
                dotenvy::Error::Io(io_err) if io_err.kind() == ErrorKind::NotFound => {}
                _ => eprintln!("warning: unable to load {}: {}", path, err),
            }
        }
    }
}

async fn run_http_server(
    addr: String,
    app_state: Arc<AppState>,
    mut shutdown: watch::Receiver<bool>,
) -> Result<()> {
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/status", get(status))
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
    Json(json!({ "status": "ok" }))
}

async fn status(State(state): State<Arc<AppState>>) -> Json<StatusResponse> {
    Json(StatusResponse {
        status: "ok",
        started_at: state.started_at,
        config: state.config.clone(),
        runtime: state.runtime.snapshot().await,
        persisted: state.persisted.snapshot().await,
    })
}

async fn run_ws_loop(
    config: Arc<Config>,
    kalshi: Arc<KalshiClient>,
    discord: Arc<DiscordClient>,
    persisted: Arc<StateStore>,
    runtime: Arc<RuntimeState>,
    mut shutdown: watch::Receiver<bool>,
) -> Result<()> {
    loop {
        if *shutdown.borrow() {
            break;
        }

        match run_ws_session(
            &config,
            &kalshi,
            &discord,
            &persisted,
            &runtime,
            shutdown.clone(),
        )
        .await
        {
            Ok(()) => {
                runtime.set_ws_connected(false).await;
                runtime.record_ws_error("websocket session ended").await;
                warn!("kalshi websocket session ended; reconnecting");
            }
            Err(err) => {
                runtime.set_ws_connected(false).await;
                runtime.record_ws_error(err.to_string()).await;
                warn!("kalshi websocket session failed: {err:#}");
                let message = format!("Kalshi websocket disconnected: {}", err);
                if let Err(notify_err) = discord.notify(&message, Some("warning")).await {
                    warn!("failed to send websocket error alert to discord-hub: {notify_err:#}");
                }
            }
        }

        tokio::select! {
            _ = shutdown.changed() => {
                if *shutdown.borrow() {
                    break;
                }
            }
            _ = sleep(config.ws_reconnect_delay) => {}
        }
    }

    Ok(())
}

async fn run_ws_session(
    config: &Config,
    kalshi: &KalshiClient,
    discord: &DiscordClient,
    persisted: &StateStore,
    runtime: &RuntimeState,
    mut shutdown: watch::Receiver<bool>,
) -> Result<()> {
    let mut socket = kalshi.connect_websocket().await?;
    runtime.set_ws_connected(true).await;
    runtime.clear_ws_error().await;

    let subscribe = json!({
        "id": 1,
        "cmd": "subscribe",
        "params": {
            "channels": ["ticker"],
            "market_tickers": config.market_tickers.clone(),
        }
    });

    socket
        .send(Message::Text(subscribe.to_string()))
        .await
        .context("failed to subscribe to ticker channel")?;
    info!(
        "subscribed to Kalshi ticker channel for {}",
        config.market_tickers.join(",")
    );

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                if *shutdown.borrow() {
                    let _ = socket.close(None).await;
                    break;
                }
            }
            next_message = socket.next() => {
                let Some(message_result) = next_message else {
                    break;
                };

                match message_result {
                    Ok(Message::Text(text)) => {
                        runtime.record_ws_message().await;
                        handle_ws_text(config, kalshi, discord, persisted, runtime, &text).await;
                    }
                    Ok(Message::Ping(payload)) => {
                        socket
                            .send(Message::Pong(payload))
                            .await
                            .context("failed to respond with websocket pong")?;
                    }
                    Ok(Message::Close(frame)) => {
                        info!("kalshi websocket closed: {:?}", frame);
                        break;
                    }
                    Ok(_) => {}
                    Err(err) => {
                        return Err(err).context("kalshi websocket read failed");
                    }
                }
            }
        }
    }

    runtime.set_ws_connected(false).await;
    Ok(())
}

async fn handle_ws_text(
    config: &Config,
    kalshi: &KalshiClient,
    discord: &DiscordClient,
    persisted: &StateStore,
    runtime: &RuntimeState,
    text: &str,
) {
    let value: serde_json::Value = match serde_json::from_str(text) {
        Ok(value) => value,
        Err(err) => {
            warn!("ignoring malformed websocket payload: {err}");
            return;
        }
    };

    let message_type = value
        .get("type")
        .and_then(|entry| entry.as_str())
        .unwrap_or("");
    match message_type {
        "ticker" => {
            let envelope: TickerEnvelope = match serde_json::from_value(value) {
                Ok(parsed) => parsed,
                Err(err) => {
                    warn!("ignoring ticker payload parse error: {err}");
                    return;
                }
            };

            if let Err(err) =
                process_ticker_update(config, kalshi, discord, persisted, runtime, envelope.msg)
                    .await
            {
                runtime.record_ws_error(err.to_string()).await;
                warn!("ticker processing failed: {err:#}");
            }
        }
        "error" => {
            let envelope: WsErrorEnvelope =
                serde_json::from_value(value).unwrap_or(WsErrorEnvelope {
                    msg: WsErrorMessage::default(),
                });
            let code = envelope.msg.code.unwrap_or(-1);
            let message = envelope
                .msg
                .msg
                .unwrap_or_else(|| "unknown websocket error".to_string());
            runtime.record_ws_error(format!("code={} msg={}", code, message)).await;
            warn!("kalshi websocket returned error code {}: {}", code, message);
        }
        _ => {}
    }
}

async fn process_ticker_update(
    config: &Config,
    kalshi: &KalshiClient,
    discord: &DiscordClient,
    persisted: &StateStore,
    runtime: &RuntimeState,
    payload: TickerPayload,
) -> Result<()> {
    let market_ticker = payload.market_ticker.trim();
    if market_ticker.is_empty() {
        return Ok(());
    }

    let Some(yes_bid) = payload.yes_bid_decimal()? else {
        return Ok(());
    };

    let yes_bid_str = format_decimal_4(yes_bid);
    runtime.record_ticker_price(market_ticker, yes_bid_str.clone()).await;

    let is_above = yes_bid >= config.trigger_yes_bid_dollars;
    if !persisted.has_market(market_ticker).await {
        persisted.update_market(market_ticker, |state| {
            state.was_above_threshold = is_above;
            state.last_yes_bid_dollars = Some(yes_bid_str.clone());
            state.last_action = Some("initialized market state".to_string());
        }).await?;
        info!(
            "initialized state for {} at yes_bid={} (above_threshold={})",
            market_ticker, yes_bid_str, is_above
        );
        return Ok(());
    }

    let previous = persisted.market_snapshot(market_ticker).await;

    if !previous.was_above_threshold && is_above {
        let threshold = config.trigger_threshold_string();
        let trigger_message = format!(
            "[trigger] {} YES bid {} crossed above {}",
            market_ticker, yes_bid_str, threshold
        );
        info!("{}", trigger_message);
        if let Err(err) = discord.notify(&trigger_message, Some("info")).await {
            warn!("failed to deliver trigger alert to discord-hub: {err:#}");
        }

        let mut client_order_id = previous.last_client_order_id.clone();
        let action_summary = if config.auto_sell_enabled {
            let outcome = attempt_sell(config, kalshi, discord, market_ticker).await?;
            if outcome.client_order_id.is_some() {
                client_order_id = outcome.client_order_id;
            }
            outcome.summary
        } else {
            "trigger fired (auto-sell disabled)".to_string()
        };

        persisted.update_market(market_ticker, |state| {
            state.was_above_threshold = true;
            state.last_yes_bid_dollars = Some(yes_bid_str.clone());
            state.last_triggered_at = Some(Utc::now());
            state.last_client_order_id = client_order_id.clone();
            state.last_action = Some(action_summary.clone());
        }).await?;
        return Ok(());
    }

    if previous.was_above_threshold && !is_above {
        let threshold = config.trigger_threshold_string();
        let rearmed_message = format!(
            "[re-armed] {} YES bid {} dropped below {}",
            market_ticker, yes_bid_str, threshold
        );
        info!("{}", rearmed_message);
        if let Err(err) = discord.notify(&rearmed_message, Some("info")).await {
            warn!("failed to deliver re-armed alert to discord-hub: {err:#}");
        }

        persisted.update_market(market_ticker, |state| {
            state.was_above_threshold = false;
            state.last_yes_bid_dollars = Some(yes_bid_str.clone());
            state.last_action = Some("trigger re-armed".to_string());
        }).await?;
    }

    Ok(())
}

async fn attempt_sell(
    config: &Config,
    kalshi: &KalshiClient,
    discord: &DiscordClient,
    market_ticker: &str,
) -> Result<SellOutcome> {
    let position_fp = kalshi
        .fetch_yes_position(market_ticker, config.subaccount)
        .await
        .with_context(|| format!("failed to load YES position for {}", market_ticker))?;

    if position_fp <= Decimal::ZERO {
        let message = format!(
            "[auto-sell skipped] {} crossed threshold but no YES position was found on subaccount {}",
            market_ticker, config.subaccount
        );
        if let Err(err) = discord.notify(&message, Some("warning")).await {
            warn!("failed to deliver no-position alert to discord-hub: {err:#}");
        }

        return Ok(SellOutcome {
            summary: "trigger fired but no YES position found".to_string(),
            client_order_id: None,
        });
    }

    let count_fp = format_decimal_2(position_fp);
    let yes_price = config.trigger_threshold_string();
    let client_order_id = build_client_order_id(market_ticker);

    if config.dry_run {
        let message = format!(
            "[dry-run] would submit reduce-only IOC sell: ticker={} contracts={} yes_price={} subaccount={} client_order_id={}",
            market_ticker, count_fp, yes_price, config.subaccount, client_order_id
        );
        if let Err(err) = discord.notify(&message, Some("warning")).await {
            warn!("failed to deliver dry-run alert to discord-hub: {err:#}");
        }

        return Ok(SellOutcome {
            summary: format!("dry-run sell prepared for {} contracts", count_fp),
            client_order_id: Some(client_order_id),
        });
    }

    let order = kalshi
        .create_reduce_only_sell_order(
            market_ticker,
            &count_fp,
            &yes_price,
            &client_order_id,
            config.subaccount,
        )
        .await
        .with_context(|| {
            format!(
                "failed to create reduce-only sell order for {}",
                market_ticker
            )
        })?;

    let summary = format!(
        "[order] submitted reduce-only IOC sell: ticker={} contracts={} yes_price={} order_id={} status={} filled={} remaining={}",
        market_ticker,
        count_fp,
        yes_price,
        order.order_id,
        order.status,
        order.fill_count_fp.unwrap_or_else(|| "n/a".to_string()),
        order.remaining_count_fp.unwrap_or_else(|| "n/a".to_string()),
    );

    if let Err(err) = discord.notify(&summary, Some("info")).await {
        warn!("failed to deliver order alert to discord-hub: {err:#}");
    }

    Ok(SellOutcome {
        summary,
        client_order_id: Some(client_order_id),
    })
}

fn build_client_order_id(market_ticker: &str) -> String {
    let mut normalized: String = market_ticker
        .chars()
        .map(|ch| {
            if ch.is_ascii_alphanumeric() {
                ch.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect();

    while normalized.contains("--") {
        normalized = normalized.replace("--", "-");
    }

    normalized = normalized.trim_matches('-').to_string();
    if normalized.is_empty() {
        normalized = "market".to_string();
    }
    normalized.truncate(24);

    format!("sell60-{}-{}", normalized, Utc::now().timestamp_millis())
}

fn format_decimal_4(value: Decimal) -> String {
    format!("{:.4}", value.round_dp(4))
}

fn format_decimal_2(value: Decimal) -> String {
    format!("{:.2}", value.round_dp(2))
}
