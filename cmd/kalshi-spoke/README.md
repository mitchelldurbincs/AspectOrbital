# kalshi-spoke

`kalshi-spoke` watches Kalshi ticker updates, tracks rule crossings, and can optionally place a reduce-only sell order when a rule fires.

## Canonical local port map

| Service | Local bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

It also sends alerts through `discord-hub` using `POST /notify`.

## Rule behavior

- Markets in `KALSHI_MARKET_TICKERS` are always subscribed and observed.
- Trigger rules are optional and bootstrapped from `KALSHI_TRIGGER_RULES` using `TICKER=0.1234` entries.
- Markets without a rule stay in observe-only mode (price/status updates still work).
- `yes_bid_dollars` is the **current market YES bid**, not your original entry price.
- Trigger is edge-based: it fires when crossing from below to above.
- It re-arms only after price drops below the threshold again.

## Kalshi API alignment

- Current implementation is intentionally YES-only because the ticker websocket feed exposes `yes_bid_dollars` for market updates, and the order API accepts side-specific order fields such as `side=yes` with `yes_price_dollars`.
- Relevant docs:
  - WebSocket ticker channel: `https://docs.kalshi.com/websockets/market-ticker.md`
  - WebSocket connection/subscription flow: `https://docs.kalshi.com/websockets/websocket-connection.md`
  - Market details endpoint: `https://docs.kalshi.com/api-reference/market/get-market.md`
  - Create order endpoint: `https://docs.kalshi.com/api-reference/orders/create-order.md`
- The internal model now uses rules so NO-side support can be added later without another full rewrite.

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

- `KALSHI_TRIGGER_RULES` (optional `TICKER=0.1234` pairs, comma-separated; boots YES bid crossing rules)
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

- `kalshi-status` — returns current runtime and persisted rule state.
- `kalshi-positions` — returns YES/NO contract exposure summary with market titles, prompts, and tickers.
- `kalshi-rules` — returns rule-enabled vs observe-only mode for tracked markets.
- `kalshi-rule-set` — sets a YES bid crossing rule for a ticker (`ticker`, `threshold_dollars`).
- `kalshi-rule-remove` — removes a rule for a ticker (observe-only mode).

## Command examples

Show current rule state:

```bash
curl -X POST http://127.0.0.1:8092/control/command \
  -H 'Content-Type: application/json' \
  -d '{"command":"kalshi-rules","context":{"discordUserId":"local-user"}}'
```

Set a YES bid crossing rule for a tracked ticker:

```bash
curl -X POST http://127.0.0.1:8092/control/command \
  -H 'Content-Type: application/json' \
  -d '{"command":"kalshi-rule-set","context":{"discordUserId":"local-user"},"options":{"ticker":"INX-TEST-1","threshold_dollars":0.6000}}'
```

Remove a rule and return the market to observe-only mode:

```bash
curl -X POST http://127.0.0.1:8092/control/command \
  -H 'Content-Type: application/json' \
  -d '{"command":"kalshi-rule-remove","context":{"discordUserId":"local-user"},"options":{"ticker":"INX-TEST-1"}}'
```
