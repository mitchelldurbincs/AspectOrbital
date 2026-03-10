mod app;
mod commands;
mod config;
mod discord;
mod formatting;
mod http;
mod kalshi;
mod models;
mod rules;
mod state;
mod summaries;
mod trading;
mod triggers;
mod ws;

use std::{io::ErrorKind, sync::Arc, time::Duration};

use anyhow::{Context, Result};
use app::{AppState, MarketDetailsCache, RuntimeState};
use chrono::Utc;
use config::Config;
use discord::DiscordClient;
use kalshi::KalshiClient;
use state::StateStore;
use tokio::sync::watch;
use tracing::{error, info, warn};

use crate::{http::run_http_server, ws::run_ws_loop};

const MARKET_DETAILS_CACHE_TTL: Duration = Duration::from_secs(600);

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
    let seeded_rules = persisted
        .bootstrap_rules_if_empty(&config.bootstrap_trigger_rules)
        .await
        .context("failed to bootstrap persisted rule config")?;
    if seeded_rules > 0 {
        info!(
            "bootstrapped {} market rule(s) from KALSHI_TRIGGER_RULES",
            seeded_rules
        );
    }

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
        spoke_command_auth_token: config.spoke_command_auth_token.clone(),
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

        let active_rules = persisted
            .snapshot()
            .await
            .trigger_rules
            .values()
            .filter(|rule| {
                rule.spec.enabled
                    && config
                        .market_tickers
                        .iter()
                        .any(|tracked| tracked.eq_ignore_ascii_case(&rule.spec.ticker))
            })
            .count();
        let online_message = format!(
            "kalshi-spoke online: tickers={} trigger_enabled={} observe_only={} auto_sell={} dry_run={} subaccount={}",
            config.market_tickers.join(","),
            active_rules,
            config.market_tickers.len().saturating_sub(active_rules),
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
    let path = ".env";
    if let Err(err) = dotenvy::from_filename(path) {
        match err {
            dotenvy::Error::Io(io_err) if io_err.kind() == ErrorKind::NotFound => {}
            _ => eprintln!("warning: unable to load {}: {}", path, err),
        }
    }
}
