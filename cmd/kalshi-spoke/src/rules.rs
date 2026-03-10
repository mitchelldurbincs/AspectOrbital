use anyhow::{bail, Result};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::formatting::format_decimal_4;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub(crate) enum TriggerRuleSide {
    Yes,
}

impl TriggerRuleSide {
    pub(crate) fn as_str(self) -> &'static str {
        match self {
            Self::Yes => "yes",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum TriggerPriceSource {
    Bid,
}

impl TriggerPriceSource {
    pub(crate) fn as_str(self) -> &'static str {
        match self {
            Self::Bid => "bid",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum TriggerDirection {
    CrossesAbove,
}

impl TriggerDirection {
    pub(crate) fn as_str(self) -> &'static str {
        match self {
            Self::CrossesAbove => "crosses_above",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub(crate) struct TriggerRule {
    pub(crate) id: String,
    pub(crate) ticker: String,
    pub(crate) side: TriggerRuleSide,
    pub(crate) price_source: TriggerPriceSource,
    pub(crate) direction: TriggerDirection,
    pub(crate) threshold_dollars: String,
    pub(crate) enabled: bool,
}

impl TriggerRule {
    pub(crate) fn yes_bid_crosses_above(ticker: &str, threshold_dollars: Decimal) -> Result<Self> {
        if threshold_dollars <= Decimal::ZERO || threshold_dollars >= Decimal::ONE {
            bail!("trigger threshold must be between 0 and 1")
        }

        let ticker = ticker.trim();
        if ticker.is_empty() {
            bail!("market ticker is required")
        }

        Ok(Self {
            id: build_rule_id(
                ticker,
                TriggerRuleSide::Yes,
                TriggerPriceSource::Bid,
                TriggerDirection::CrossesAbove,
            ),
            ticker: ticker.to_string(),
            side: TriggerRuleSide::Yes,
            price_source: TriggerPriceSource::Bid,
            direction: TriggerDirection::CrossesAbove,
            threshold_dollars: format_decimal_4(threshold_dollars),
            enabled: true,
        })
    }

    pub(crate) fn threshold_decimal(&self) -> Result<Decimal> {
        self.threshold_dollars
            .trim()
            .parse::<Decimal>()
            .map_err(Into::into)
    }

    pub(crate) fn matches_market(&self, market_ticker: &str) -> bool {
        self.ticker.eq_ignore_ascii_case(market_ticker)
    }
}

pub(crate) fn build_rule_id(
    ticker: &str,
    side: TriggerRuleSide,
    price_source: TriggerPriceSource,
    direction: TriggerDirection,
) -> String {
    format!(
        "{}:{}:{}:{}",
        ticker.trim().to_ascii_uppercase(),
        side.as_str(),
        price_source.as_str(),
        direction.as_str()
    )
}
