mod config;
mod discord;
mod kalshi;
mod state;

use std::{
    collections::{BTreeMap, BTreeSet},
    io::ErrorKind,
    str::FromStr,
    sync::Arc,
    time::Duration,
};

use anyhow::{Context, Result};
use axum::{
    extract::State,
    http::StatusCode,
    routing::{get, post},
    Json, Router,
};
use chrono::{DateTime, Utc};
use config::{Config, PublicConfig};
use discord::DiscordClient;
use futures_util::{SinkExt, StreamExt};
use kalshi::{KalshiClient, MarketDetails, MarketPositionSnapshot};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use serde_json::json;
use state::{PersistedState, StateStore};
use tokio::{
    net::TcpListener,
    sync::{watch, Mutex},
    time::sleep,
};
use tokio_tungstenite::tungstenite::Message;
use tracing::{error, info, warn};

const COMMAND_CATALOG_VERSION: u8 = 1;
const COMMAND_CATALOG_SERVICE: &str = "kalshi-spoke";
const COMMAND_NAME_STATUS: &str = "kalshi-status";
const COMMAND_NAME_POSITIONS: &str = "kalshi-positions";
const COMMAND_MESSAGE_MAX_CHARS: usize = 1800;
const MARKET_DETAILS_CACHE_TTL: Duration = Duration::from_secs(600);

#[derive(Clone)]
struct AppState {
    started_at: DateTime<Utc>,
    config: PublicConfig,
    kalshi: Arc<KalshiClient>,
    market_details_cache: Arc<MarketDetailsCache>,
    runtime: Arc<RuntimeState>,
    persisted: Arc<StateStore>,
}

struct MarketDetailsCache {
    ttl: Duration,
    entries: Mutex<BTreeMap<String, CachedMarketDetails>>,
}

#[derive(Clone)]
struct CachedMarketDetails {
    fetched_at: DateTime<Utc>,
    details: MarketDetails,
}

struct RuntimeState {
    inner: Mutex<RuntimeSnapshot>,
}

#[derive(Debug, Clone, Default, Serialize)]
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

#[derive(Debug, Serialize)]
struct CommandCatalogResponse {
    version: u8,
    service: &'static str,
    commands: Vec<CommandDefinition>,
    #[serde(rename = "commandNames")]
    command_names: Vec<String>,
}

#[derive(Debug, Serialize)]
struct CommandDefinition {
    name: &'static str,
    description: &'static str,
}

#[derive(Debug, Deserialize)]
struct CommandRequest {
    command: String,
    context: CommandContext,
    #[allow(dead_code)]
    options: Option<serde_json::Map<String, serde_json::Value>>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct CommandContext {
    discord_user_id: String,
    #[allow(dead_code)]
    guild_id: Option<String>,
    #[allow(dead_code)]
    channel_id: Option<String>,
}

#[derive(Debug, Serialize)]
struct CommandResponse {
    status: &'static str,
    command: String,
    message: String,
    data: serde_json::Value,
}

#[derive(Debug, Serialize)]
struct PositionsSummary {
    subaccount: u32,
    yes_contracts: String,
    no_contracts: String,
    yes_markets: usize,
    no_markets: usize,
    markets: Vec<MarketPositionView>,
}

#[derive(Debug, Serialize)]
struct MarketPositionView {
    ticker: String,
    event_ticker: Option<String>,
    title: Option<String>,
    prompt: Option<String>,
    side: &'static str,
    contracts: String,
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

impl MarketDetailsCache {
    fn new(ttl: Duration) -> Self {
        Self {
            ttl,
            entries: Mutex::new(BTreeMap::new()),
        }
    }

    async fn get(&self, ticker: &str) -> Option<MarketDetails> {
        let now = Utc::now();
        let guard = self.entries.lock().await;
        let entry = guard.get(ticker)?;

        let age = now.signed_duration_since(entry.fetched_at);
        if age.num_seconds() < 0 || age.num_seconds() as u64 > self.ttl.as_secs() {
            return None;
        }

        Some(entry.details.clone())
    }

    async fn put(&self, details: MarketDetails) {
        let key = details.ticker.trim().to_string();
        if key.is_empty() {
            return;
        }

        self.entries.lock().await.insert(
            key,
            CachedMarketDetails {
                fetched_at: Utc::now(),
                details,
            },
        );
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
        config.hub_notify_auth_token.clone(),
        config.notify_channel.clone(),
        config.notify_severity.clone(),
    ));

