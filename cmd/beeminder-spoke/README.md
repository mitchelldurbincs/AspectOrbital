# beeminder-spoke

`beeminder-spoke` polls Beeminder and sends reminders to `discord-hub` when you are behind your daily goal.

## Canonical local port map

| Service | Local bind |
|---|---|
| `discord-hub` | `127.0.0.1:8080` |
| `beeminder-spoke` | `127.0.0.1:8090` |
| `finance-spoke` | `127.0.0.1:8091` |
| `kalshi-spoke` | `127.0.0.1:8092` |
| `accountability-spoke` | `127.0.0.1:8093` |

## What it does

- Polls Beeminder every minute (configurable).
- After your configured reminder start time, sends a reminder every 5 minutes until you hit your daily target.
- Auto-pauses reminders briefly when progress increases (to avoid pinging while you are actively working).
- Supports manual controls: started, b-snooze, resume, status.

## Required env vars

- `BEEMINDER_AUTH_TOKEN`
- `BEEMINDER_USERNAME`
- `BEEMINDER_GOAL_SLUG`
- `DISCORD_HUB_NOTIFY_URL`
- `BEEMINDER_NOTIFY_CHANNEL`

Copy `cmd/beeminder-spoke/.env.example` to `cmd/beeminder-spoke/.env` and fill values.

`beeminder-spoke` loads env files in this order: `cmd/beeminder-spoke/.env`, then `.env` (legacy fallback).

## Run locally

```bash
go run ./cmd/beeminder-spoke
```

## Control API

The spoke exposes a small local API:

- `GET /status`
- `GET /control/commands`
- `POST /control/command`
- `POST /control/started`
- `POST /control/snooze`
- `POST /control/resume`

Examples:

```bash
curl -X POST http://127.0.0.1:8090/control/started

curl -X POST http://127.0.0.1:8090/control/snooze \
  -H "Content-Type: application/json" \
  -d '{"duration":"45m"}'

curl -X POST http://127.0.0.1:8090/control/command \
  -H "Content-Type: application/json" \
  -d '{"command":"status","context":{"discordUserId":"local-user"}}'

curl -X POST http://127.0.0.1:8090/control/command \
  -H "Content-Type: application/json" \
  -d '{"command":"b-snooze","context":{"discordUserId":"local-user"},"options":{"duration":"1h"}}'
```

Command names are configurable via env (`BEEMINDER_COMMAND_*`), so Discord slash mappings can stay outside `discord-hub` and be owned by the Beeminder spoke.

`GET /control/commands` returns a command catalog with command descriptions and option metadata. `discord-hub` reads this catalog and mirrors it as slash commands.

`discord-hub` discovers these commands through `SPOKE_COMMAND_SERVICES` entries that point to:

- `GET /control/commands`
- `POST /control/command`
