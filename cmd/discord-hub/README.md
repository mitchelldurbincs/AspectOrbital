# discord-hub

`discord-hub` keeps a Discord gateway connection alive and exposes a local REST endpoint for internal services to post alerts.

## Canonical local port map

| Service | Default bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

## Local run

1. Copy `cmd/discord-hub/.env.example` to `cmd/discord-hub/.env` and fill values.
2. Start the hub:

```bash
go run ./cmd/discord-hub
```

3. In Discord, run `/ping` to verify the bot is responsive.

`discord-hub` loads env files in this order: `cmd/discord-hub/.env`, then `.env` (legacy fallback).

## Beeminder-spoke command discovery

If `beeminder-spoke` is running, `discord-hub` can auto-register spoke-owned slash commands by reading:

- `SPOKE_COMMANDS_URL` (default `http://127.0.0.1:8090/control/commands`)
- `SPOKE_COMMAND_URL` (default `http://127.0.0.1:8090/control/command`)

The spoke publishes command names, descriptions, and option metadata. `discord-hub` maps those directly into Discord slash commands and forwards interaction options back to the spoke.

Set `SPOKE_COMMANDS_ENABLED=false` to disable discovery and keep only `/ping`.

## Notify endpoint

Send alerts from local services:

```bash
curl -X POST http://localhost:8080/notify \
  -H "Content-Type: application/json" \
  -d '{"targetChannel":"kalshi-alerts","message":"Test alert","severity":"info"}'
```

Valid severities: `info`, `warning`, `critical`.
