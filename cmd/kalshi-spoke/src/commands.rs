use std::{str::FromStr, sync::Arc};

use axum::{
    extract::State,
    http::{HeaderMap, StatusCode},
    Json,
};
use rust_decimal::Decimal;

use crate::{
    app::AppState,
    formatting::format_decimal_4,
    http::authorize,
    models::{
        CommandCatalogResponse, CommandDefinition, CommandOptionDefinition, CommandOptionType,
        CommandRequest, CommandResponse,
    },
    rules::TriggerRule,
    summaries::{
        build_positions_message, build_positions_summary, build_rules_summary,
        build_status_response,
    },
};

const COMMAND_CATALOG_VERSION: u8 = 1;
const COMMAND_CATALOG_SERVICE: &str = "kalshi-spoke";
const COMMAND_NAME_STATUS: &str = "kalshi-status";
const COMMAND_NAME_POSITIONS: &str = "kalshi-positions";
const COMMAND_NAME_RULES: &str = "kalshi-rules";
const COMMAND_NAME_RULE_SET: &str = "kalshi-rule-set";
const COMMAND_NAME_RULE_REMOVE: &str = "kalshi-rule-remove";
const OPTION_NAME_TICKER: &str = "ticker";
const OPTION_NAME_THRESHOLD_DOLLARS: &str = "threshold_dollars";

fn known_command_names() -> [&'static str; 5] {
    [
        COMMAND_NAME_STATUS,
        COMMAND_NAME_POSITIONS,
        COMMAND_NAME_RULES,
        COMMAND_NAME_RULE_SET,
        COMMAND_NAME_RULE_REMOVE,
    ]
}

pub(crate) async fn control_commands() -> Json<CommandCatalogResponse> {
    Json(CommandCatalogResponse {
        version: COMMAND_CATALOG_VERSION,
        service: COMMAND_CATALOG_SERVICE,
        commands: vec![
            CommandDefinition {
                name: COMMAND_NAME_STATUS,
                description: "Show Kalshi monitor runtime and persisted state",
                options: vec![],
            },
            CommandDefinition {
                name: COMMAND_NAME_POSITIONS,
                description: "Show YES and NO contract exposure by market",
                options: vec![],
            },
            CommandDefinition {
                name: COMMAND_NAME_RULES,
                description: "Show trigger-enabled and observe-only market rules",
                options: vec![],
            },
            CommandDefinition {
                name: COMMAND_NAME_RULE_SET,
                description: "Set a YES bid crossing rule for a market ticker",
                options: vec![
                    CommandOptionDefinition {
                        name: OPTION_NAME_TICKER,
                        option_type: CommandOptionType::String,
                        description: "Market ticker, e.g. PRES24",
                        required: true,
                    },
                    CommandOptionDefinition {
                        name: OPTION_NAME_THRESHOLD_DOLLARS,
                        option_type: CommandOptionType::Number,
                        description: "Trigger threshold, between 0 and 1",
                        required: true,
                    },
                ],
            },
            CommandDefinition {
                name: COMMAND_NAME_RULE_REMOVE,
                description: "Remove a market rule and switch to observe-only",
                options: vec![CommandOptionDefinition {
                    name: OPTION_NAME_TICKER,
                    option_type: CommandOptionType::String,
                    description: "Market ticker, e.g. PRES24",
                    required: true,
                }],
            },
        ],
        command_names: vec![
            COMMAND_NAME_STATUS.to_string(),
            COMMAND_NAME_POSITIONS.to_string(),
            COMMAND_NAME_RULES.to_string(),
            COMMAND_NAME_RULE_SET.to_string(),
            COMMAND_NAME_RULE_REMOVE.to_string(),
        ],
    })
}

