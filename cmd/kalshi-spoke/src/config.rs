use std::{env, path::PathBuf, str::FromStr, time::Duration};

use anyhow::{anyhow, bail, Context, Result};
use rust_decimal::Decimal;

#[derive(Clone)]
pub struct Config {
    pub enabled: bool,
    pub http_addr: String,
    pub hub_notify_url: String,
    pub hub_notify_auth_token: String,
    pub notify_channel: String,
    pub notify_severity: String,
    pub state_file: PathBuf,
    pub kalshi_api_base_url: String,
    pub kalshi_ws_url: String,
    pub kalshi_access_key: String,
    pub kalshi_private_key_path: PathBuf,
    pub market_tickers: Vec<String>,
    pub trigger_yes_bid_dollars: Decimal,
    pub auto_sell_enabled: bool,
    pub dry_run: bool,
    pub subaccount: u32,
    pub http_timeout: Duration,
    pub ws_reconnect_delay: Duration,
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct PublicConfig {
    pub enabled: bool,
    pub http_addr: String,
    pub hub_notify_url: String,
    pub notify_channel: String,
    pub notify_severity: String,
    pub state_file: String,
    pub kalshi_api_base_url: String,
    pub kalshi_ws_url: String,
    pub market_tickers: Vec<String>,
    pub trigger_yes_bid_dollars: String,
    pub auto_sell_enabled: bool,
    pub dry_run: bool,
    pub subaccount: u32,
    pub http_timeout_secs: u64,
    pub ws_reconnect_delay_secs: u64,
}

impl Config {
    pub fn load() -> Result<Self> {
        let enabled = bool_env_required("KALSHI_SPOKE_ENABLED")?;
        let http_addr = string_env_required("KALSHI_SPOKE_HTTP_ADDR")?;
        let hub_notify_url = string_env_required("KALSHI_HUB_NOTIFY_URL")?;
        let hub_notify_auth_token = string_env_required("KALSHI_HUB_NOTIFY_AUTH_TOKEN")?;
        let notify_channel = string_env_required("KALSHI_NOTIFY_CHANNEL")?;
        let notify_severity = normalize_severity(&string_env_required("KALSHI_NOTIFY_SEVERITY")?)?;
        let state_file = PathBuf::from(string_env_required("KALSHI_STATE_FILE")?);

        let kalshi_api_base_url = trim_trailing_slash(&string_env_required("KALSHI_API_BASE_URL")?);
        let kalshi_ws_url = string_env_required("KALSHI_WS_URL")?;
        let kalshi_access_key = string_env_required("KALSHI_ACCESS_KEY")?;
        let kalshi_private_key_path =
            PathBuf::from(string_env_required("KALSHI_PRIVATE_KEY_PATH")?);
        let market_tickers = parse_csv_env_required("KALSHI_MARKET_TICKERS")?;

        let trigger_yes_bid_dollars = decimal_env_required("KALSHI_TRIGGER_YES_BID_DOLLARS")?;
        let auto_sell_enabled = bool_env_required("KALSHI_AUTO_SELL_ENABLED")?;
        let dry_run = bool_env_required("KALSHI_DRY_RUN")?;
        let subaccount = u32_env_required("KALSHI_SUBACCOUNT")?;
        let http_timeout = duration_env_required("KALSHI_HTTP_TIMEOUT")?;
        let ws_reconnect_delay = duration_env_required("KALSHI_WS_RECONNECT_DELAY")?;

        if trigger_yes_bid_dollars <= Decimal::ZERO || trigger_yes_bid_dollars >= Decimal::ONE {
            bail!("KALSHI_TRIGGER_YES_BID_DOLLARS must be between 0 and 1 (example: 0.6000)");
        }

        if subaccount > 32 {
            bail!("KALSHI_SUBACCOUNT must be between 0 and 32");
        }
        if ws_reconnect_delay.is_zero() {
            bail!("KALSHI_WS_RECONNECT_DELAY must be positive");
        }
        if http_timeout.is_zero() {
            bail!("KALSHI_HTTP_TIMEOUT must be positive");
        }

        Ok(Self {
            enabled,
            http_addr,
            hub_notify_url,
            hub_notify_auth_token,
            notify_channel,
            notify_severity,
            state_file,
            kalshi_api_base_url,
            kalshi_ws_url,
            kalshi_access_key,
            kalshi_private_key_path,
            market_tickers,
            trigger_yes_bid_dollars,
            auto_sell_enabled,
            dry_run,
            subaccount,
            http_timeout,
            ws_reconnect_delay,
        })
    }

