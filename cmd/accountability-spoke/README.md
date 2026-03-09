# accountability-spoke

`accountability-spoke` manages personal commitments, deadlines, and proof submissions via a local control API.

## Local run

Copy `cmd/accountability-spoke/.env.example` to `cmd/accountability-spoke/.env` and set values for your setup.

```bash
go run ./cmd/accountability-spoke
```

## Env loading precedence

At startup, env files are loaded in this order:

1. `cmd/accountability-spoke/.env`
2. `.env` (legacy fallback)

Later files only fill variables that are still unset, so values in `cmd/accountability-spoke/.env` take precedence.
