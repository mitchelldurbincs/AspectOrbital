use std::collections::{BTreeMap, BTreeSet};
use std::sync::Arc;

use anyhow::{Context, Result};
use rust_decimal::Decimal;
use tracing::warn;

use crate::{
    app::AppState,
    formatting::{format_decimal_2, format_market_lines, truncate_message},
    kalshi::{MarketDetails, MarketPositionSnapshot},
    models::{MarketPositionView, MarketRuleView, PositionsSummary, RulesSummary, StatusResponse},
};

const COMMAND_MESSAGE_MAX_CHARS: usize = 1800;

pub(crate) async fn build_positions_summary(state: Arc<AppState>) -> Result<PositionsSummary> {
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

pub(crate) fn build_positions_message(summary: &PositionsSummary) -> String {
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

pub(crate) async fn build_rules_summary(state: Arc<AppState>) -> Result<RulesSummary> {
    let persisted = state.persisted.snapshot().await;
    let runtime = state.runtime.snapshot().await;

    let mut markets = Vec::with_capacity(state.config.market_tickers.len());
    for ticker in &state.config.market_tickers {
        let rule = persisted
            .trigger_rules
            .values()
            .find(|entry| entry.spec.enabled && entry.spec.matches_market(ticker))
            .map(|entry| entry.spec.clone());
        let runtime_market = runtime.markets.get(ticker);
        let mode = if rule.is_some() {
            "trigger-enabled"
        } else {
            "observe-only"
        };

        markets.push(MarketRuleView {
            ticker: ticker.clone(),
            rule,
            mode,
            last_yes_bid_dollars: runtime_market
                .and_then(|entry| entry.last_yes_bid_dollars.clone())
                .or_else(|| {
                    persisted
                        .markets
                        .get(ticker)
                        .and_then(|entry| entry.last_yes_bid_dollars.clone())
                }),
        });
    }

    let trigger_enabled_markets = markets.iter().filter(|entry| entry.rule.is_some()).count();

    Ok(RulesSummary {
        total_markets: markets.len(),
        trigger_enabled_markets,
        observe_only_markets: markets.len().saturating_sub(trigger_enabled_markets),
        markets,
    })
}

pub(crate) async fn build_status_response(state: Arc<AppState>) -> StatusResponse {
    StatusResponse {
        status: "ok",
        started_at: state.started_at,
        config: state.config.clone(),
        runtime: state.runtime.snapshot().await,
        persisted: state.persisted.snapshot().await,
    }
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