    pub fn trigger_threshold_string(&self) -> String {
        format!("{:.4}", self.trigger_yes_bid_dollars)
    }

    pub fn public(&self) -> PublicConfig {
        PublicConfig {
            enabled: self.enabled,
            http_addr: self.http_addr.clone(),
            hub_notify_url: self.hub_notify_url.clone(),
            notify_channel: self.notify_channel.clone(),
            notify_severity: self.notify_severity.clone(),
            state_file: self.state_file.to_string_lossy().to_string(),
            kalshi_api_base_url: self.kalshi_api_base_url.clone(),
            kalshi_ws_url: self.kalshi_ws_url.clone(),
            market_tickers: self.market_tickers.clone(),
            trigger_yes_bid_dollars: self.trigger_threshold_string(),
            auto_sell_enabled: self.auto_sell_enabled,
            dry_run: self.dry_run,
            subaccount: self.subaccount,
            http_timeout_secs: self.http_timeout.as_secs(),
            ws_reconnect_delay_secs: self.ws_reconnect_delay.as_secs(),
        }
    }
}

fn string_env_required(key: &str) -> Result<String> {
    match env::var(key) {
        Ok(value) if !value.trim().is_empty() => Ok(value.trim().to_string()),
        _ => Err(anyhow!("{} is required", key)),
    }
}

fn parse_csv_env_required(key: &str) -> Result<Vec<String>> {
    let raw = string_env_required(key)?;
    Ok(raw
        .split(',')
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToString::to_string)
        .collect())
}

fn bool_env_required(key: &str) -> Result<bool> {
    let raw = string_env_required(key)?;
    match raw.trim().to_ascii_lowercase().as_str() {
        "1" | "true" | "yes" | "on" => Ok(true),
        "0" | "false" | "no" | "off" => Ok(false),
        _ => Err(anyhow!("invalid {} value {:?}; use true/false", key, raw)),
    }
}

fn u32_env_required(key: &str) -> Result<u32> {
    let raw = string_env_required(key)?;
    raw.trim()
        .parse::<u32>()
        .with_context(|| format!("invalid {} value {:?}", key, raw))
}

fn duration_env_required(key: &str) -> Result<Duration> {
    let raw = string_env_required(key)?;
    parse_duration(raw.trim()).with_context(|| format!("invalid {} value {:?}", key, raw))
}

fn parse_duration(raw: &str) -> Result<Duration> {
    if let Some(value) = raw.strip_suffix("ms") {
        let ms = u64::from_str(value.trim())?;
        return Ok(Duration::from_millis(ms));
    }
    if let Some(value) = raw.strip_suffix('s') {
        let secs = u64::from_str(value.trim())?;
        return Ok(Duration::from_secs(secs));
    }
    if let Some(value) = raw.strip_suffix('m') {
        let mins = u64::from_str(value.trim())?;
        return Ok(Duration::from_secs(mins * 60));
    }
    if let Some(value) = raw.strip_suffix('h') {
        let hours = u64::from_str(value.trim())?;
        return Ok(Duration::from_secs(hours * 60 * 60));
    }

    let secs = u64::from_str(raw.trim())?;
    Ok(Duration::from_secs(secs))
}

fn decimal_env_required(key: &str) -> Result<Decimal> {
    let raw = string_env_required(key)?;
    Decimal::from_str(raw.trim()).with_context(|| format!("invalid {} value {:?}", key, raw))
}

fn normalize_severity(raw: &str) -> Result<String> {
    let normalized = raw.trim().to_ascii_lowercase();
    match normalized.as_str() {
        "info" | "warning" | "critical" => Ok(normalized),
        _ => Err(anyhow!(
            "KALSHI_NOTIFY_SEVERITY must be one of: info, warning, critical"
        )),
    }
}

fn trim_trailing_slash(value: &str) -> String {
    value.trim_end_matches('/').to_string()
}
