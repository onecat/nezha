# Nezha Dashboard v0.30.05

Security and reliability maintenance release for the V0.20.13 local-auth Dashboard. Linux AMD64, single executable with embedded web resources.

## Changes
- Go 1.27.4 and updated dependencies; remove obsolete OAuth SDKs.
- Local authentication, API token ownership, CSRF, visitor cookie and hidden-server access hardening.
- Terminal/file/NAT session ownership and Agent identity verification, bounded streams, deadlines and legacy NAT EOF handling.
- Track dispatched monitoring and command task receipts; serialize per-Agent task sends.
- Dashboard webhook destination policy, static root confinement, embedded frontend upgrades and locally reviewed Vue/Bootstrap mitigations.
- Preserve V0.20.5 Agent wire protocol and existing SQLite schema.

## Upgrade
Back up the existing executable, config.yaml and SQLite database before stopping Dashboard and replacing the executable. Verify SHA256SUMS. Existing local administrator configuration is required. No Agent upgrade is required for the tested V0.20.5 features.

Private notification/DDNS webhook destinations now require explicit webhookallowedcidrs configuration; environment proxies and cross-origin redirects are not used by these dedicated webhook clients. Old visitor view-password cookies expire on restart. Outstanding task receipts are process-local and lost on restart. Custom themes may need independent offline compatibility checks.

## Validation and limitations
Release is gated on full Go tests, vet, race tests, Go vulnerability scanning, frontend advisory disposition and real V0.20.5 Agent/browser integration (monitoring, reconnect, six themes, terminal, file checksums and NAT), plus a short resource regression test. This is not a long-duration production soak or a guarantee of zero vulnerabilities. Vue 2 and Bootstrap 3 are EOL; three raw npm advisories are retained with reviewed mitigations and tests. See SECURITY_V03005.md for scope and limitations.
