# Nezha internal Dashboard (V0 protocol)

Based on upstream V0.20.13. Local authentication is mandatory; OAuth settings are ignored.
Configure an existing V0 SQLite deployment with:

    localauth:
      username: admin
      passwordhash: '$2y$12$REPLACE_WITH_BCRYPT_HASH'

Use htpasswd -nBC 12 admin, and copy only the hash after the colon. Restart the Dashboard after changes.
Existing OAuth sessions are rejected. Existing API tokens remain supported.
The Agent command shown in the UI assumes that a compatible V0.20.5 executable has already been installed from your own trusted internal distribution; no official installer is invoked.
Agent V0.20.5 itself is unchanged, including public IP discovery.

Run python3 script/vendor-assets.py before compiling; this obtains pinned frontend dependencies from npm at build time, rewrites bundled templates, and embeds assets in the binary. Runtime does not need a CDN for bundled pages. Custom templates and configured monitors, notifications and DDNS remain administrator-controlled.

The workflow publishes Linux AMD64 nezha-app and SHA256SUMS only after tests and compilation.
