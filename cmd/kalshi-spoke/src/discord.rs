use anyhow::{bail, Context, Result};
use reqwest::Client;
use serde::Serialize;

#[derive(Clone)]
pub struct DiscordClient {
    client: Client,
    notify_url: String,
    notify_auth_token: String,
    default_channel: String,
    default_severity: String,
}

#[derive(Serialize)]
struct NotifyRequest<'a> {
    #[serde(rename = "targetChannel")]
    target_channel: &'a str,
    message: &'a str,
    severity: &'a str,
}

impl DiscordClient {
    pub fn new(
        client: Client,
        notify_url: String,
        notify_auth_token: String,
        default_channel: String,
        default_severity: String,
    ) -> Self {
        Self {
            client,
            notify_url,
            notify_auth_token,
            default_channel,
            default_severity,
        }
    }

    pub async fn notify(&self, message: &str, severity: Option<&str>) -> Result<()> {
        let resolved_severity = severity.unwrap_or(&self.default_severity);

        let payload = NotifyRequest {
            target_channel: &self.default_channel,
            message,
            severity: resolved_severity,
        };

        let response = self
            .client
            .post(&self.notify_url)
            .bearer_auth(&self.notify_auth_token)
            .json(&payload)
            .send()
            .await
            .context("failed to call discord-hub notify endpoint")?;

        if !response.status().is_success() {
            let status = response.status();
            let body = response
                .text()
                .await
                .unwrap_or_else(|_| "unable to read response body".to_string());
            bail!("discord-hub notify failed ({}): {}", status, body.trim());
        }

        Ok(())
    }
}
