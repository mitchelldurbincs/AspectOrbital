use std::sync::Arc;

use anyhow::{Context, Result};
use futures_util::{SinkExt, StreamExt};
use serde_json::json;
use tokio::{sync::watch, time::sleep};
use tokio_tungstenite::tungstenite::Message;
use tracing::{info, warn};

use crate::{
    app::RuntimeState,
    config::Config,
    discord::DiscordClient,
    kalshi::KalshiClient,
    models::{TickerEnvelope, WsErrorEnvelope, WsErrorMessage},
    state::StateStore,
    triggers::process_ticker_update,
};

pub(crate) async fn run_ws_loop(
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
                warn!(
                    "ticker processing failed: {err:#}; state may be stale and pending sell reconciliation may require operator attention"
                );
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
