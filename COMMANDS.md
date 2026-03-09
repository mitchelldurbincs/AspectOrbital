# Discord command reference

This file lists the Discord slash commands exposed by `discord-hub` itself and by each spoke.

## `discord-hub` (built-in)

- `/ping` — checks whether `discord-hub` is alive.

## `beeminder-spoke` (discovered via `/control/commands`)

Default command names (all configurable via `BEEMINDER_COMMAND_*` env vars):

- `/started` — pause reminders while you get started.
- `/snooze` — pause reminders for a duration.
  - option: `duration` (string, optional)
- `/resume` — resume reminders immediately.
- `/status` — show progress and next reminder time.

## `accountability-spoke` (discovered via `/control/commands`)

Default command names (all configurable via `ACCOUNTABILITY_COMMAND_*` env vars):

- `/commit` — commit to a task with a deadline.
  - options:
    - `task` (string, required)
    - `goal` (string, required)
    - `deadline` (string, required; RFC3339, unix seconds, or duration)
- `/proof` — submit proof for your active commitment.
  - option: `proof` (attachment, required)
- `/status` — show your active commitment.
- `/cancel` — cancel your active commitment.

## `finance-spoke` (discovered via `/control/commands`)

- `/finance-status` — show scheduler configuration and summary state snapshot.

## `kalshi-spoke` (discovered via `/control/commands`)

- `/kalshi-status` — show monitor runtime state and persisted trigger state.
