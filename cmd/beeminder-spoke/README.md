# beeminder-spoke

`beeminder-spoke` polls Beeminder and sends reminders to `discord-hub` when you are behind what you need to do today.

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
- Tracks one or more goal slugs.
- Uses Beeminder `delta` (road due now) and can also enforce the daily rate floor to keep streak pressure.
- After your configured reminder start time, sends reminders until you hit today's required progress.
- Supports optional reminder escalation schedules and bedtime cutoff.
- Auto-pauses reminders briefly when progress increases (to avoid pinging while you are actively working).
- Supports manual controls: started, b-snooze, resume, status.

## Required env vars

- `BEEMINDER_AUTH_TOKEN`
- `BEEMINDER_USERNAME`
- `BEEMINDER_GOAL_SLUGS` (comma-separated)
- `HUB_NOTIFY_URL`
- `HUB_NOTIFY_AUTH_TOKEN`
- `SPOKE_COMMAND_AUTH_TOKEN`
- `BEEMINDER_NOTIFY_CHANNEL`

Configure these in the repository root `.env`.

`beeminder-spoke` reads configuration from the repository root `.env`.

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
  -H "Authorization: Bearer ${SPOKE_COMMAND_AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"command":"status","context":{"discordUserId":"local-user"}}'

curl -X POST http://127.0.0.1:8090/control/command \
  -H "Authorization: Bearer ${SPOKE_COMMAND_AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"command":"b-snooze","context":{"discordUserId":"local-user"},"options":{"duration":"1h"}}'
```

Command names are configurable via env (`BEEMINDER_COMMAND_*`), so Discord slash mappings can stay outside `discord-hub` and be owned by the Beeminder spoke.

## Behavior config highlights

- `BEEMINDER_REQUIRE_DAILY_RATE=true|false`
  - `true` (default): required progress is `max(beeminder delta, daily rate)`.
  - `false`: required progress is Beeminder `delta` only (emergency-only mode).
- `BEEMINDER_REMINDER_SCHEDULE` can override intervals during the day (example: `16:00=1h,18:00=30m,19:15=5m`).
- `BEEMINDER_BEDTIME=HH:MM` suppresses reminders at/after bedtime in your Beeminder timezone.
- `BEEMINDER_ACTION_URLS=goal=https://...` appends per-goal quick links to reminders (for example, Skritter).
- `BEEMINDER_MAX_SNOOZE` caps `/b-snooze` duration.

`GET /control/commands` returns a command catalog with command descriptions and option metadata. `discord-hub` reads this catalog and mirrors it as slash commands.

`discord-hub` discovers these commands through `SPOKE_COMMAND_SERVICES` entries that point to:

- `GET /control/commands`
- `POST /control/command`

`POST /control/command` requires both `Authorization: Bearer ${SPOKE_COMMAND_AUTH_TOKEN}` and `context.discordUserId`.
