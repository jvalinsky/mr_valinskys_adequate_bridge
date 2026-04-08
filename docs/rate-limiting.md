# Per-DID Rate Limiting

See also: [docs index](./README.md), [runbook](./runbook.md)

## Overview

The forward bridge implements per-DID rate limiting to prevent noisy ATProto accounts from flooding the processor. It applies before mapping or persistence, so a rate-limited event does not create or update a `messages` row. Reverse sync is not controlled by this limiter.

## How It Works

Each bridged DID gets its own token bucket rate limiter using `golang.org/x/time/rate`:

- **Default limit**: 300 messages per minute (5 msg/sec)
- **Burst capacity**: Equal to the per-minute limit
- **Scope**: Applied to each incoming ATProto event for a given DID before record mapping starts

## Configuration

### CLI Flag

```bash
# Default: 300 msg/min per DID
bridge-cli start

# Disable rate limiting entirely
bridge-cli start --max-msgs-per-did-per-min 0

# Custom limit
bridge-cli start --max-msgs-per-did-per-min 600
```

### Constants

Default values are defined in [`internal/config/constants.go`](../internal/config/constants.go):

| Constant | Default | Description |
|----------|---------|-------------|
| `MaxMessagesPerDIDPerMinute` | 300 | Tokens per minute (0 disables limiting) |
| `RateLimiterCleanupInterval` | 5 | Minutes between stale DID cleanup runs |
| `RateLimiterIdleTimeout` | 10 | Minutes of inactivity before a limiter is removed |

## Behavior

### Rate-Limited Events

When a DID exceeds its rate limit:

1. The event is silently dropped (no DB write)
2. A log message is emitted: `event=rate_limited did=... seq=...`
3. The `bridge_rate_limited_total` metric is incremented

### Stale Limiter Cleanup

Idle limiters are automatically cleaned up to prevent memory growth:

- Every 5 minutes (configurable), the processor checks for stale limiters
- A limiter is stale if its DID has had no activity for 10+ minutes
- Stale limiters are removed from memory

This ensures that abandoned or inactive accounts don't accumulate rate limiter objects indefinitely.

### Disabling Rate Limiting

Set `--max-msgs-per-did-per-min 0` to disable rate limiting entirely. This is useful for:

- Testing and development
- Isolated deployments with trusted accounts
- Debugging message ingestion issues

## Implementation Details

See [`internal/bridge/processor.go`](../internal/bridge/processor.go) for the implementation:

- `Processor.rateLimiters` — map of DID → `*rate.Limiter`
- `Processor.maxMessagesPerMinute` — configured limit
- `getRateLimiter(did)` — retrieves or creates per-DID limiter
- `StartRateLimiterCleanup(ctx, interval, timeout)` — background cleanup goroutine

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `bridge_rate_limited_total` | Counter | Total messages dropped due to rate limiting |

---

## See Also

- [Bridge Operator Runbook](./runbook.md)
- [Bridge Core Code References](./README.md#bridge-core)
