# finance-spoke

`finance-spoke` posts a weekly Discord summary of unique subscription charges detected from Plaid recurring outflow streams.

## Canonical local port map

| Service | Default bind |
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

## Required env vars (when enabled)

- `FINANCE_SUMMARY_ENABLED=true`
- `PLAID_CLIENT_ID`
- `PLAID_SECRET`
- `PLAID_ENV` (`sandbox`, `development`, `production`)
- `PLAID_ACCESS_TOKENS` (comma-separated Item access tokens)
- `FINANCE_NOTIFY_CHANNEL`
- `FINANCE_HUB_NOTIFY_URL`

## Local run

Copy `cmd/finance-spoke/.env.example` to `cmd/finance-spoke/.env` and fill values.

```bash
go run ./cmd/finance-spoke
```

`finance-spoke` loads env files in this order: `cmd/finance-spoke/.env`, then `.env` (legacy fallback).

## Local API

- `GET /healthz`
- `GET /status`
- `POST /run/weekly-summary`
- `GET /plaid/setup`
- `POST /plaid/link-token`
- `POST /plaid/exchange-public-token`

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

Quick setup flow for real accounts:

1. Start `finance-spoke`.
2. Open `http://127.0.0.1:8091/plaid/setup`.
3. Connect Fifth Third, copy returned `accessToken`.
4. Repeat and connect American Express, copy second token.
5. Set `PLAID_ACCESS_TOKENS=token1,token2` in `cmd/finance-spoke/.env` and enable `FINANCE_SUMMARY_ENABLED=true`.
