# Dashboard task storage (development)

The service/agentupgrade package uses the existing GORM/SQLite dependencies.
It creates a new table only when Open is explicitly called. Dashboard startup
and all HTTP/gRPC routes remain unchanged; the feature is not enabled.

The immutable task identity and current state are stored as the bounded core
snapshot. A nullable UNIQUE active_server_id reserves one active task per
server across process restarts. Terminal tasks retain their primary key to
prevent ID reuse. Expired active tasks remain reserved until a validated
terminal transition. The revision and previous snapshot form a compare-and-swap
predicate. No automatic cleanup or deletion is provided.

The store does not authenticate operators or Agent receipts. Scheduler
integration must verify authorization, exact target identity, package trust,
and health proof before calling Create or Advance. A database error is not an
authorization success; callers must fail closed. SQLite busy errors are not
silently retried. Recovery uses bounded pages; a recovery page is not a lease.
A separate durable audit, release repository and Agent-side high-water mark
are still required. The storage API is intentionally not exposed to users yet.

Tests exercise real file-backed SQLite, reopening, duplicate reservations,
concurrent creation, stale revisions, invalid transitions, expiration,
task ID reuse and corrupted persisted rows. The development CI adds Linux
CGO-enabled SQLite tests; this is separate from the platform-neutral core tests.
