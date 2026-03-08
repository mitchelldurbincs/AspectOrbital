use std::{fs, str::FromStr};

use anyhow::{bail, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use chrono::Utc;
use http::HeaderValue;
use rand::thread_rng;
use reqwest::{Client, Method, Url};
use rsa::{
    pkcs1::DecodeRsaPrivateKey,
    pkcs8::DecodePrivateKey,
    pss::BlindedSigningKey,
    signature::{RandomizedSigner, SignatureEncoding},
    RsaPrivateKey,
};
use rust_decimal::Decimal;
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use sha2::Sha256;
use tokio::net::TcpStream;
use tokio_tungstenite::{
    connect_async, tungstenite::client::IntoClientRequest, MaybeTlsStream, WebSocketStream,
};

#[derive(Clone)]
pub struct KalshiClient {
    client: Client,
    api_base_url: String,
    ws_url: String,
    access_key: String,
    private_key: RsaPrivateKey,
}

#[derive(Debug, Clone)]
pub struct CreatedOrder {
    pub order_id: String,
    pub status: String,
    pub fill_count_fp: Option<String>,
    pub remaining_count_fp: Option<String>,
}

#[derive(Deserialize)]
struct GetPositionsResponse {
    #[serde(default)]
    market_positions: Vec<MarketPosition>,
}

#[derive(Deserialize)]
struct MarketPosition {
    ticker: String,
    position_fp: String,
}

#[derive(Serialize)]
struct CreateOrderRequest<'a> {
    ticker: &'a str,
    side: &'static str,
    action: &'static str,
    count_fp: &'a str,
    yes_price_dollars: &'a str,
    time_in_force: &'static str,
    reduce_only: bool,
    client_order_id: &'a str,
    subaccount: u32,
}

#[derive(Deserialize)]
struct CreateOrderResponse {
    order: CreateOrderOrder,
}

#[derive(Deserialize)]
struct CreateOrderOrder {
    order_id: String,
    status: String,
    #[serde(default)]
    fill_count_fp: Option<String>,
    #[serde(default)]
    remaining_count_fp: Option<String>,
}

impl KalshiClient {
    pub fn new(
        client: Client,
        api_base_url: String,
        ws_url: String,
        access_key: String,
        private_key_path: &std::path::Path,
    ) -> Result<Self> {
        let pem = fs::read_to_string(private_key_path).with_context(|| {
            format!(
                "failed to read KALSHI private key file {}",
                private_key_path.display()
            )
        })?;

        let private_key = RsaPrivateKey::from_pkcs8_pem(&pem)
            .or_else(|_| RsaPrivateKey::from_pkcs1_pem(&pem))
            .context("failed to parse KALSHI private key (expected PKCS8 or PKCS1 PEM)")?;

        Ok(Self {
            client,
            api_base_url,
            ws_url,
            access_key,
            private_key,
        })
    }

    pub async fn connect_websocket(&self) -> Result<WebSocketStream<MaybeTlsStream<TcpStream>>> {
        let timestamp_ms = timestamp_ms();
        let ws_path = Url::parse(&self.ws_url)
            .context("invalid KALSHI_WS_URL")?
            .path()
            .to_string();
        let signature = self.sign(timestamp_ms, "GET", &ws_path)?;

        let mut request = self
            .ws_url
            .clone()
            .into_client_request()
            .context("failed to build websocket request")?;

        request.headers_mut().insert(
            "KALSHI-ACCESS-KEY",
            HeaderValue::from_str(&self.access_key)
                .context("invalid characters in KALSHI access key")?,
        );
        request.headers_mut().insert(
            "KALSHI-ACCESS-TIMESTAMP",
            HeaderValue::from_str(&timestamp_ms.to_string())
                .context("invalid websocket timestamp header")?,
        );
        request.headers_mut().insert(
            "KALSHI-ACCESS-SIGNATURE",
            HeaderValue::from_str(&signature).context("invalid websocket signature header")?,
        );

        let (stream, _response) = connect_async(request)
            .await
            .context("failed to establish websocket connection to Kalshi")?;

        Ok(stream)
    }

