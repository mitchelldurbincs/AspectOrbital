use anyhow::Result;
use rust_decimal::Decimal;
use tracing::{info, warn};

use crate::{
    app::RuntimeState,
    config::Config,
    discord::DiscordClient,
    formatting::format_decimal_4,
    kalshi::{KalshiClient, OrderSnapshot},
    models::TickerPayload,
    state::{PersistedTriggerRule, StateStore, TriggerRulePhase},
    trading::{attempt_sell, build_client_order_id},
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

    let rule = persisted.active_rule_for_market(market_ticker).await;
    let threshold = active_rule_threshold(rule.as_ref())?;
    let is_above = threshold.map(|value| yes_bid >= value).unwrap_or(false);

    if !persisted.has_market(market_ticker).await {
        persisted
            .update_market(market_ticker, |state| {
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some(if threshold.is_some() {
                    "initialized market state (rule-enabled)".to_string()
                } else {
                    "initialized market state (observe-only: no rule configured)".to_string()
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

    if threshold.is_none() {
        persisted
            .update_market(market_ticker, |state| {
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some("observe-only update (rule unset)".to_string());
            })
            .await?;
        return Ok(());
    }

    let Some(rule) = rule else {
        return Ok(());
    };
    let threshold = threshold.expect("checked threshold exists");

    if rule.state.phase == TriggerRulePhase::SellPending {
        let action_summary =
            reconcile_pending_sell(config, kalshi, persisted, &rule, market_ticker).await?;
        persisted
            .update_market(market_ticker, |state| {
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some(action_summary.clone());
            })
            .await?;
        return Ok(());
    }

    if !rule.state.was_condition_true && is_above {
        let threshold = format_decimal_4(threshold);
        let trigger_message = format!(
            "[trigger] {} YES bid {} crossed above {}",
            market_ticker, yes_bid_str, threshold
        );
        info!("{}", trigger_message);
        if let Err(err) = discord.notify(&trigger_message, Some("info")).await {
            warn!("failed to deliver trigger alert to discord-hub: {err:#}");
        }

        let client_order_id = config
            .auto_sell_enabled
            .then(|| build_client_order_id(market_ticker));
        let action_summary = if config.auto_sell_enabled {
            let pending_action = format!("sell pending reconciliation at threshold {}", threshold);
            persisted
                .mark_rule_sell_pending(&rule.spec.id, client_order_id.clone(), pending_action)
                .await?;

            let outcome = attempt_sell(
                config,
                kalshi,
                discord,
                market_ticker,
                &threshold,
                client_order_id
                    .as_deref()
                    .expect("client order id exists when auto-sell enabled"),
            )
            .await?;
            persisted
                .mark_rule_triggered(
                    &rule.spec.id,
                    outcome.client_order_id.clone(),
                    outcome.summary.clone(),
                )
                .await?;
            outcome.summary
        } else {
            persisted
                .mark_rule_triggered(
                    &rule.spec.id,
                    rule.state.last_client_order_id.clone(),
                    "trigger fired (auto-sell disabled)".to_string(),
                )
                .await?;
            "trigger fired (auto-sell disabled)".to_string()
        };

        persisted
            .update_market(market_ticker, |state| {
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some(action_summary.clone());
            })
            .await?;
        return Ok(());
    }

    if rule.state.was_condition_true && !is_above {
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
            .mark_rule_rearmed(&rule.spec.id, "trigger re-armed".to_string())
            .await?;
        persisted
            .update_market(market_ticker, |state| {
                state.last_yes_bid_dollars = Some(yes_bid_str.clone());
                state.last_action = Some("trigger re-armed".to_string());
            })
            .await?;
    }

    Ok(())
}

async fn reconcile_pending_sell(
    config: &Config,
    kalshi: &KalshiClient,
    persisted: &StateStore,
    rule: &PersistedTriggerRule,
    market_ticker: &str,
) -> Result<String> {
    let pending_client_order_id = rule.state.pending_client_order_id.clone();
    if let Some(client_order_id) = pending_client_order_id.as_deref() {
        if let Some(order) = kalshi
            .fetch_order_by_client_order_id(market_ticker, config.subaccount, client_order_id)
            .await?
        {
            match classify_pending_order(&order) {
                PendingSellResolution::Completed(action_summary) => {
                    persisted
                        .mark_rule_triggered(
                            &rule.spec.id,
                            Some(order.client_order_id.clone()),
                            action_summary.clone(),
                        )
                        .await?;
                    return Ok(action_summary);
                }
                PendingSellResolution::StillPending(action_summary) => return Ok(action_summary),
            }
        }
    }

    let position_fp = kalshi
        .fetch_yes_position(market_ticker, config.subaccount)
        .await?;

    if position_fp <= Decimal::ZERO {
        let action_summary = format!(
            "pending sell reconciled; no YES position remains{}",
            pending_client_order_id
                .as_deref()
                .map(|value| format!(" (client_order_id={})", value))
                .unwrap_or_default()
        );
        persisted
            .mark_rule_triggered(
                &rule.spec.id,
                pending_client_order_id,
                action_summary.clone(),
            )
            .await?;
        return Ok(action_summary);
    }

    Ok(format!(
        "sell still pending reconciliation; order lookup not yet conclusive and YES position remains {}",
        format_decimal_4(position_fp)
    ))
}

enum PendingSellResolution {
    Completed(String),
    StillPending(String),
}

fn classify_pending_order(order: &OrderSnapshot) -> PendingSellResolution {
    let status = order.status.trim().to_ascii_lowercase();
    let summary = format!(
        "pending sell reconciled via Kalshi order lookup: client_order_id={} order_id={} status={} filled={} remaining={}",
        order.client_order_id,
        order.order_id,
        order.status.trim(),
        order.fill_count_fp.trim(),
        order.remaining_count_fp.trim(),
    );

    if status == "executed" || status == "canceled" {
        return PendingSellResolution::Completed(summary);
    }

    PendingSellResolution::StillPending(format!(
        "sell still pending reconciliation; Kalshi order {} is {} with filled={} remaining={}",
        order.order_id,
        order.status.trim(),
        order.fill_count_fp.trim(),
        order.remaining_count_fp.trim(),
    ))
}

fn active_rule_threshold(rule: Option<&PersistedTriggerRule>) -> Result<Option<Decimal>> {
    match rule {
        Some(rule) => Ok(Some(rule.spec.threshold_decimal()?)),
        None => Ok(None),
    }
}

#[cfg(test)]
mod tests {
    use super::{classify_pending_order, PendingSellResolution};
    use crate::kalshi::OrderSnapshot;

    #[test]
    fn classify_pending_order_marks_completed_statuses_as_terminal() {
        let executed = OrderSnapshot {
            order_id: "order-1".to_string(),
            client_order_id: "client-1".to_string(),
            status: "executed".to_string(),
            fill_count_fp: "10.00".to_string(),
            remaining_count_fp: "0.00".to_string(),
        };
        let canceled = OrderSnapshot {
            status: "canceled".to_string(),
            ..executed.clone()
        };

        assert!(matches!(
            classify_pending_order(&executed),
            PendingSellResolution::Completed(_)
        ));
        assert!(matches!(
            classify_pending_order(&canceled),
            PendingSellResolution::Completed(_)
        ));
    }

    #[test]
    fn classify_pending_order_keeps_resting_orders_pending() {
        let order = OrderSnapshot {
            order_id: "order-1".to_string(),
            client_order_id: "client-1".to_string(),
            status: "resting".to_string(),
            fill_count_fp: "2.00".to_string(),
            remaining_count_fp: "8.00".to_string(),
        };

        assert!(matches!(
            classify_pending_order(&order),
            PendingSellResolution::StillPending(_)
        ));
    }
}
