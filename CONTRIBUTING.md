# Contributing

## Spoke lifecycle pattern

When implementing a long-running spoke process (`cmd/*-spoke/main.go`), follow this shutdown lifecycle:

1. **Top-level entrypoint logs once**: keep `main()` minimal (`if err := run(logger); err != nil { logger.Printf(...) }`) and avoid calling `Fatalf` from worker/select loops.
2. **Run loop returns errors**: use an error-returning loop (for example `lifecycle.WaitForExit`) that watches:
   - OS signal channel (`os.Interrupt`, `syscall.SIGTERM`)
   - HTTP server error channel
   - periodic ticker channel for background work
3. **Centralized shutdown**: once the run loop exits, always:
   - cancel any background loop contexts
   - create a bounded context via `context.WithTimeout`
   - call `http.Server.Shutdown`
   - log shutdown errors as non-fatal diagnostics
4. **Operational failures inside periodic work are non-fatal**: log and continue polling.

This keeps service shutdown behavior consistent and testable across spokes.