pub(crate) async fn control_command(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(request): Json<CommandRequest>,
) -> Result<Json<CommandResponse>, (StatusCode, String)> {
    authorize(&headers, state.spoke_command_auth_token.as_str())?;

    if request.context.discord_user_id.trim().is_empty() {
        return Err((
            StatusCode::BAD_REQUEST,
            "context.discordUserId is required".to_string(),
        ));
    }

    let command = request.command.clone();
    match command.as_str() {
        COMMAND_NAME_STATUS => {
            let status_payload = build_status_response(state).await;
            let message = format!(
                "Kalshi status: enabled={}, websocketConnected={}, trackedMarkets={}.",
                status_payload.config.enabled,
                status_payload.runtime.ws_connected,
                status_payload.runtime.markets.len()
            );

            let data = serde_json::to_value(status_payload)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        COMMAND_NAME_POSITIONS => {
            let summary = build_positions_summary(state)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;
            let message = build_positions_message(&summary);

            let data = serde_json::to_value(summary)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        COMMAND_NAME_RULES => {
            let summary = build_rules_summary(state)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;
            let message = format!(
                "Kalshi rules: trigger-enabled={} observe-only={} total={}",
                summary.trigger_enabled_markets,
                summary.observe_only_markets,
                summary.total_markets,
            );

            let data = serde_json::to_value(summary)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        COMMAND_NAME_RULE_SET => {
            let ticker = option_required(&request, OPTION_NAME_TICKER)
                .map_err(|message| (StatusCode::BAD_REQUEST, message))?;
            let threshold = option_required_decimal(&request, OPTION_NAME_THRESHOLD_DOLLARS)
                .map_err(|message| (StatusCode::BAD_REQUEST, message))?;

            if threshold <= Decimal::ZERO || threshold >= Decimal::ONE {
                return Err((
                    StatusCode::BAD_REQUEST,
                    "threshold_dollars must be between 0 and 1".to_string(),
                ));
            }

            let rule = TriggerRule::yes_bid_crosses_above(&ticker, threshold)
                .map_err(|err| (StatusCode::BAD_REQUEST, err.to_string()))?;

            state
                .persisted
                .set_yes_bid_rule(rule)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            let tracked = state
                .config
                .market_tickers
                .iter()
                .any(|entry| entry.eq_ignore_ascii_case(&ticker));
            let message = if tracked {
                format!(
                    "Rule set for {}: yes bid crosses above {} (trigger-enabled).",
                    ticker,
                    format_decimal_4(threshold)
                )
            } else {
                format!(
                    "Rule stored for {}: yes bid crosses above {}. This ticker is not currently in KALSHI_MARKET_TICKERS.",
                    ticker,
                    format_decimal_4(threshold)
                )
            };

            let summary = build_rules_summary(state)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;
            let data = serde_json::to_value(summary)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        COMMAND_NAME_RULE_REMOVE => {
            let ticker = option_required(&request, OPTION_NAME_TICKER)
                .map_err(|message| (StatusCode::BAD_REQUEST, message))?;

            let removed = state
                .persisted
                .remove_rules_for_market(&ticker)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            let message = if removed {
                format!(
                    "Rule removed for {}. Market is now observe-only (no trigger actions).",
                    ticker
                )
            } else {
                format!(
                    "No stored rule found for {}. Market remains observe-only.",
                    ticker
                )
            };

            let summary = build_rules_summary(state)
                .await
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;
            let data = serde_json::to_value(summary)
                .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

            Ok(Json(CommandResponse {
                status: "ok",
                command,
                message,
                data,
            }))
        }
        _ => Err((
            StatusCode::BAD_REQUEST,
            format!(
                "unknown command {:?}; valid commands: {}, {}, {}, {}, {}",
                request.command,
                known_command_names()[0],
                known_command_names()[1],
                known_command_names()[2],
                known_command_names()[3],
                known_command_names()[4],
            ),
        )),
    }
}

fn option_required(
    request: &CommandRequest,
    option_name: &str,
) -> std::result::Result<String, String> {
    let Some(options) = request.options.as_ref() else {
        return Err(format!("options.{} is required", option_name));
    };

    let Some(value) = options.get(option_name) else {
        return Err(format!("options.{} is required", option_name));
    };

    let raw = value
        .as_str()
        .map(ToString::to_string)
        .unwrap_or_else(|| value.to_string());
    if raw.trim() != raw {
        return Err(format!(
            "options.{} must not include leading or trailing spaces",
            option_name
        ));
    }
    let normalized = raw.trim_matches('"').to_string();
    if normalized.is_empty() {
        return Err(format!("options.{} cannot be empty", option_name));
    }

    Ok(normalized)
}

fn option_required_decimal(
    request: &CommandRequest,
    option_name: &str,
) -> std::result::Result<Decimal, String> {
    let raw = option_required(request, option_name)?;
    Decimal::from_str(raw.as_str())
        .map_err(|_| format!("options.{} must be a valid decimal number", option_name))
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    fn test_request(command: &str, options: serde_json::Value) -> CommandRequest {
        CommandRequest {
            command: command.to_string(),
            context: crate::models::CommandContext {
                discord_user_id: "123456789012345678".to_string(),
                guild_id: None,
                channel_id: None,
            },
            options: options.as_object().cloned(),
        }
    }

    #[test]
    fn option_required_rejects_surrounding_whitespace() {
        let request = test_request(
            COMMAND_NAME_RULE_SET,
            json!({OPTION_NAME_TICKER: " INX-TEST-1 ", OPTION_NAME_THRESHOLD_DOLLARS: "0.61"}),
        );

        let err = option_required(&request, OPTION_NAME_TICKER).expect_err("ticker should fail");
        assert_eq!(err, "options.ticker must not include leading or trailing spaces");
    }

    #[test]
    fn option_required_decimal_rejects_surrounding_whitespace() {
        let request = test_request(
            COMMAND_NAME_RULE_SET,
            json!({OPTION_NAME_TICKER: "INX-TEST-1", OPTION_NAME_THRESHOLD_DOLLARS: " 0.61 "}),
        );

        let err = option_required_decimal(&request, OPTION_NAME_THRESHOLD_DOLLARS)
            .expect_err("threshold should fail");
        assert_eq!(
            err,
            "options.threshold_dollars must not include leading or trailing spaces"
        );
    }

    #[test]
    fn known_command_names_rejects_legacy_command_name() {
        assert!(!known_command_names().contains(&"kalshi-thresholds"));
    }

    #[test]
    fn known_command_names_rejects_mixed_case_command_name() {
        assert!(!known_command_names().contains(&"Kalshi-rules"));
    }

    #[test]
    fn option_required_decimal_rejects_legacy_option_alias() {
        let request = test_request(
            COMMAND_NAME_RULE_SET,
            json!({OPTION_NAME_TICKER: "INX-TEST-1", "yes_bid_dollars": 0.61}),
        );

        let err = option_required_decimal(&request, OPTION_NAME_THRESHOLD_DOLLARS)
            .expect_err("legacy option alias should fail");
        assert_eq!(err, "options.threshold_dollars is required");
    }
}
