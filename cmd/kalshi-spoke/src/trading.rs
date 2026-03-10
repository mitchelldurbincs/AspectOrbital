use anyhow::{Context, Result};
use chrono::Utc;
use rust_decimal::Decimal;
use tracing::warn;

use crate::{
    config::Config, discord::DiscordClient, formatting::format_decimal_2, kalshi::KalshiClient,
    models::SellOutcome,
};

pub(crate) async fn attempt_sell(
    config: &Config,
    kalshi: &KalshiClient,
    discord: &DiscordClient,
    market_ticker: &str,
    yes_price: &str,
    client_order_id: &str,
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
            client_order_id: Some(client_order_id.to_string()),
        });
    }

    let order = kalshi
        .create_reduce_only_sell_order(
            market_ticker,
            &count_fp,
            yes_price,
            client_order_id,
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
        client_order_id: Some(client_order_id.to_string()),
    })
}

pub(crate) fn build_client_order_id(market_ticker: &str) -> String {
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
