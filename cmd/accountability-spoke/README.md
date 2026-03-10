# accountability-spoke

`accountability-spoke` manages personal commitments, deadlines, and proof submissions via a local control API.
It can also send overdue reminder pings to `discord-hub` and lets you snooze reminder spam with a slash command.

## What it does

- Exposes slash-command metadata and execution endpoints for `discord-hub`.
- Tracks one active commitment per user in SQLite.
- Sends overdue reminders via `discord-hub /notify`.
- Supports lightweight `/checkin` progress pings before the deadline.
- Supports snoozing reminders with `/a-snooze`.
- Applies proof policies from `policies.json` (including optional `openai_vision`).

## Local run

Configure values in the repository root `.env`.

Minimum required settings for a local run:

- `ACCOUNTABILITY_SPOKE_HTTP_ADDR` - control API listen address.
- `ACCOUNTABILITY_DB_PATH` - SQLite DSN or file path.
- `ACCOUNTABILITY_EXPIRY_POLL_INTERVAL` - overdue sweep interval.
- `ACCOUNTABILITY_EXPIRY_GRACE_PERIOD` - how long proof can still arrive after the deadline before the commitment is marked failed.
- `ACCOUNTABILITY_REMINDER_INTERVAL` - minimum spacing between reminder notifications.
- `ACCOUNTABILITY_CHECKIN_QUIET_PERIOD` - how long `/checkin` pauses reminders.
- `HUB_NOTIFY_URL` and `HUB_NOTIFY_AUTH_TOKEN` - hub notification endpoint and bearer token.
- `SPOKE_COMMAND_AUTH_TOKEN` - bearer token required by `POST /control/command`.
- `ACCOUNTABILITY_DISCORD_CALLBACK_URL` and `ACCOUNTABILITY_CALLBACK_AUTH_TOKEN` - callback endpoint and bearer token for reminder action buttons.
- `ACCOUNTABILITY_NOTIFY_CHANNEL` and `ACCOUNTABILITY_NOTIFY_SEVERITY` - default target for overdue reminders.
- `ACCOUNTABILITY_POLICY_FILE` - JSON policy catalog path.
- `ACCOUNTABILITY_DEFAULT_SNOOZE` and `ACCOUNTABILITY_MAX_SNOOZE` - slash-command snooze defaults and upper bound.
- `ACCOUNTABILITY_COMMAND_*` - command-name overrides; defaults in `.env.example` are `commit`, `proof`, `checkin`, `status`, `cancel`, and `a-snooze`.

Optional OpenAI settings are only needed for `openai_vision` presets:

- `ACCOUNTABILITY_OPENAI_API_KEY`
- `ACCOUNTABILITY_OPENAI_BASE_URL`
- `ACCOUNTABILITY_OPENAI_MODEL`
- `ACCOUNTABILITY_OPENAI_TIMEOUT`

Minimal example:

```env
ACCOUNTABILITY_SPOKE_HTTP_ADDR=127.0.0.1:8093
ACCOUNTABILITY_DB_PATH=file:accountability.db?_pragma=busy_timeout(5000)
ACCOUNTABILITY_EXPIRY_POLL_INTERVAL=45s
ACCOUNTABILITY_EXPIRY_GRACE_PERIOD=12h
ACCOUNTABILITY_REMINDER_INTERVAL=5m
ACCOUNTABILITY_CHECKIN_QUIET_PERIOD=10m
HUB_NOTIFY_URL=http://127.0.0.1:8080/notify
HUB_NOTIFY_AUTH_TOKEN=replace-with-long-random-token
SPOKE_COMMAND_AUTH_TOKEN=replace-with-long-random-token
ACCOUNTABILITY_DISCORD_CALLBACK_URL=http://127.0.0.1:8093/discord/callback
ACCOUNTABILITY_CALLBACK_AUTH_TOKEN=replace-with-long-random-token
ACCOUNTABILITY_NOTIFY_CHANNEL=accountability
ACCOUNTABILITY_NOTIFY_SEVERITY=warning
ACCOUNTABILITY_POLICY_FILE=cmd/accountability-spoke/policies.json
ACCOUNTABILITY_DEFAULT_SNOOZE=10m
ACCOUNTABILITY_MAX_SNOOZE=60m
ACCOUNTABILITY_COMMAND_COMMIT=commit
ACCOUNTABILITY_COMMAND_PROOF=proof
ACCOUNTABILITY_COMMAND_CHECKIN=checkin
ACCOUNTABILITY_COMMAND_STATUS=status
ACCOUNTABILITY_COMMAND_CANCEL=cancel
ACCOUNTABILITY_COMMAND_SNOOZE=a-snooze
```

```bash
go run ./cmd/accountability-spoke
```

## Commands

Default command names are configurable with `ACCOUNTABILITY_COMMAND_*` env vars.

- `/commit deadline:"..." [task:"..."] [preset:"..."]`
- `/proof [proof:<attachment>] [text:"..."]`
- `/checkin text:"..."`
- `/status`
- `/cancel`
- `/a-snooze [duration:"10m"]`

Reminder action buttons POST to `/discord/callback` and require `Authorization: Bearer <ACCOUNTABILITY_CALLBACK_AUTH_TOKEN>`.

`POST /control/command` requires `Authorization: Bearer <SPOKE_COMMAND_AUTH_TOKEN>` plus `context.discordUserId`.

Deadline input supports:

- RFC3339 (for example `2026-03-10T04:30:00Z`)
- Unix seconds
- durations like `2h`
- clock-time shortcuts like `4:30am` (uses today if still future, otherwise rolls to next day)

## Policy presets

Proof rules are configured with `ACCOUNTABILITY_POLICY_FILE` (JSON). Copy `cmd/accountability-spoke/policies.example.json` to `cmd/accountability-spoke/policies.json` and customize presets.

- `/commit deadline:"4:30am"` uses `defaultPreset` and the preset task.
- `/commit deadline:"4:30am" preset:"photo_checkin"` overrides the preset.
- `/proof` accepts either attachment (`proof`) or text (`text`) depending on the selected preset engine.
- `openai_vision` presets require `ACCOUNTABILITY_OPENAI_API_KEY` and an image attachment URL.

Built-in policy engines:

- `manual_attachment`
- `text_reply`
- `openai_vision`

`openai_vision` config fields:

- `prompt` (required)
- `minConfidence` (optional, default `0.75`)

Policy loading is fail-hard: the spoke does not start if `policies.json` is invalid, references an unknown engine, or references `openai_vision` without OpenAI config.
