# beeminder-spoke

`beeminder-spoke` polls Beeminder and sends reminders to `discord-hub` when you are behind your daily goal.

## What it does

- Polls Beeminder every minute (configurable).
- After your configured reminder start time (default `19:00`), sends a reminder every 5 minutes until you hit your daily target.
- Auto-pauses reminders briefly when progress increases (to avoid pinging while you are actively working).
- Supports manual controls: started, snooze, resume, status.

## Required env vars

- `BEEMINDER_AUTH_TOKEN`
- `BEEMINDER_USERNAME`
- `BEEMINDER_GOAL_SLUG`
- `DISCORD_HUB_NOTIFY_URL`
- `BEEMINDER_NOTIFY_CHANNEL`

Copy from `.env.example` and fill values.

## Run locally

```bash
go run ./cmd/beeminder-spoke
```

## Control API

The spoke exposes a small local API (default `127.0.0.1:8090`):

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
  -d '{"command":"status"}'
```

Command names are configurable via env (`BEEMINDER_COMMAND_*`), so Discord slash mappings can stay outside `discord-hub` and be owned by the Beeminder spoke.

`discord-hub` can discover these names dynamically via:

- `SPOKE_COMMANDS_URL` -> `GET /control/commands`
- `SPOKE_COMMAND_URL` -> `POST /control/command`
