# Kalshi NO-Side Rule Plan

This plan extends the new `kalshi-spoke` rule model from YES-only to dual-side support while staying aligned with the Kalshi API.

## Current API facts to preserve

- WebSocket ticker updates include `yes_bid_dollars` and `yes_ask_dollars` on the ticker channel: `https://docs.kalshi.com/websockets/market-ticker.md`
- Market details include both `yes_bid_dollars` / `yes_ask_dollars` and `no_bid_dollars` / `no_ask_dollars`: `https://docs.kalshi.com/api-reference/market/get-market.md`
- Order creation is side-specific and supports `side` = `yes | no`, with side-specific order prices via `yes_price_dollars` and `no_price_dollars`: `https://docs.kalshi.com/api-reference/orders/create-order.md`
- WebSocket subscriptions for ticker data are still done through the normal authenticated connection flow: `https://docs.kalshi.com/websockets/websocket-connection.md`

## Goal

Allow rules like:

- `PRES24 yes bid crosses above 0.8500`
- `RAIN-NYC no bid crosses above 0.1500`

without duplicating the trigger engine.

## Constraints

- Keep one generic rule engine.
- Keep market subscription separate from rules.
- Do not infer NO bid from YES bid unless Kalshi explicitly guarantees the mapping for the exact field we need.
- Keep existing YES rules working without migration pain.

## Recommended implementation phases

### Phase 1: Extend the rule model

- Add `TriggerRuleSide::No` to `cmd/kalshi-spoke/src/rules.rs`.
- Keep `TriggerPriceSource::Bid` only for now.
- Keep `TriggerDirection::CrossesAbove` only for now.
- Update rule parsing helpers so env/bootstrap and commands can specify side.

Suggested command/API shape:

- `kalshi-rule-set`
  - `ticker`
  - `side`
  - `threshold_dollars`

Suggested env shape:

- `KALSHI_TRIGGER_RULES=PRES24:yes=0.8500,RAIN-NYC:no=0.1500`

This keeps the current bootstrap flow but makes side explicit.

### Phase 2: Capture the right market data

- Expand runtime ticker snapshots to track both YES and NO bid/ask values if available.
- Before relying on websocket-only NO rules, confirm whether the ticker channel actually emits `no_bid_dollars` in practice for the subscribed markets.
- If websocket NO bid is not consistently present, add a fallback strategy:
  - either hydrate from `GET /markets/{ticker}`
  - or subscribe to a richer channel if Kalshi exposes one that is a better fit

Important: prefer direct NO-side values from Kalshi over synthetic inversion from YES-side values.

### Phase 3: Generalize trigger evaluation

- Replace YES-specific price extraction in `cmd/kalshi-spoke/src/triggers.rs` with a side-aware selector.
- Example internal helper:

```text
observed_value_for(rule, ticker_update) -> Option<Decimal>
```

- For YES bid rules, select `yes_bid_dollars`.
- For NO bid rules, select `no_bid_dollars` once we trust the source.
- Keep the rest of the crossing logic unchanged.

### Phase 4: Generalize trading actions

- Refactor `cmd/kalshi-spoke/src/trading.rs` and `cmd/kalshi-spoke/src/kalshi.rs` so order creation uses the rule side.
- Order payloads should map like:
  - YES rule -> `side=yes`, `action=sell`, `yes_price_dollars=<threshold>`
  - NO rule -> `side=no`, `action=sell`, `no_price_dollars=<threshold>`
- Add side-aware position lookup if needed for NO holdings.

### Phase 5: Commands, summaries, and docs

- Update `kalshi-rules` output to show side explicitly.
- Update `kalshi-rule-set` and `kalshi-rule-remove` docs/examples.
- Add README notes explaining why side-specific order fields exist in the Kalshi API.

## Risks to resolve before shipping NO-side support

- Whether websocket ticker payloads expose reliable NO-side bid data for the exact markets we monitor.
- Whether NO-side reduce-only sell behavior lines up cleanly with current position-fetching assumptions.
- Whether any existing persisted YES-only rule ids need migration when `side` becomes user-configurable.

## Acceptance criteria

- A YES rule and a NO rule can coexist for different markets.
- Rules remain observe-only when no active rule exists.
- Trigger summaries show side, threshold, and mode clearly.
- Order submission uses Kalshi's side-specific order fields correctly.
- Existing YES-only users can upgrade without losing persisted rule state.