    let kalshi = Arc::new(KalshiClient::new(
        http_client.clone(),
        config.kalshi_api_base_url.clone(),
        config.kalshi_ws_url.clone(),
        config.kalshi_access_key.clone(),
        &config.kalshi_private_key_path,
    )?);

    let app_state = Arc::new(AppState {
        started_at: Utc::now(),
        config: config.public(),
        kalshi: kalshi.clone(),
        market_details_cache: Arc::new(MarketDetailsCache::new(MARKET_DETAILS_CACHE_TTL)),
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
        let kalshi = kalshi.clone();

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
    Json(json!({ "status": "ok" }))
}

async fn status(State(state): State<Arc<AppState>>) -> Json<StatusResponse> {
    Json(build_status_response(state).await)
}

async fn control_commands() -> Json<CommandCatalogResponse> {
    Json(CommandCatalogResponse {
        version: COMMAND_CATALOG_VERSION,
        service: COMMAND_CATALOG_SERVICE,
        commands: vec![
            CommandDefinition {
                name: COMMAND_NAME_STATUS,
                description: "Show Kalshi monitor runtime and persisted state",
            },
            CommandDefinition {
                name: COMMAND_NAME_POSITIONS,
                description: "Show YES and NO contract exposure by market",
            },
        ],
        command_names: vec![
            COMMAND_NAME_STATUS.to_string(),
            COMMAND_NAME_POSITIONS.to_string(),
        ],
    })
}

async fn control_command(
    State(state): State<Arc<AppState>>,
    Json(request): Json<CommandRequest>,
) -> Result<Json<CommandResponse>, (StatusCode, String)> {
    if request.context.discord_user_id.trim().is_empty() {
        return Err((
            StatusCode::BAD_REQUEST,
            "context.discordUserId is required".to_string(),
        ));
    }

    let command = request.command.trim().to_ascii_lowercase();
    match command.as_str() {
        COMMAND_NAME_STATUS => {
            let status_payload = build_status_response(state).await;
            let message = format!(
                "Kalshi status: enabled={}, websocketConnected={}, trackedMarkets={}.",
                status_payload.config.enabled,
                status_payload.runtime.ws_connected,
                status_payload.runtime.markets.len()
            );

            let data = serde_json::to_value(status_payload)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        COMMAND_NAME_POSITIONS => {
            let summary = build_positions_summary(state)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;
            let message = build_positions_message(&summary);

            let data = serde_json::to_value(summary)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        _ => Err((
            StatusCode::BAD_REQUEST,
            format!(
                "unknown command {:?}; valid commands: {}, {}",
                request.command, COMMAND_NAME_STATUS, COMMAND_NAME_POSITIONS
            ),
        )),
    }
}

async fn build_positions_summary(state: Arc<AppState>) -> Result<PositionsSummary> {
    let positions = state
        .kalshi
        .fetch_market_positions(state.config.subaccount)
        .await
        .context("failed to load market positions from Kalshi")?;

    let details_by_ticker = resolve_market_details(state.as_ref(), &positions).await;

    let mut yes_total = Decimal::ZERO;
    let mut no_total = Decimal::ZERO;
    let mut yes_markets = 0usize;
    let mut no_markets = 0usize;
    let mut markets = Vec::new();

    for position in positions {
        if position.position_fp == Decimal::ZERO {
            continue;
        }

        let side = if position.position_fp > Decimal::ZERO {
            yes_total += position.position_fp;
            yes_markets += 1;
            "YES"
        } else {
            no_total += position.position_fp.abs();
            no_markets += 1;
            "NO"
        };

        let details = details_by_ticker.get(&position.ticker);
        let prompt = details.and_then(|entry| {
            let text = if side == "YES" {
                entry.yes_sub_title.trim()
            } else {
                entry.no_sub_title.trim()
            };

            if text.is_empty() {
                None
            } else {
                Some(text.to_string())
            }
        });

        markets.push(MarketPositionView {
            ticker: position.ticker.clone(),
            event_ticker: details.map(|entry| entry.event_ticker.clone()),
            title: details.and_then(|entry| {
                let title = entry.title.trim();
                if title.is_empty() {
                    None
                } else {
                    Some(title.to_string())
                }
            }),
            prompt,
            side,
            contracts: format_decimal_2(position.position_fp.abs()),
        });
    }

    markets.sort_by(|left, right| {
        left.side
            .cmp(right.side)
            .then_with(|| left.ticker.cmp(&right.ticker))
    });

    Ok(PositionsSummary {
        subaccount: state.config.subaccount,
        yes_contracts: format_decimal_2(yes_total),
        no_contracts: format_decimal_2(no_total),
        yes_markets,
        no_markets,
        markets,
    })
}

fn build_positions_message(summary: &PositionsSummary) -> String {
    if summary.markets.is_empty() {
        return format!(
            "Kalshi positions (subaccount {}): no open YES/NO contracts.",
            summary.subaccount
        );
    }

    let mut lines = vec![format!(
        "Kalshi positions (subaccount {})",
        summary.subaccount
    )];
    lines.push(format!(
        "Totals: YES {} | NO {} | markets {}/{}",
        summary.yes_contracts, summary.no_contracts, summary.yes_markets, summary.no_markets
    ));

    let max_markets = 6usize;
    for (index, market) in summary.markets.iter().take(max_markets).enumerate() {
        lines.extend(format_market_lines(index + 1, market));
    }

    if summary.markets.len() > max_markets {
        lines.push(format!(
            "... and {} more position(s)",
            summary.markets.len() - max_markets
        ));
    }

    truncate_message(lines.join("\n"), COMMAND_MESSAGE_MAX_CHARS)
}

fn format_market_lines(index: usize, market: &MarketPositionView) -> Vec<String> {
    let title = market
        .title
        .as_deref()
        .filter(|value| !value.trim().is_empty())
        .unwrap_or("(title unavailable)");

    let mut lines = vec![format!(
        "{}. [{} {}] {}",
        index, market.side, market.contracts, title
    )];

    lines.push(format!("   ticker: {}", market.ticker));

    if let Some(event_ticker) = market.event_ticker.as_deref() {
        lines.push(format!("   event: {}", event_ticker));
    }

    if let Some(prompt) = market.prompt.as_deref() {
        if !prompt.trim().is_empty() {
            lines.push(format!("   prompt: {}", prompt));
        }
    }

    lines
}

fn truncate_message(input: String, limit: usize) -> String {
    if input.chars().count() <= limit {
        return input;
    }

    let mut truncated: String = input.chars().take(limit.saturating_sub(1)).collect();
    truncated.push('…');
    truncated
}

async fn resolve_market_details(
    state: &AppState,
    positions: &[MarketPositionSnapshot],
) -> BTreeMap<String, MarketDetails> {
    let mut tickers = BTreeSet::new();
    for position in positions {
        let ticker = position.ticker.trim();
        if !ticker.is_empty() {
            tickers.insert(ticker.to_string());
        }
    }

    let mut details = BTreeMap::new();
    for ticker in tickers {
        if let Some(cached) = state.market_details_cache.get(&ticker).await {
            details.insert(ticker, cached);
            continue;
        }

        match state.kalshi.fetch_market_details(&ticker).await {
            Ok(fetched) => {
                state.market_details_cache.put(fetched.clone()).await;
                details.insert(ticker, fetched);
            }
            Err(err) => {
                warn!("failed to fetch market details for {}: {err:#}", ticker);
            }
        }
    }

    details
}

async fn build_status_response(state: Arc<AppState>) -> StatusResponse {
    StatusResponse {
        status: "ok",
        started_at: state.started_at,
        config: state.config.clone(),
        runtime: state.runtime.snapshot().await,
        persisted: state.persisted.snapshot().await,
    }
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
            runtime
                .record_ws_error(format!("code={} msg={}", code, message))
                .await;
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
    runtime
        .record_ticker_price(market_ticker, yes_bid_str.clone())
        .await;

    let is_above = yes_bid >= config.trigger_yes_bid_dollars;
    if !persisted.has_market(market_ticker).await {
        persisted
            .update_market(market_ticker, |state| {
                state.was_above_threshold = is_above;
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some("initialized market state".to_string());
            })
            .await?;
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

        persisted
            .update_market(market_ticker, |state| {
                state.was_above_threshold = true;
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_triggered_at = Some(Utc::now());
                state.last_client_order_id = client_order_id.clone();
                state.last_action = Some(action_summary.clone());
            })
            .await?;
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

        persisted
            .update_market(market_ticker, |state| {
                state.was_above_threshold = false;
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some("trigger re-armed".to_string());
            })
            .await?;
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
