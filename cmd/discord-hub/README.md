# discord-hub

`discord-hub` keeps a Discord gateway connection alive and exposes a local REST endpoint for internal services to post alerts.

## Canonical local port map

| Service | Local bind |
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

## Spoke command discovery

`discord-hub` can auto-register spoke-owned slash commands.

### Required configuration (`SPOKE_COMMAND_SERVICES`)

Use `SPOKE_COMMAND_SERVICES` to load commands from multiple spokes. It must be a JSON array with `name`, `commandsUrl`, and `executeUrl` per service.

Example with two services:

```bash
SPOKE_COMMAND_SERVICES='[
  {"name":"beeminder-spoke","commandsUrl":"http://beeminder-spoke:8090/control/commands","executeUrl":"http://beeminder-spoke:8090/control/command"},
  {"name":"finance-spoke","commandsUrl":"http://finance-spoke:8091/control/commands","executeUrl":"http://finance-spoke:8091/control/command"}
]'
```

Example with four services:

```bash
SPOKE_COMMAND_SERVICES='[
  {"name":"beeminder-spoke","commandsUrl":"http://beeminder-spoke:8090/control/commands","executeUrl":"http://beeminder-spoke:8090/control/command"},
  {"name":"finance-spoke","commandsUrl":"http://finance-spoke:8091/control/commands","executeUrl":"http://finance-spoke:8091/control/command"},
  {"name":"kalshi-spoke","commandsUrl":"http://kalshi-spoke:8092/control/commands","executeUrl":"http://kalshi-spoke:8092/control/command"},
  {"name":"accountability-spoke","commandsUrl":"http://accountability-spoke:8093/control/commands","executeUrl":"http://accountability-spoke:8093/control/command"}
]'
```

Set `SPOKE_COMMANDS_ENABLED=false` to disable discovery and keep only `/ping`.

`discord-hub` enforces globally unique slash command names across all services. Startup fails when duplicate names are discovered.

## Notify endpoint

Configure channel routing with `DISCORD_CHANNEL_MAP`:

```bash
DISCORD_CHANNEL_MAP='kalshi-alerts:1234567890,mandarin-streaks:2345678901'
```

Send alerts from local services:

```bash
export HUB_NOTIFY_AUTH_TOKEN=replace-with-long-random-token

curl -X POST http://localhost:8080/notify \
  -H "Authorization: Bearer ${HUB_NOTIFY_AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targetChannel":"kalshi-alerts","message":"Test alert","severity":"info"}'
```

Valid severities: `info`, `warning`, `critical`.
