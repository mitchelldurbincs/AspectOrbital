use std::{collections::BTreeMap, sync::Arc, time::Duration};

use chrono::{DateTime, Utc};
use tokio::sync::Mutex;

use crate::{
    config::PublicConfig,
    kalshi::{KalshiClient, MarketDetails},
    state::StateStore,
};

#[derive(Clone)]
pub(crate) struct AppState {
    pub(crate) started_at: DateTime<Utc>,
    pub(crate) config: PublicConfig,
    pub(crate) kalshi: Arc<KalshiClient>,
    pub(crate) market_details_cache: Arc<MarketDetailsCache>,
    pub(crate) runtime: Arc<RuntimeState>,
    pub(crate) persisted: Arc<StateStore>,
}

pub(crate) struct MarketDetailsCache {
    ttl: Duration,
    entries: Mutex<BTreeMap<String, CachedMarketDetails>>,
}

#[derive(Clone)]
struct CachedMarketDetails {
    fetched_at: DateTime<Utc>,
    details: MarketDetails,
}

pub(crate) struct RuntimeState {
    inner: Mutex<RuntimeSnapshot>,
}

#[derive(Debug, Clone, Default, serde::Serialize)]
pub(crate) struct RuntimeSnapshot {
    pub(crate) ws_connected: bool,
    pub(crate) last_ws_message_at: Option<DateTime<Utc>>,
    pub(crate) last_ws_error: Option<String>,
    pub(crate) markets: BTreeMap<String, RuntimeMarketSnapshot>,
}

#[derive(Debug, Clone, Default, serde::Serialize)]
pub(crate) struct RuntimeMarketSnapshot {
    pub(crate) last_yes_bid_dollars: Option<String>,
    pub(crate) last_updated_at: Option<DateTime<Utc>>,
}

impl RuntimeState {
    pub(crate) fn new() -> Self {
        Self {
            inner: Mutex::new(RuntimeSnapshot::default()),
        }
    }

    pub(crate) async fn snapshot(&self) -> RuntimeSnapshot {
        self.inner.lock().await.clone()
    }

    pub(crate) async fn set_ws_connected(&self, connected: bool) {
        self.inner.lock().await.ws_connected = connected;
    }

    pub(crate) async fn clear_ws_error(&self) {
        self.inner.lock().await.last_ws_error = None;
    }

    pub(crate) async fn record_ws_error(&self, message: impl Into<String>) {
        self.inner.lock().await.last_ws_error = Some(message.into());
    }

    pub(crate) async fn record_ws_message(&self) {
        self.inner.lock().await.last_ws_message_at = Some(Utc::now());
    }

    pub(crate) async fn record_ticker_price(&self, market_ticker: &str, yes_bid_dollars: String) {
        let mut guard = self.inner.lock().await;
        let market = guard.markets.entry(market_ticker.to_string()).or_default();
        market.last_yes_bid_dollars = Some(yes_bid_dollars);
        market.last_updated_at = Some(Utc::now());
    }
}

impl MarketDetailsCache {
    pub(crate) fn new(ttl: Duration) -> Self {
        Self {
            ttl,
            entries: Mutex::new(BTreeMap::new()),
        }
    }

    pub(crate) async fn get(&self, ticker: &str) -> Option<MarketDetails> {
        let now = Utc::now();
        let guard = self.entries.lock().await;
        let entry = guard.get(ticker)?;

        let age = now.signed_duration_since(entry.fetched_at);
        if age.num_seconds() < 0 || age.num_seconds() as u64 > self.ttl.as_secs() {
            return None;
        }

        Some(entry.details.clone())
    }

    pub(crate) async fn put(&self, details: MarketDetails) {
        let key = details.ticker.trim().to_string();
        if key.is_empty() {
            return;
        }

        self.entries.lock().await.insert(
            key,
            CachedMarketDetails {
                fetched_at: Utc::now(),
                details,
            },
        );
    }
}
