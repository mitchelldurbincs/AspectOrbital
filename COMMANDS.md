# Discord command reference

This file lists the Discord slash commands exposed by `discord-hub` itself and by each spoke.

## `discord-hub` (built-in)

- `/ping` — checks whether `discord-hub` is alive.

## `beeminder-spoke` (discovered via `/control/commands`)

Default command names (all configurable via `BEEMINDER_COMMAND_*` env vars):

- `/started` — pause reminders while you get started.
- `/b-snooze` — pause reminders for a duration.
  - option: `duration` (string, optional)
- `/resume` — resume reminders immediately.
- `/status` — show progress and next reminder time.

## `accountability-spoke` (discovered via `/control/commands`)

Default command names (all configurable via `ACCOUNTABILITY_COMMAND_*` env vars):

- `/commit` — commit to a task with a deadline.
  - options:
    - `deadline` (string, required; RFC3339, unix seconds, duration, or clock time like `4:30am`)
    - `task` (string, optional; defaults from preset)
    - `preset` (string, optional; defaults to global defaultPreset)
- `/proof` — submit proof for your active commitment.
  - options:
    - `proof` (attachment, optional)
    - `text` (string, optional)
- `/checkin` — record that you're actively working on your commitment.
  - options:
    - `text` (string, required)
- `/status` — show your active commitment.
- `/a-snooze` — snooze reminders for your active commitment.
  - option: `duration` (string, optional; example `10m`)
- `/cancel` — cancel your active commitment.

## `finance-spoke` (discovered via `/control/commands`)

- `/finance-status` — show scheduler configuration and summary state snapshot.

## `kalshi-spoke` (discovered via `/control/commands`)

- `/kalshi-status` — show monitor runtime state and persisted trigger state.
- `/kalshi-positions` — show YES/NO contract exposure with market titles and tickers.
- `/kalshi-rules` — show tracked markets in trigger-enabled vs observe-only mode.
- `/kalshi-rule-set` — set a YES bid crossing rule for a ticker.
  - options:
    - `ticker` (string, required)
    - `threshold_dollars` (number, required; between 0 and 1)
- `/kalshi-rule-remove` — remove a ticker rule and return to observe-only mode.
  - options:
    - `ticker` (string, required)
