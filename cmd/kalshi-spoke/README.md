# kalshi-spoke

`kalshi-spoke` watches Kalshi ticker updates, tracks threshold crossings, and can optionally place a reduce-only sell order when a rule fires.

## Canonical local port map

| Service | Local bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

It also sends alerts through `discord-hub` using `POST /notify`.

## Trigger behavior

- Trigger applies per market when a threshold exists in `KALSHI_TRIGGER_YES_BID_BY_MARKET`.
- Markets without a threshold stay in observe-only mode (price/status updates still work).
- `yes_bid_dollars` is the **current market YES bid**, not your original entry price.
- Trigger is edge-based: it fires when crossing from below to above.
- It re-arms only after price drops below the threshold again.

## Safety settings

- `KALSHI_SPOKE_ENABLED=false` keeps monitoring off.
- `KALSHI_AUTO_SELL_ENABLED=false` keeps auto-sell off.
- `KALSHI_DRY_RUN=true` keeps order submission in dry-run mode.

To actually place orders, all three must be true/false in the right combination:

- `KALSHI_SPOKE_ENABLED=true`
- `KALSHI_AUTO_SELL_ENABLED=true`
- `KALSHI_DRY_RUN=false`

## Required env vars

`kalshi-spoke` validates these at startup even when `KALSHI_SPOKE_ENABLED=false`, so disabling monitoring does not bypass config validation.

- `KALSHI_ACCESS_KEY`
- `KALSHI_PRIVATE_KEY_PATH`
- `KALSHI_MARKET_TICKERS` (comma-separated)

Other common vars:

- `KALSHI_TRIGGER_YES_BID_BY_MARKET` (optional `TICKER=0.1234` pairs, comma-separated)
- `HUB_NOTIFY_URL`
- `HUB_NOTIFY_AUTH_TOKEN`
- `KALSHI_NOTIFY_CHANNEL`
- `KALSHI_SUBACCOUNT`

## Local run

Configure values in the repository root `.env`.

```bash
cargo run --manifest-path cmd/kalshi-spoke/Cargo.toml
```

`kalshi-spoke` reads configuration from the repository root `.env`.

## Local API

- `GET /healthz`
- `GET /status`
- `GET /control/commands`
- `POST /control/command`

`POST /control/command` requires `context.discordUserId` in the JSON body.

Bind address is configured by `KALSHI_SPOKE_HTTP_ADDR`.

## Discord command catalog

`kalshi-spoke` now exposes a spoke command catalog consumed by `discord-hub`:

- `kalshi-status` — returns current runtime and persisted trigger state.
- `kalshi-positions` — returns YES/NO contract exposure summary with market titles, prompts, and tickers.
- `kalshi-thresholds` — returns trigger-enabled vs observe-only mode for tracked markets.
- `kalshi-threshold-set` — sets trigger threshold for a ticker (`ticker`, `yes_bid_dollars`).
- `kalshi-threshold-remove` — removes trigger threshold for a ticker (observe-only mode).
