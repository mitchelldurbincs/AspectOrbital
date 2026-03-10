use std::str::FromStr;

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::{app::RuntimeSnapshot, config::PublicConfig, state::PersistedState};

#[derive(Debug, Serialize)]
pub(crate) struct StatusResponse {
    pub(crate) status: &'static str,
    pub(crate) started_at: DateTime<Utc>,
    pub(crate) config: PublicConfig,
    pub(crate) runtime: RuntimeSnapshot,
    pub(crate) persisted: PersistedState,
}

#[derive(Debug, Serialize)]
pub(crate) struct CommandCatalogResponse {
    pub(crate) version: u8,
    pub(crate) service: &'static str,
    pub(crate) commands: Vec<CommandDefinition>,
    #[serde(rename = "commandNames")]
    pub(crate) command_names: Vec<String>,
}

#[derive(Debug, Serialize)]
pub(crate) struct CommandDefinition {
    pub(crate) name: &'static str,
    pub(crate) description: &'static str,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub(crate) options: Vec<CommandOptionDefinition>,
}

#[derive(Debug, Serialize)]
pub(crate) struct CommandOptionDefinition {
    pub(crate) name: &'static str,
    #[serde(rename = "type")]
    pub(crate) option_type: &'static str,
    pub(crate) description: &'static str,
    pub(crate) required: bool,
}

#[derive(Debug, Deserialize)]
pub(crate) struct CommandRequest {
    pub(crate) command: String,
    pub(crate) context: CommandContext,
    #[allow(dead_code)]
    pub(crate) options: Option<serde_json::Map<String, serde_json::Value>>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct CommandContext {
    pub(crate) discord_user_id: String,
    #[allow(dead_code)]
    pub(crate) guild_id: Option<String>,
    #[allow(dead_code)]
    pub(crate) channel_id: Option<String>,
}

#[derive(Debug, Serialize)]
pub(crate) struct CommandResponse {
    pub(crate) status: &'static str,
    pub(crate) command: String,
    pub(crate) message: String,
    pub(crate) data: serde_json::Value,
}

#[derive(Debug, Serialize)]
pub(crate) struct ThresholdsSummary {
    pub(crate) total_markets: usize,
    pub(crate) trigger_enabled_markets: usize,
    pub(crate) observe_only_markets: usize,
    pub(crate) markets: Vec<MarketThresholdView>,
}

#[derive(Debug, Serialize)]
pub(crate) struct MarketThresholdView {
    pub(crate) ticker: String,
    pub(crate) threshold_yes_bid_dollars: Option<String>,
    pub(crate) mode: &'static str,
    pub(crate) last_yes_bid_dollars: Option<String>,
}

#[derive(Debug, Serialize)]
pub(crate) struct PositionsSummary {
    pub(crate) subaccount: u32,
    pub(crate) yes_contracts: String,
    pub(crate) no_contracts: String,
    pub(crate) yes_markets: usize,
    pub(crate) no_markets: usize,
    pub(crate) markets: Vec<MarketPositionView>,
}

#[derive(Debug, Serialize)]
pub(crate) struct MarketPositionView {
    pub(crate) ticker: String,
    pub(crate) event_ticker: Option<String>,
    pub(crate) title: Option<String>,
    pub(crate) prompt: Option<String>,
    pub(crate) side: &'static str,
    pub(crate) contracts: String,
}

#[derive(Debug, Deserialize)]
pub(crate) struct TickerEnvelope {
    pub(crate) msg: TickerPayload,
}

#[derive(Debug, Deserialize)]
pub(crate) struct TickerPayload {
    pub(crate) market_ticker: String,
    #[serde(default)]
    pub(crate) yes_bid_dollars: Option<String>,
    #[serde(default)]
    pub(crate) yes_bid: Option<i64>,
}

#[derive(Debug, Deserialize)]
pub(crate) struct WsErrorEnvelope {
    #[serde(default)]
    pub(crate) msg: WsErrorMessage,
}

#[derive(Debug, Default, Deserialize)]
pub(crate) struct WsErrorMessage {
    #[serde(default)]
    pub(crate) code: Option<i64>,
    #[serde(default)]
    pub(crate) msg: Option<String>,
}

pub(crate) struct SellOutcome {
    pub(crate) summary: String,
    pub(crate) client_order_id: Option<String>,
}

impl TickerPayload {
    pub(crate) fn yes_bid_decimal(&self) -> Result<Option<Decimal>> {
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
