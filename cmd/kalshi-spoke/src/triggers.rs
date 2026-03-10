use anyhow::Result;
use chrono::Utc;
use rust_decimal::Decimal;
use tracing::{info, warn};

use crate::{
    app::RuntimeState,
    config::Config,
    discord::DiscordClient,
    formatting::format_decimal_4,
    kalshi::KalshiClient,
    models::TickerPayload,
    state::StateStore,
    trading::attempt_sell,
};

pub(crate) async fn process_ticker_update(
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

    let threshold = resolve_threshold_yes_bid_dollars_for_update(config, persisted, market_ticker).await?;
    let is_above = threshold.map(|value| yes_bid >= value).unwrap_or(false);

    if !persisted.has_market(market_ticker).await {
        persisted
            .update_market(market_ticker, |state| {
                state.was_above_threshold = is_above;
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some(if threshold.is_some() {
                    "initialized market state (trigger-enabled)".to_string()
                } else {
                    "initialized market state (observe-only: no threshold configured)".to_string()
                });
            })
            .await?;
        info!(
            "initialized state for {} at yes_bid={} (trigger_enabled={} above_threshold={})",
            market_ticker,
            yes_bid_str,
            threshold.is_some(),
            is_above
        );
        return Ok(());
    }

    let previous = persisted.market_snapshot(market_ticker).await;

    if threshold.is_none() {
        persisted
            .update_market(market_ticker, |state| {
                state.was_above_threshold = false;
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some("observe-only update (threshold unset)".to_string());
            })
            .await?;
        return Ok(());
    }

    let threshold = threshold.expect("checked threshold exists");

    if !previous.was_above_threshold && is_above {
        let threshold = format_decimal_4(threshold);
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
            let outcome = attempt_sell(config, kalshi, discord, market_ticker, &threshold).await?;
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
        let threshold = format_decimal_4(threshold);
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

pub(crate) async fn resolve_threshold_yes_bid_dollars_for_update(
    config: &Config,
    persisted: &StateStore,
    market_ticker: &str,
) -> Result<Option<Decimal>> {
    let from_persisted = persisted
        .market_threshold_yes_bid_dollars(market_ticker)
        .await?;
    if from_persisted.is_some() {
        return Ok(from_persisted);
    }

    Ok(config.market_threshold_decimal(market_ticker))
}
