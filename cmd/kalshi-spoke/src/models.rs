use std::str::FromStr;

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::rules::TriggerRule;
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
    pub(crate) option_type: CommandOptionType,
    pub(crate) description: &'static str,
    pub(crate) required: bool,
}

#[derive(Debug, Clone, Copy, Serialize)]
#[allow(dead_code)]
#[serde(rename_all = "lowercase")]
pub(crate) enum CommandOptionType {
    String,
    Integer,
    Number,
    Boolean,
    Attachment,
}

#[derive(Debug, Deserialize)]
pub(crate) struct CommandRequest {
    pub(crate) command: String,
    pub(crate) context: CommandContext,
    #[allow(dead_code)]
    pub(crate) options: Option<serde_json::Map<String, serde_json::Value>>,
}

#[derive(Debug, Deserialize, Serialize)]
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
pub(crate) struct RulesSummary {
    pub(crate) total_markets: usize,
    pub(crate) trigger_enabled_markets: usize,
    pub(crate) observe_only_markets: usize,
    pub(crate) markets: Vec<MarketRuleView>,
}

#[derive(Debug, Serialize)]
pub(crate) struct MarketRuleView {
    pub(crate) ticker: String,
    pub(crate) rule: Option<TriggerRule>,
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

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::{
        CommandCatalogResponse, CommandContext, CommandDefinition, CommandOptionDefinition,
        CommandOptionType, CommandRequest,
    };

    const SPOKE_CONTRACT_SCHEMA_V2: &str =
        include_str!("../../../contracts/spoke-contract-v2.schema.json");

    fn schema_root() -> serde_json::Value {
        serde_json::from_str(SPOKE_CONTRACT_SCHEMA_V2).expect("schema JSON should parse")
    }

    fn schema_def(definition: &str) -> serde_json::Value {
        let root = schema_root();
        root["$defs"][definition].clone()
    }

    fn assert_schema_def_exists(definition: &str) {
        let root: serde_json::Value =
            serde_json::from_str(SPOKE_CONTRACT_SCHEMA_V2).expect("schema JSON should parse");
        assert!(
            root["$defs"].get(definition).is_some(),
            "schema should include {definition}"
        );
    }

    #[test]
    fn schema_contract_compat_command_catalog_payload() {
        assert_schema_def_exists("commandCatalog");

        let payload = serde_json::to_value(CommandCatalogResponse {
            version: 1,
            service: "kalshi-spoke",
            commands: vec![CommandDefinition {
                name: "kalshi-rule-set",
                description: "Set rule",
                options: vec![CommandOptionDefinition {
                    name: "threshold_dollars",
                    option_type: CommandOptionType::Number,
                    description: "Trigger threshold",
                    required: true,
                }],
            }],
            command_names: vec!["kalshi-rule-set".to_string()],
        })
        .expect("catalog should serialize");

        let command_catalog_def = schema_def("commandCatalog");
        assert_eq!(
            command_catalog_def["required"],
            json!(["version", "service", "commands"])
        );
        assert_eq!(payload["commandNames"], json!(["kalshi-rule-set"]));
        assert_eq!(payload["commands"][0]["options"][0]["type"], "number");
    }

    #[test]
    fn schema_contract_compat_command_request_payload() {
        assert_schema_def_exists("commandExecuteRequest");

        let payload = json!({
            "command": "kalshi-rule-set",
            "context": {
                "discordUserId": "123456789012345678",
                "guildId": "223456789012345678",
                "channelId": "323456789012345678"
            },
            "options": {
                "ticker": "PRES24",
                "threshold_dollars": 0.61
            }
        });

        let request_def = schema_def("commandExecuteRequest");
        assert_eq!(request_def["required"], json!(["command", "context"]));
        assert_eq!(request_def["additionalProperties"], false);

        let parsed: CommandRequest = serde_json::from_value(payload).expect("request should parse");
        assert_eq!(parsed.context.discord_user_id, "123456789012345678");
    }

    #[test]
    fn schema_contract_compat_command_context_serialization() {
        assert_schema_def_exists("commandContext");

        let payload = serde_json::to_value(CommandContext {
            discord_user_id: "123456789012345678".to_string(),
            guild_id: Some("223456789012345678".to_string()),
            channel_id: Some("323456789012345678".to_string()),
        })
        .expect("context should serialize");

        let context_def = schema_def("commandContext");
        assert_eq!(context_def["required"], json!(["discordUserId"]));
        assert!(payload.get("discordUserId").is_some());
        assert!(payload.get("discord_user_id").is_none());
    }

    #[test]
    fn schema_contract_compat_option_type_enum() {
        assert_schema_def_exists("optionType");

        let option_type_def = schema_def("optionType");
        assert_eq!(
            option_type_def["enum"],
            json!(["string", "integer", "number", "boolean", "attachment"])
        );

        let payload = serde_json::to_value(CommandOptionDefinition {
            name: "ticker",
            option_type: CommandOptionType::Attachment,
            description: "Upload a file",
            required: false,
        })
        .expect("option definition should serialize");
        assert_eq!(payload["type"], "attachment");
    }
}
