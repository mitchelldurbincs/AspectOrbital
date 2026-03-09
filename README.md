# AspectOrbital

Personal infrastructure automation platform built as a modular microservice ecosystem. A central **Discord bot hub** coordinates specialized service "spokes" that handle accountability tracking, Beeminder goal monitoring, financial subscription summaries, and options trading alerts.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Discord API                       │
└────────────────────────┬────────────────────────────┘
                         │
                ┌────────▼────────┐
                │  discord-hub    │  :8080
                │  (Go)           │
                └──┬────┬────┬───┘
       ┌───────────┤    │    ├───────────┐
       ▼           ▼    │    ▼           ▼
┌────────────┐┌────────┐│┌────────┐┌──────────┐
│ beeminder  ││finance │││kalshi  ││account-  │
│ spoke      ││spoke   │││spoke   ││ability   │
│ (Go) :8090 ││(Go)    │││(Rust)  ││spoke     │
│            ││  :8091 │││  :8092 ││(Go) :8093│
└────────────┘└────────┘│└────────┘└──────────┘
                        │
              External APIs:
              Beeminder, Plaid, Kalshi
```

## Services

| Service | Language | Port | Purpose |
|---|---|---|---|
| **discord-hub** | Go | 8080 | Central Discord bot; routes slash commands to spokes and forwards notifications |
| **beeminder-spoke** | Go | 8090 | Monitors Beeminder goals (e.g. Skritter streaks) and sends configurable reminders |
| **finance-spoke** | Go | 8091 | Generates weekly subscription summaries via Plaid bank account aggregation |
| **kalshi-spoke** | Rust | 8092 | Watches Kalshi prediction market prices and triggers alerts or auto-sells |
| **accountability-spoke** | Go | 8093 | Task commitment tracking with deadlines and proof-of-completion via Discord |

## Project Status

### What's working

- Hub-and-spoke architecture with HTTP-based service discovery and command registration
- Discord slash commands for Beeminder reminders (`/started`, `/snooze`, `/resume`, `/status`)
- Discord slash commands for accountability tracking (`/commit`, `/proof`, `/status`, `/cancel`)
- Configurable reminder engine with snooze, grace periods, and active-session detection
- Plaid integration for recurring subscription detection and weekly summary generation
- Kalshi WebSocket price streaming with configurable trigger thresholds
- Multi-stage Dockerfiles for all services with non-root execution
- Docker Compose orchestration for local and deployment use
- Shared Go packages (`pkg/`) for logging, lifecycle management, hub notifications, and config utilities
- Strict environment variable validation across all spokes (no silent fallback defaults)
- Per-service `.env` files with root-level fallback

### In progress / not yet complete

- **CI/CD** — no automated pipeline; tests and builds are run manually
- **Test coverage** — test files exist for discord-hub and config loading, but coverage is limited across spokes
- **Kubernetes** — stub directory exists under `deployments/kubernetes/` but no active manifests
- **Accountability spoke** — not yet added to `docker-compose.yml`

## Quick Start

```bash
# 1. Clone and configure
cp .env.example .env
# Edit .env with your tokens and channel IDs (see comments in .env.example)

# 2. Run with Docker Compose
cd deployments
docker compose up --build
```

Per-service `.env` files can also be placed in each `cmd/*/` directory for local development. See `.env.example` for the full list of configuration variables.

## Documentation

- [COMMANDS.md](COMMANDS.md) — Discord slash command reference
- [CONTRIBUTING.md](CONTRIBUTING.md) — Spoke lifecycle patterns and shutdown conventions
- [cmd/kalshi-spoke/README.md](cmd/kalshi-spoke/README.md) — Kalshi spoke details
- [cmd/accountability-spoke/README.md](cmd/accountability-spoke/README.md) — Accountability spoke details

## Tech Stack

- **Go 1.24** — discord-hub, beeminder-spoke, finance-spoke, accountability-spoke
- **Rust (2021 edition)** — kalshi-spoke (Axum + Tokio)
- **Docker** / **Docker Compose** — containerization and orchestration
- **discordgo** — Discord bot framework
- **Plaid API** — bank account aggregation
- **Beeminder API** — goal tracking
- **Kalshi API** — prediction market data and trading
