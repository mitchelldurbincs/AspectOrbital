# accountability-spoke

`accountability-spoke` manages personal commitments, deadlines, and proof submissions via a local control API.
It can also send overdue reminder pings to `discord-hub` and lets you snooze reminder spam with a slash command.

## What it does

- Exposes slash-command metadata and execution endpoints for `discord-hub`.
- Tracks one active commitment per user in SQLite.
- Sends overdue reminders via `discord-hub /notify`.
- Supports snoozing reminders with `/a-snooze`.
- Applies proof policies from `policies.json` (including optional `openai_vision`).

## Local run

Configure values in the repository root `.env`.

```bash
go run ./cmd/accountability-spoke
```

## Commands

Default command names are configurable with `ACCOUNTABILITY_COMMAND_*` env vars.

- `/commit deadline:"..." [task:"..."] [preset:"..."]`
- `/proof [proof:<attachment>] [text:"..."]`
- `/accountability-status`
- `/cancel`
- `/a-snooze [duration:"10m"]`

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
