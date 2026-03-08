# kalshi-spoke

`kalshi-spoke` watches Kalshi ticker updates, tracks threshold crossings, and can optionally place a reduce-only sell order when a rule fires.

## Canonical local port map

| Service | Default bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

It also sends alerts through `discord-hub` using `POST /notify`.

## Rule in this V1

- Trigger when `yes_bid_dollars >= KALSHI_TRIGGER_YES_BID_DOLLARS`.
- `yes_bid_dollars` is the **current market YES bid**, not your original entry price.
- Trigger is edge-based: it fires when crossing from below to above.
- It re-arms only after price drops below the threshold again.

## Safety defaults

- `KALSHI_SPOKE_ENABLED=false` by default (monitoring off).
- `KALSHI_AUTO_SELL_ENABLED=false` by default.
- `KALSHI_DRY_RUN=true` by default.

To actually place orders, all three must be true/false in the right combination:

- `KALSHI_SPOKE_ENABLED=true`
- `KALSHI_AUTO_SELL_ENABLED=true`
- `KALSHI_DRY_RUN=false`

## Required env vars (when enabled)

- `KALSHI_ACCESS_KEY`
- `KALSHI_PRIVATE_KEY_PATH`
- `KALSHI_MARKET_TICKERS` (comma-separated)

Other common vars:

- `KALSHI_TRIGGER_YES_BID_DOLLARS` (default `0.6000`)
- `KALSHI_HUB_NOTIFY_URL` (default `http://127.0.0.1:8080/notify`)
- `KALSHI_NOTIFY_CHANNEL` (default `kalshi-alerts`)
- `KALSHI_SUBACCOUNT` (default `0`)

## Local run

Copy `cmd/kalshi-spoke/.env.example` to `cmd/kalshi-spoke/.env` and fill values.

```bash
cargo run --manifest-path cmd/kalshi-spoke/Cargo.toml
```

`kalshi-spoke` loads env files in this order: `cmd/kalshi-spoke/.env`, then `.env` (legacy fallback).

## Local API

- `GET /healthz`
- `GET /status`

Default bind address: `127.0.0.1:8092`.
