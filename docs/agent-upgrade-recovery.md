# Durable upgrade recovery contract (development)

The shared `RecoveryDecision` and `ValidateActiveTransactions` functions
validate snapshots loaded from durable storage. They do not implement storage,
installation, rollback or an upgrade endpoint. Persist each transition before
any external side effect, using a transaction and a per-Agent uniqueness guard.

On startup, a persisted install, health-check or rolling-back transaction
requires the local supervisor to perform rollback or resume an already durable
rollback. Do not infer success from process restart or a Dashboard receipt.
A partially transferred package can only resume after revalidating its immutable
digest, authenticated identity, chunk offsets and expiry. Expired pre-install
transactions abort. Terminal states remain audit records and cannot be resumed.

The durable store must prevent concurrent active transactions for the same
Agent, including across multiple Dashboard instances. This helper only validates
a supplied snapshot; it does not provide locking or enforce uniqueness by itself.
The Agent must separately fsync its monotonic release sequence and rollback
journal, using platform-appropriate atomic durability guarantees.
