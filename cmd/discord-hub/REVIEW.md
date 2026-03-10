# Code Review: discord-hub -- REQUEST CHANGES

## 1. Spoke Bridge Concurrency: Accidental Safety

`spoke_runtime.go:43-57` -- `storeSyncResult` replaces `b.bridge` with a new pointer, but
`ExecuteCommand` in `execute.go:16` operates on the old Bridge struct returned by
`currentBridge()` with no lock held during execution. The bridge's internal maps (`commands`,
`commandOwners`) are replaced wholesale during sync. If a sync completes mid-execution, you're
reading from a stale bridge -- which is fine -- but only by accident because you do a full
pointer swap. If anyone ever mutates the existing bridge in place (which the architecture
invites), you get a data race. There's zero documentation that this "copy-on-write via pointer
swap" behavior is intentional and load-bearing.

## 2. ExecuteCommand Silent Fallback is an Open Proxy

`execute.go:24-26` -- If a command isn't in `commandOwners` but there's only one service, it
silently routes to that service. Any garbage command name gets forwarded to your backend. The
moment someone typo-squats a command name or Discord delivers a stale interaction, you're
sending unknown commands to a service that doesn't expect them.

## 3. Discovery Retry is a time.Sleep Bomb

`discover.go:118-131` -- 8 retries x 2-second sleeps = 16 seconds blocking per service, no
backoff. With 3 down services: 48 seconds of goroutine sleep. During this, `syncInProgress` is
true, so all spoke commands return "still syncing." Runs every 2 minutes. With consistently slow
services, you get perpetual "syncing" windows.

## 4. No Message Length Validation on /notify

`handlers_notify.go:66-69` -- Critical mention is prepended to the message with no check
against Discord's 2000-character limit. `TruncateForDiscord` is called for spoke responses but
not for notifications. A 1990-char message + mention = Discord 400 rejection with an
uninformative error log.

## 5. buildChannelMap Silently Eats Malformed Config

`channel_map.go:8-37` -- Malformed DISCORD_CHANNEL_MAP entries are silently skipped with no
startup warning. Debugging means staring at env vars. Also, the second loop (lines 30-35)
checking for empty values is dead code -- already guarded on line 22.

## 6. resolveApplicationID is Fragile

`app.go:94-100` -- Only checks `session.State.User.ID`, which depends on the READY event.
Delayed or malformed READY = opaque error. No retry, no fallback to `/users/@me` API.

## 7. Stale Commands Are Never Cleaned Up

`discord_commands.go:26-39` -- Commands are created/updated but never deleted when removed from
spoke catalogs. Removed commands persist in Discord forever. Users see them, try them, get
"not available." The diff between existing and desired is trivial to compute.

## 8. Single Shared Auth Token

`handlers_notify.go:87-100` -- One bearer token for all /notify callers. No per-service tokens,
no scoping, no selective revocation.

## 9. No Rate Limiting

Any caller with the token can flood Discord channels. Discord 429s are not handled specifically,
producing generic "failed to send" errors. A misbehaving service exhausts rate limit budget for
all callers.

## 10. TruncateForDiscord Counts Bytes, Not Characters

`discord.go:51-61` -- `len(message)` returns byte count. Discord limits are in Unicode
characters. Multi-byte characters (emoji, CJK) will be truncated too aggressively or sliced
mid-character, producing invalid UTF-8.

## 11. Ephemeral-Only Responses Are Hardcoded

Every spoke command response is ephemeral. Users cannot share command outputs. Should be
configurable per-command.

## 12. interactionHandler Swallows Context

`discord_interactions.go:49` -- Fresh `context.Background()` instead of using the interaction
context. Shutdown won't cancel in-flight executions; they'll run for up to 8 seconds after
the HTTP server stops.

## Edge Cases Missing

- **Interaction token expiry**: Tokens expire after 15 minutes. Slow spoke services approaching
  this window will cause silent followup failures.
- **Concurrent sync overlap**: Two syncs could overlap with slow services, upserting conflicting
  command sets.
- **Service name collisions**: Auto-generated names (`service-1`) can collide with explicit
  names. Last one wins silently.
- **Empty spoke response body**: 200 with empty body fails JSON unmarshal. Command succeeded but
  user sees "invalid spoke command response."
- **Channel map key casing**: Names aren't normalized to lowercase. `"Alerts:123"` won't match
  `"targetChannel": "alerts"`.
