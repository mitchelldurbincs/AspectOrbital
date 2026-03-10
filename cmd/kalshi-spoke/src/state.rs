use std::{
    collections::BTreeMap,
    fs,
    io::Write,
    path::{Path, PathBuf},
};

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tempfile::NamedTempFile;
use tokio::sync::Mutex;

use crate::rules::TriggerRule;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PersistedState {
    pub markets: BTreeMap<String, MarketState>,
    #[serde(default)]
    pub rules_initialized: bool,
    #[serde(default)]
    pub trigger_rules: BTreeMap<String, PersistedTriggerRule>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MarketState {
    pub last_yes_bid_dollars: Option<String>,
    pub last_action: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PersistedTriggerRule {
    pub spec: TriggerRule,
    #[serde(default)]
    pub state: TriggerRuleState,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TriggerRuleState {
    pub was_condition_true: bool,
    #[serde(default)]
    pub phase: TriggerRulePhase,
    pub last_triggered_at: Option<DateTime<Utc>>,
    pub last_client_order_id: Option<String>,
    #[serde(default)]
    pub pending_started_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub pending_client_order_id: Option<String>,
    pub last_action: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum TriggerRulePhase {
    #[default]
    Idle,
    SellPending,
    Triggered,
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

    pub async fn has_market(&self, market_ticker: &str) -> bool {
        self.state.lock().await.markets.contains_key(market_ticker)
    }

    pub async fn active_rule_for_market(
        &self,
        market_ticker: &str,
    ) -> Option<PersistedTriggerRule> {
        self.state
            .lock()
            .await
            .trigger_rules
            .values()
            .find(|rule| rule.spec.enabled && rule.spec.matches_market(market_ticker))
            .cloned()
    }

    pub async fn bootstrap_rules_if_empty(&self, rules: &[TriggerRule]) -> Result<usize> {
        let mut guard = self.state.lock().await;
        if guard.rules_initialized {
            return Ok(0);
        }

        guard.rules_initialized = true;
        for rule in rules {
            guard.trigger_rules.insert(
                rule.id.clone(),
                PersistedTriggerRule {
                    spec: rule.clone(),
                    state: TriggerRuleState::default(),
                },
            );
        }

        self.save_locked(&guard)?;

        Ok(guard.trigger_rules.len())
    }

    pub async fn set_yes_bid_rule(&self, rule: TriggerRule) -> Result<()> {
        let mut guard = self.state.lock().await;
        guard.rules_initialized = true;
        if let Some(existing) = guard.trigger_rules.get_mut(&rule.id) {
            existing.spec = rule;
        } else {
            guard.trigger_rules.insert(
                rule.id.clone(),
                PersistedTriggerRule {
                    spec: rule,
                    state: TriggerRuleState::default(),
                },
            );
        }
        self.save_locked(&guard)
    }

    pub async fn remove_rules_for_market(&self, market_ticker: &str) -> Result<bool> {
        let mut guard = self.state.lock().await;
        guard.rules_initialized = true;
        let ids_to_remove = guard
            .trigger_rules
            .keys()
            .filter(|id| {
                guard
                    .trigger_rules
                    .get(*id)
                    .map(|rule| rule.spec.matches_market(market_ticker))
                    .unwrap_or(false)
            })
            .cloned()
            .collect::<Vec<_>>();

        let removed = !ids_to_remove.is_empty();
        for id in ids_to_remove {
            guard.trigger_rules.remove(&id);
        }

        if removed {
            self.save_locked(&guard)?;
        }

        Ok(removed)
    }

    pub async fn update_rule<F>(
        &self,
        rule_id: &str,
        update: F,
    ) -> Result<Option<PersistedTriggerRule>>
    where
        F: FnOnce(&mut PersistedTriggerRule),
    {
        let mut guard = self.state.lock().await;
        let Some(entry) = guard.trigger_rules.get_mut(rule_id) else {
            return Ok(None);
        };

        update(entry);
        let snapshot = entry.clone();
        self.save_locked(&guard)?;
        Ok(Some(snapshot))
    }

    pub async fn mark_rule_sell_pending(
        &self,
        rule_id: &str,
        client_order_id: Option<String>,
        action: String,
    ) -> Result<Option<PersistedTriggerRule>> {
        self.update_rule(rule_id, move |entry| {
            entry.state.was_condition_true = true;
            entry.state.phase = TriggerRulePhase::SellPending;
            entry.state.pending_started_at = Some(Utc::now());
            entry.state.pending_client_order_id = client_order_id.clone();
            entry.state.last_action = Some(action.clone());
        })
        .await
    }

    pub async fn mark_rule_triggered(
        &self,
        rule_id: &str,
        client_order_id: Option<String>,
        action: String,
    ) -> Result<Option<PersistedTriggerRule>> {
        self.update_rule(rule_id, move |entry| {
            entry.state.was_condition_true = true;
            entry.state.phase = TriggerRulePhase::Triggered;
            entry.state.last_triggered_at = Some(Utc::now());
            entry.state.last_client_order_id = client_order_id
                .clone()
                .or_else(|| entry.state.pending_client_order_id.clone());
            entry.state.pending_started_at = None;
            entry.state.pending_client_order_id = None;
            entry.state.last_action = Some(action.clone());
        })
        .await
    }

    pub async fn mark_rule_rearmed(
        &self,
        rule_id: &str,
        action: String,
    ) -> Result<Option<PersistedTriggerRule>> {
        self.update_rule(rule_id, move |entry| {
            entry.state.was_condition_true = false;
            entry.state.phase = TriggerRulePhase::Idle;
            entry.state.pending_started_at = None;
            entry.state.pending_client_order_id = None;
            entry.state.last_action = Some(action.clone());
        })
        .await
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
        let parent = self
            .path
            .parent()
            .map(Path::to_path_buf)
            .unwrap_or_else(|| PathBuf::from("."));
        fs::create_dir_all(&parent)
            .with_context(|| format!("failed to create state dir {}", parent.display()))?;

        let payload =
            serde_json::to_vec_pretty(state).context("failed to serialize state payload")?;
        let temp_file = write_temp_file(&parent, &payload)
            .with_context(|| format!("failed to stage state file for {}", self.path.display()))?;
        persist_temp_file(temp_file, &self.path)
            .with_context(|| format!("failed to finalize state file {}", self.path.display()))?;

        Ok(())
    }
}

fn write_temp_file(parent: &Path, payload: &[u8]) -> Result<NamedTempFile> {
    let mut temp_file = NamedTempFile::new_in(parent)
        .with_context(|| format!("failed to create temp state file in {}", parent.display()))?;
    temp_file
        .write_all(payload)
        .context("failed to write state payload to temp file")?;
    temp_file
        .flush()
        .context("failed to flush temp state file")?;
    temp_file
        .as_file()
        .sync_all()
        .context("failed to sync temp state file")?;

    Ok(temp_file)
}

fn persist_temp_file(temp_file: NamedTempFile, path: &Path) -> Result<()> {
    temp_file.persist(path).map_err(|err| {
        let temp_path = err.file.path().to_path_buf();
        anyhow::Error::new(err.error).context(format!(
            "failed to replace {} with temp file {}",
            path.display(),
            temp_path.display()
        ))
    })?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rules::TriggerRule;
    use rust_decimal::Decimal;
    use tempfile::tempdir;

    #[test]
    fn repeated_saves_replace_existing_state_file() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");

        runtime
            .block_on(store.update_market("FIRST", |state| {
                state.last_action = Some("first save".to_string());
            }))
            .expect("first save succeeds");

        runtime
            .block_on(store.update_market("SECOND", |state| {
                state.last_action = Some("second save".to_string());
            }))
            .expect("second save succeeds");

        let reloaded = StateStore::load(&path).expect("reload state store");
        let snapshot = runtime.block_on(reloaded.snapshot());

        assert_eq!(snapshot.markets.len(), 2);
        assert_eq!(
            snapshot
                .markets
                .get("SECOND")
                .and_then(|entry| entry.last_action.as_deref()),
            Some("second save")
        );
    }

    #[test]
    fn successful_save_does_not_leave_temp_files_behind() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");

        runtime
            .block_on(store.update_market("ONLY", |state| {
                state.last_action = Some("saved".to_string());
            }))
            .expect("save succeeds");

        let entries = fs::read_dir(dir.path())
            .expect("read tempdir")
            .map(|entry| {
                entry
                    .expect("dir entry")
                    .file_name()
                    .to_string_lossy()
                    .into_owned()
            })
            .collect::<Vec<_>>();

        assert_eq!(entries, vec!["state.json"]);
    }

    #[test]
    fn bootstrap_rules_only_applies_once() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");
        let rules = vec![
            TriggerRule::yes_bid_crosses_above("FIRST", Decimal::new(60, 2)).expect("rule builds"),
        ];

        let inserted = runtime
            .block_on(store.bootstrap_rules_if_empty(&rules))
            .expect("bootstrap succeeds");
        assert_eq!(inserted, 1);

        runtime
            .block_on(store.remove_rules_for_market("FIRST"))
            .expect("remove succeeds");

        let inserted_again = runtime
            .block_on(store.bootstrap_rules_if_empty(&rules))
            .expect("second bootstrap succeeds");
        assert_eq!(inserted_again, 0);

        let snapshot = runtime.block_on(store.snapshot());
        assert!(snapshot.trigger_rules.is_empty());
        assert!(snapshot.rules_initialized);
    }

    #[test]
    fn mark_rule_sell_pending_persists_pending_phase() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");
        let rule =
            TriggerRule::yes_bid_crosses_above("FIRST", Decimal::new(60, 2)).expect("rule builds");

        runtime
            .block_on(store.set_yes_bid_rule(rule.clone()))
            .expect("rule save succeeds");
        runtime
            .block_on(store.mark_rule_sell_pending(
                &rule.id,
                Some("coid-1".to_string()),
                "pending".to_string(),
            ))
            .expect("pending save succeeds");

        let snapshot = runtime.block_on(store.snapshot());
        let saved = snapshot
            .trigger_rules
            .get(&rule.id)
            .expect("saved rule exists");
        assert!(saved.state.was_condition_true);
        assert_eq!(saved.state.phase, TriggerRulePhase::SellPending);
        assert_eq!(
            saved.state.pending_client_order_id.as_deref(),
            Some("coid-1")
        );
        assert!(saved.state.pending_started_at.is_some());
    }

    #[test]
    fn mark_rule_triggered_clears_pending_fields() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");
        let rule =
            TriggerRule::yes_bid_crosses_above("FIRST", Decimal::new(60, 2)).expect("rule builds");

        runtime
            .block_on(store.set_yes_bid_rule(rule.clone()))
            .expect("rule save succeeds");
        runtime
            .block_on(store.mark_rule_sell_pending(
                &rule.id,
                Some("coid-1".to_string()),
                "pending".to_string(),
            ))
            .expect("pending save succeeds");
        runtime
            .block_on(store.mark_rule_triggered(&rule.id, None, "triggered".to_string()))
            .expect("triggered save succeeds");

        let snapshot = runtime.block_on(store.snapshot());
        let saved = snapshot
            .trigger_rules
            .get(&rule.id)
            .expect("saved rule exists");
        assert!(saved.state.was_condition_true);
        assert_eq!(saved.state.phase, TriggerRulePhase::Triggered);
        assert_eq!(saved.state.last_client_order_id.as_deref(), Some("coid-1"));
        assert!(saved.state.last_triggered_at.is_some());
        assert!(saved.state.pending_started_at.is_none());
        assert!(saved.state.pending_client_order_id.is_none());
    }
}