    pub async fn fetch_yes_position(
        &self,
        market_ticker: &str,
        subaccount: u32,
    ) -> Result<Decimal> {
        let response: GetPositionsResponse = self
            .authed_get_json(
                "/portfolio/positions",
                &[
                    ("ticker", market_ticker.to_string()),
                    ("subaccount", subaccount.to_string()),
                ],
            )
            .await?;

        let maybe_position = response
            .market_positions
            .iter()
            .find(|entry| entry.ticker.eq_ignore_ascii_case(market_ticker));

        let Some(position) = maybe_position else {
            return Ok(Decimal::ZERO);
        };

        let value = Decimal::from_str(position.position_fp.trim()).with_context(|| {
            format!(
                "failed to parse position_fp {:?} for market {}",
                position.position_fp, market_ticker
            )
        })?;

        Ok(value)
    }

    pub async fn create_reduce_only_sell_order(
        &self,
        market_ticker: &str,
        contracts_fp: &str,
        yes_price_dollars: &str,
        client_order_id: &str,
        subaccount: u32,
    ) -> Result<CreatedOrder> {
        let payload = CreateOrderRequest {
            ticker: market_ticker,
            side: "yes",
            action: "sell",
            count_fp: contracts_fp,
            yes_price_dollars,
            time_in_force: "immediate_or_cancel",
            reduce_only: true,
            client_order_id,
            subaccount,
        };

        let response: CreateOrderResponse =
            self.authed_post_json("/portfolio/orders", &payload).await?;

        Ok(CreatedOrder {
            order_id: response.order.order_id,
            status: response.order.status,
            fill_count_fp: response.order.fill_count_fp,
            remaining_count_fp: response.order.remaining_count_fp,
        })
    }

    async fn authed_get_json<T: DeserializeOwned>(
        &self,
        path: &str,
        query: &[(&str, String)],
    ) -> Result<T> {
        self.authed_request_json::<T, ()>(Method::GET, path, Some(query), None)
            .await
    }

    async fn authed_post_json<T: DeserializeOwned, B: Serialize>(
        &self,
        path: &str,
        body: &B,
    ) -> Result<T> {
        self.authed_request_json(Method::POST, path, None, Some(body))
            .await
    }

    async fn authed_request_json<T: DeserializeOwned, B: Serialize>(
        &self,
        method: Method,
        path: &str,
        query: Option<&[(&str, String)]>,
        body: Option<&B>,
    ) -> Result<T> {
        if !path.starts_with('/') {
            bail!("path must start with '/': {}", path);
        }

        let mut url = Url::parse(&format!("{}{}", self.api_base_url, path))
            .with_context(|| format!("failed to build URL for path {}", path))?;

        if let Some(query_items) = query {
            let mut pairs = url.query_pairs_mut();
            for (key, value) in query_items {
                pairs.append_pair(key, value);
            }
        }

        let timestamp_ms = timestamp_ms();
        let signature = self.sign(timestamp_ms, method.as_str(), url.path())?;

        let mut request = self
            .client
            .request(method.clone(), url)
            .header("KALSHI-ACCESS-KEY", &self.access_key)
            .header("KALSHI-ACCESS-TIMESTAMP", timestamp_ms.to_string())
            .header("KALSHI-ACCESS-SIGNATURE", signature);

        if let Some(payload) = body {
            request = request.json(payload);
        }

        let response = request
            .send()
            .await
            .with_context(|| format!("request to Kalshi failed: {} {}", method, path))?;

        let status = response.status();
        let response_text = response
            .text()
            .await
            .context("failed to read Kalshi response body")?;

        if !status.is_success() {
            bail!(
                "Kalshi {} {} failed ({}): {}",
                method,
                path,
                status,
                response_text.trim()
            );
        }

        serde_json::from_str::<T>(&response_text).with_context(|| {
            format!(
                "failed to parse Kalshi response for {} {}: {}",
                method,
                path,
                response_text.trim()
            )
        })
    }

    fn sign(&self, timestamp_ms: i64, method: &str, path: &str) -> Result<String> {
        let sign_path = path.split('?').next().unwrap_or(path);
        let sign_payload = format!(
            "{}{}{}",
            timestamp_ms,
            method.trim().to_ascii_uppercase(),
            sign_path
        );

        let signing_key = BlindedSigningKey::<Sha256>::new(self.private_key.clone());
        let signature = signing_key.sign_with_rng(&mut thread_rng(), sign_payload.as_bytes());
        Ok(STANDARD.encode(signature.to_vec()))
    }
}

fn timestamp_ms() -> i64 {
    Utc::now().timestamp_millis()
}
