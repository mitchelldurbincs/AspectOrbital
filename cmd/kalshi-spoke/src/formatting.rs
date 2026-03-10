use rust_decimal::Decimal;

use crate::models::MarketPositionView;

pub(crate) fn format_market_lines(index: usize, market: &MarketPositionView) -> Vec<String> {
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

pub(crate) fn truncate_message(input: String, limit: usize) -> String {
    if input.chars().count() <= limit {
        return input;
    }

    let mut truncated: String = input.chars().take(limit.saturating_sub(1)).collect();
    truncated.push('…');
    truncated
}

pub(crate) fn format_decimal_4(value: Decimal) -> String {
    format!("{:.4}", value.round_dp(4))
}

pub(crate) fn format_decimal_2(value: Decimal) -> String {
    format!("{:.2}", value.round_dp(2))
}
