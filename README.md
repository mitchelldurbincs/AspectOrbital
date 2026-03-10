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

- CI pipeline for Go/Rust linting, build, tests, and Docker Compose smoke + health checks
- Hub-and-spoke architecture with HTTP-based service discovery and command registration
- Discord slash commands for Beeminder reminders (`/started`, `/b-snooze`, `/resume`, `/status`)
- Discord slash commands for accountability tracking (`/commit`, `/proof`, `/checkin`, `/status`, `/cancel`, `/a-snooze`)
- Discord slash commands for finance and Kalshi monitoring (`/finance-status`, `/kalshi-status`, `/kalshi-positions`, `/kalshi-rules`, `/kalshi-rule-set`, `/kalshi-rule-remove`)
- Configurable reminder engine with snooze, grace periods, and active-session detection
- Plaid integration for recurring subscription detection and weekly summary generation
- Kalshi WebSocket price streaming with configurable trigger thresholds
- Multi-stage Dockerfiles for all services with non-root execution
- Docker Compose orchestration for local and deployment use
- Shared Go packages (`pkg/`) for logging, lifecycle management, hub notifications, and config utilities
- Strict environment variable validation across all spokes (no silent fallback defaults)
- Single root `.env` configuration shared by local runs and Docker Compose

### In progress / not yet complete

- **CD deployment** — production/home-server deploy automation is not wired yet
- **Test coverage** — coverage is still uneven, but automated tests now exist in `discord-hub`, `beeminder-spoke`, `accountability-spoke`, `finance-spoke`, and shared `pkg/` modules
- **Kubernetes** — stub directory exists under `deployments/kubernetes/` but no active manifests
- **Accountability spoke compose wiring** — service exists and is documented, but it is not yet added to `deployments/docker-compose.yml`

## CI

GitHub Actions runs on push/PR to `main` and currently includes:

- Go: `gofmt` check, `golangci-lint`, `go build`, `go vet`, and `go test`
- Rust (`kalshi-spoke`): `cargo build`, `cargo test`, `cargo fmt -- --check`, and `cargo clippy -- -D warnings`
- Docker Compose smoke test: config validation, image build, startup, and `/healthz` checks for `beeminder-spoke`, `finance-spoke`, and `kalshi-spoke`

`discord-hub` is included in compose image builds, but runtime health checks are intentionally skipped in CI for now because it requires a live Discord bot session.

## Quick Start

```bash
# 1. Clone and configure
cp .env.example .env
# Edit .env with your tokens and channel mappings (see comments in .env.example)

# 2. Run with Docker Compose
cd deployments
docker compose up --build
```

All services read configuration from the root `.env` file.

## Documentation

- [COMMANDS.md](COMMANDS.md) — Discord slash command reference
- [CONTRIBUTING.md](CONTRIBUTING.md) — Spoke lifecycle patterns and shutdown conventions
- [INFORMATION_SECURITY_POLICY.md](INFORMATION_SECURITY_POLICY.md) — Baseline security controls and governance
- [PRIVACY.md](PRIVACY.md) — Privacy policy for Plaid-connected finance workflows
- [DATA_RETENTION_POLICY.md](DATA_RETENTION_POLICY.md) — Data retention and deletion policy
- [contracts/spoke-contract-v2.schema.json](contracts/spoke-contract-v2.schema.json) — canonical spoke/hub wire contract
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
