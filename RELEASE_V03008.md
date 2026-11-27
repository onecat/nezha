# Dashboard v0.30.08

Linux amd64 only. URL upgrade is available for a same-host TLS proxy deployment with explicit loopback HTTP and gRPC bind addresses and tls: true, unless NZ_DISABLE_URL_UPGRADE=1. An existing NZ_MANAGED_UPGRADE_CONFIG continues to select the original manual mode.

Select one to three enrolled Agents, paste the versioned raw binary URL from the independent onecat/nezha-agent GitHub release, and confirm. The Dashboard retrieves the release checksum and raw binary, validates Linux amd64 ELF and Go component identity, automatically creates a signed package, and admits the tasks. The existing Agent supervisor verifies signatures, durable sequence, exact version and installation receipt; it backs up and rolls back on failed health checks. UI polls task status. No arbitrary download origins, no upstream update URLs. A small explicitly approved batch replaces the manual canary ID in URL mode.

The online signing key is created locally in the private database-adjacent .url-upgrade directory. Back up that directory with the database; do not reset it. This mode trusts the authenticated Dashboard and its configured TLS endpoint and the independent GitHub release account; SHA256SUMS from the same release is an integrity check, not an independent publisher signature. The Supervisor enroll command pins the public key once using certificate-validated TLS and registers the current Agent identity. Old legacy Agents or existing managed ledgers require a one-time explicit migration; never reset a ledger. No production deployment performed.

Release gating includes exact source regression, existing signed upgrade/rollback flow, and a real URL upgrade using the independently published Agent v0.30.01 binary. Only nezha-app and SHA256SUMS are published.
