# v0.30.05 security development
Baseline: v0.30.03 (f2907344806b131235b4daab19ced8a027a33dfa).
Development branch only; CI never creates a Release or modifies production assets.
Initial changes: Go 1.27.4 CI, full tests/vet/race, vulnerability report, API token owner lookup and regression tests.
Next: resolve Go/dependency compatibility, remove unused OAuth SDKs, verify advisories against V0 code, fix terminal/file session authorization, CSRF, task ownership, bounded websocket resources, login concurrency and configurable SSRF. Preserve V0 Agent wire compatibility, SQLite compatibility and bundled assets. Verify all built-in themes with external requests blocked. No production release until acceptance and review; outstanding risks must be recorded.
