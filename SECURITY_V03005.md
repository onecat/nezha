# v0.30.05 security development
Baseline: v0.30.03 (f2907344806b131235b4daab19ced8a027a33dfa).
Development branch only; CI never creates a Release or modifies production assets.
Initial changes: Go 1.27.4 CI, full tests/vet/race, vulnerability report, API token owner lookup and regression tests.
Next: resolve Go/dependency compatibility, remove unused OAuth SDKs, verify advisories against V0 code, fix terminal/file session authorization, CSRF, task ownership, bounded websocket resources, login concurrency and configurable SSRF. Preserve V0 Agent wire compatibility, SQLite compatibility and bundled assets. Verify all built-in themes with external requests blocked. No production release until acceptance and review; outstanding risks must be recorded.

Dependency remediation: remove unused provider converters and OIDC packages; preserve model.User database fields. The development CI resolves current stable versions of security-sensitive modules, uploads the exact module manifests, and passes them to both verification and vulnerability scanning jobs. Commit resolved manifests and restore read-only dependency verification before release.

Resolved dependency lockfiles from CI run 45786203675 are committed. Go module now declares 1.27.0; toolchain remains 1.27.4. CI no longer resolves moving dependency versions; it verifies the checked-in manifests. Full build and vulnerability rescan remain required.

Browser mutation hardening: require exact Origin or Referer for authenticated non-safe browser requests, preserve explicit API token clients, change cron manual trigger and file creation to POST. Login reserves attempts atomically before bcrypt; at most four concurrent password checks; forwarded HTTPS is trusted only from loopback proxies. Tests added. Pending CI and browser regression.
