# Agent upgrade entry — fail-closed UI

The Dashboard server list shows a disabled “升级 Agent” entry. It is intentionally non-interactive and makes no network request. Do not enable it by only removing the HTML disabled attribute.

Required gates before activation:
1. Operator identity and per-server authorization checked server-side; CSRF protection and audit logging.
2. Durable per-Agent enablement defaulting to false; legacy v0.20.5 must be manually migrated.
3. Signed, bounded, immutable release package verification and durable release-sequence anti-rollback.
4. Agent explicitly advertises managed-upgrade support, TLS is certificate-verified, and package/task identities are bound.
5. Persistent task dispatch, bounded transfer, restart-safe Agent supervisor, health check and local automatic rollback.
6. Real end-to-end integration and rollback tests across supported targets, including failure injection.

Current UI is discoverability only; no runtime endpoint or installer is enabled. Production v0.30.05 is untouched.
