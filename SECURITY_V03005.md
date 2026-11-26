# v0.30.05 security development
Baseline: v0.30.03 (f2907344806b131235b4daab19ced8a027a33dfa).
Development branch only; CI never creates a Release or modifies production assets.
Initial changes: Go 1.27.4 CI, full tests/vet/race, vulnerability report, API token owner lookup and regression tests.
Next: resolve Go/dependency compatibility, remove unused OAuth SDKs, verify advisories against V0 code, fix terminal/file session authorization, CSRF, task ownership, bounded websocket resources, login concurrency and configurable SSRF. Preserve V0 Agent wire compatibility, SQLite compatibility and bundled assets. Verify all built-in themes with external requests blocked. No production release until acceptance and review; outstanding risks must be recorded.

Dependency remediation: remove unused provider converters and OIDC packages; preserve model.User database fields. The development CI resolves current stable versions of security-sensitive modules, uploads the exact module manifests, and passes them to both verification and vulnerability scanning jobs. Commit resolved manifests and restore read-only dependency verification before release.

Resolved dependency lockfiles from CI run 45786203675 are committed. Go module now declares 1.27.0; toolchain remains 1.27.4. CI no longer resolves moving dependency versions; it verifies the checked-in manifests. Full build and vulnerability rescan remain required.

Browser mutation hardening: require exact Origin or Referer for authenticated non-safe browser requests, preserve explicit API token clients, change cron manual trigger and file creation to POST. Login reserves attempts atomically before bcrypt; at most four concurrent password checks; forwarded HTTPS is trusted only from loopback proxies. Tests added. Pending CI and browser regression.

Stream hardening: bind each terminal/file stream to authenticated user, expected Agent and stream kind, including Agent binding for NAT. Reject duplicate peers, strictly validate existing V0 stream header, cap shared streams at 256, expire pending streams after 30 seconds, bound incoming WebSocket frames at 4 MiB (upload chunks are 1 MiB), limit public stats connections, and use write deadlines. Tests cover ownership, duplicate connections, forwarding and capacity. NAT and real V0 Agent interoperability require further validation.

CI run 45786429938 passed build, test, vet and race, leaving only GO-2026-6278 and GO-2026-5676 (2 reachable findings versus 14 previously). Resolve exactly the advisory-fixed Gorilla WebSocket pseudo-version and quic-go v0.59.1, then pin generated manifests after CI resolution. Keep existing 1 MiB IO copy buffer to avoid changing file-message fragmentation characteristics; the active stream cap bounds aggregate allocation. No release until pinned manifests and all checks pass.

CI 45791386866: no Go vulnerabilities found; all test/vet/race/build checks passed. Verified dependency manifests pinned. Task receipt ledger reserves before dispatch and accepts only matching Agent, task ID and type, consumes each outstanding reservation once, bounds storage and expires monitor receipts after five minutes and command receipts after 24 hours. V0 protocol lacks a unique dispatch nonce, so overlapping runs of the same task cannot be distinguished; in-flight results across Dashboard restart are dropped. Actual Agent regression remains necessary.
