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
    use tempfile::tempdir;

    #[test]
    fn repeated_saves_replace_existing_state_file() {
        let dir = tempdir().expect("create tempdir");
        let path = dir.path().join("state.json");

        let store = StateStore::load(&path).expect("load state store");
        let runtime = tokio::runtime::Runtime::new().expect("create runtime");

        runtime
            .block_on(store.update_market("FIRST", |state| {
                state.was_above_threshold = true;
                state.last_action = Some("first save".to_string());
            }))
            .expect("first save succeeds");

        runtime
            .block_on(store.update_market("SECOND", |state| {
                state.was_above_threshold = false;
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
}
