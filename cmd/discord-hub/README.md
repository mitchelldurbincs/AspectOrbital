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

### Single-service (legacy) configuration

Use these env vars to discover commands from one spoke:

- `SPOKE_COMMANDS_URL`
- `SPOKE_COMMAND_URL`

Example:

```bash
SPOKE_COMMANDS_URL=http://beeminder-spoke:8090/control/commands
SPOKE_COMMAND_URL=http://beeminder-spoke:8090/control/command
```

### Multi-service configuration (`SPOKE_COMMAND_SERVICES`)

Use `SPOKE_COMMAND_SERVICES` to load commands from multiple spokes. It must be a JSON array with `name`, `commandsUrl`, and `executeUrl` per service.

Example with two services:

```bash
SPOKE_COMMAND_SERVICES='[
  {"name":"beeminder-spoke","commandsUrl":"http://beeminder-spoke:8090/control/commands","executeUrl":"http://beeminder-spoke:8090/control/command"},
  {"name":"finance-spoke","commandsUrl":"http://finance-spoke:8091/control/commands","executeUrl":"http://finance-spoke:8091/control/command"}
]'
```

Example with three services:

```bash
SPOKE_COMMAND_SERVICES='[
  {"name":"beeminder-spoke","commandsUrl":"http://beeminder-spoke:8090/control/commands","executeUrl":"http://beeminder-spoke:8090/control/command"},
  {"name":"finance-spoke","commandsUrl":"http://finance-spoke:8091/control/commands","executeUrl":"http://finance-spoke:8091/control/command"},
  {"name":"kalshi-spoke","commandsUrl":"http://kalshi-spoke:8092/control/commands","executeUrl":"http://kalshi-spoke:8092/control/command"}
]'
```

Set `SPOKE_COMMANDS_ENABLED=false` to disable discovery and keep only `/ping`.

### `accountability-spoke` status

`accountability-spoke` currently exists as an experimental spoke implementation and is **not yet part of the supported Docker Compose deployment set**. Until it is promoted to supported status (including container/deployment wiring), do not include it in `SPOKE_COMMAND_SERVICES` for shared environments.

## Notify endpoint

Send alerts from local services:

```bash
curl -X POST http://localhost:8080/notify \
  -H "Content-Type: application/json" \
  -d '{"targetChannel":"kalshi-alerts","message":"Test alert","severity":"info"}'
```

Valid severities: `info`, `warning`, `critical`.
