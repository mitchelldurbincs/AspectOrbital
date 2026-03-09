use std::{
    collections::BTreeMap,
    fs,
    path::{Path, PathBuf},
};

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tokio::sync::Mutex;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PersistedState {
    pub markets: BTreeMap<String, MarketState>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MarketState {
    pub was_above_threshold: bool,
    pub last_yes_bid_dollars: Option<String>,
    pub last_triggered_at: Option<DateTime<Utc>>,
    pub last_client_order_id: Option<String>,
    pub last_action: Option<String>,
}

pub struct StateStore {
    path: PathBuf,
    state: Mutex<PersistedState>,
}

impl StateStore {
    pub fn load(path: impl AsRef<Path>) -> Result<Self> {
        let path = path.as_ref().to_path_buf();

        let state = match fs::read(&path) {
            Ok(bytes) if !bytes.is_empty() => serde_json::from_slice::<PersistedState>(&bytes)
                .with_context(|| format!("failed to parse state file at {}", path.display()))?,
            Ok(_) => PersistedState::default(),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => PersistedState::default(),
            Err(err) => {
                return Err(err)
                    .with_context(|| format!("failed to read state file at {}", path.display()));
            }
        };

        Ok(Self {
            path,
            state: Mutex::new(state),
        })
    }

    pub async fn snapshot(&self) -> PersistedState {
        self.state.lock().await.clone()
    }

    pub async fn market_snapshot(&self, market_ticker: &str) -> MarketState {
        self.state
            .lock()
            .await
            .markets
            .get(market_ticker)
            .cloned()
            .unwrap_or_default()
    }

    pub async fn has_market(&self, market_ticker: &str) -> bool {
        self.state.lock().await.markets.contains_key(market_ticker)
    }

    pub async fn update_market<F>(&self, market_ticker: &str, update: F) -> Result<MarketState>
    where
        F: FnOnce(&mut MarketState),
    {
        let mut guard = self.state.lock().await;
        let entry = guard.markets.entry(market_ticker.to_string()).or_default();
        update(entry);
        let snapshot = entry.clone();

        self.save_locked(&guard)?;
        Ok(snapshot)
    }

    fn save_locked(&self, state: &PersistedState) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create state dir {}", parent.display()))?;
        }

        let payload =
            serde_json::to_vec_pretty(state).context("failed to serialize state payload")?;
        let temp_path = self.path.with_extension("tmp");

        fs::write(&temp_path, payload)
            .with_context(|| format!("failed to write temp state file {}", temp_path.display()))?;
        fs::rename(&temp_path, &self.path).with_context(|| {
            format!(
                "failed to move temp state file {} into {}",
                temp_path.display(),
                self.path.display()
            )
        })?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::StateStore;
    use std::{
        path::PathBuf,
        time::{SystemTime, UNIX_EPOCH},
    };

    fn unique_state_file() -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system clock before unix epoch")
            .as_nanos();
        std::env::temp_dir().join(format!("kalshi-state-{nanos}.json"))
    }

    #[tokio::test]
    async fn update_market_persists_and_reloads() {
        let path = unique_state_file();
        let store = StateStore::load(&path).expect("initial load");

        let updated = store
            .update_market("INXD-TEST", |state| {
                state.was_above_threshold = true;
                state.last_yes_bid_dollars = Some("0.7500".to_string());
                state.last_action = Some("trigger fired".to_string());
            })
            .await
            .expect("update market");

        assert!(updated.was_above_threshold);
        assert_eq!(updated.last_yes_bid_dollars.as_deref(), Some("0.7500"));

        let reloaded = StateStore::load(&path).expect("reload");
        let snapshot = reloaded.market_snapshot("INXD-TEST").await;
        assert!(snapshot.was_above_threshold);
        assert_eq!(snapshot.last_action.as_deref(), Some("trigger fired"));

        let _ = std::fs::remove_file(path);
    }
}
