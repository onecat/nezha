# Managed Agent upgrade — v0.30.06 development

Status: implementation started; no runtime upgrade endpoint or installation capability enabled.

Baseline: Dashboard v0.30.05, Agent v0.20.5. Legacy Agents require manual migration. No production branch changes. Keep all existing RPC names, protobuf field numbers, and legacy task types 1–11. Legacy task type 6 must not be reactivated.

## Implemented foundation

- Common NZU verifier: bounded ZIP, exact three entries, raw signed JSON, Ed25519 pre-provisioned keys, SHA256, immutable package digest and forward-only release policy.
- Offline package builder and CLI with independently verified output, Windows/Linux CI tests; signing key never belongs on Dashboard or CI.
- Immutable-source bounded chunk sender with full pre-stream digest verification and round-trip sender/receiver tests. Sender is not wired to gRPC or an authorization endpoint.\n- Shared transaction transition validator with identity/package binding and expiry; rollback is allowed after expiry. **This is not durable storage or an Agent installer.**
- Release sequence is a monotonically increasing uint64; the Agent must persist its highest accepted sequence independently of rollback. Version numbers allow the project's zero-padded notation.
- Signature file contains exactly 64 raw bytes; manifest is UTF-8 JSON. Linux/macOS/FreeBSD use nezha-agent; Windows uses nezha-agent.exe. Allowed platform tuple does NOT imply upgrade capability.

## Proposed wire allocation (not yet registered)

- Reserve task type 12 for managed upgrade. Never dispatch to a legacy Agent or an Agent that has not explicitly advertised support.
- Add Host fields 14–17 for managed upgrade protocol version, GOOS, GOARCH, and enabled flag; platform information is not authorization.
- A new upgrade RPC should bind authenticated server identity, task ID, immutable package SHA256 and expiry. Agent initiates stream only after explicit task; Dashboard provides bounded chunks with offsets. Health acknowledgements must bind the exact transaction and authenticated Agent.
- Require certificate-verified TLS; no plaintext or insecure TLS upgrade path.
- Keep one durable active transaction per Agent. Local upgrade policy defaults disabled. Rollback supervisor and secure staging are mandatory before any install endpoint is enabled.

## Remaining work

Create independent onecat/nezha-agent repository (current connector has no create/fork operation); confirm upstream license and preserve notices, clean upstream networking/dependencies, implement generated protobuf and authenticated streaming, durable state and platform supervisor, Dashboard storage/scheduler, server-list toolbar modal, audit, signed offline release tooling and full cross-platform integration. No releases until real upgrade and rollback are verified.
