# finance-spoke

`finance-spoke` posts a weekly Discord summary of unique subscription charges detected from Plaid recurring outflow streams.

## Canonical local port map

| Service | Local bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

## What this scaffold includes

- Weekly scheduler with configurable weekday/time/timezone.
- Plaid recurring subscription fetch (`/transactions/recurring/get`) across one or more linked Items.
- Unique subscription dedupe and message formatting.
- Delivery to `discord-hub` via `POST /notify`.
- State persistence to avoid duplicate summaries for the same week.
- Local control API for health, status, and manual test runs.
- Plaid setup endpoints to create a Link token and exchange a public token.

## Required env vars

`finance-spoke` validates these at startup even when `FINANCE_SUMMARY_ENABLED=false`, so the service will not boot unless Plaid and notify settings are present.

- `FINANCE_SUMMARY_ENABLED=true`
- `PLAID_CLIENT_ID`
- `PLAID_SECRET`
- `PLAID_ENV` (`sandbox`, `development`, `production`)
- `PLAID_ACCESS_TOKENS` (comma-separated Item access tokens)
- `PLAID_WEBHOOK_URL`
- `FINANCE_NOTIFY_CHANNEL`
- `HUB_NOTIFY_URL`
- `HUB_NOTIFY_AUTH_TOKEN`

## Local run

Configure values in the repository root `.env`.

```bash
go run ./cmd/finance-spoke
```

`finance-spoke` reads configuration from the repository root `.env`.

## Local API

- `GET /healthz`
- `GET /status`
- `GET /control/commands`
- `POST /control/command`
- `POST /run/weekly-summary`
- `GET /plaid/setup`
- `POST /plaid/link-token`
- `POST /plaid/exchange-public-token`

`POST /control/command` requires `context.discordUserId` in the JSON body.

## Discord command catalog

`finance-spoke` now exposes a spoke command catalog consumed by `discord-hub`:

- `finance-status` — returns scheduler, configuration, and recent summary run state.

Manual trigger example:

```bash
curl -X POST http://127.0.0.1:8091/run/weekly-summary

curl -X POST http://127.0.0.1:8091/plaid/link-token \
  -H "Content-Type: application/json" \
  -d '{"clientUserId":"mitchell-local"}'

curl -X POST http://127.0.0.1:8091/plaid/exchange-public-token \
  -H "Content-Type: application/json" \
  -d '{"publicToken":"public-sandbox-..."}'
```

Current setup flow for real accounts:

1. Copy `.env.example` to `.env` at the repository root.
2. Fill all required Plaid values, including `PLAID_ACCESS_TOKENS` and `PLAID_WEBHOOK_URL`.
3. Start `finance-spoke`.
4. Optionally open `http://127.0.0.1:8091/plaid/setup` to create additional Plaid Link sessions or exchange a new public token.

The local Plaid setup page is useful after the service is already configured, but it is not currently a zero-token bootstrap flow because startup validation requires `PLAID_ACCESS_TOKENS` before the HTTP server starts.
