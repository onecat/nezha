# Nezha V0.20.13 — local administrator login

This branch derives from the exact upstream tag `v0.20.13` and retains the old V0 Agent protocol.
Only the dashboard authentication/login page is modified.

## Enable local login

Existing V0 data/config.yaml:
```yaml
oauth2:
  type: local
localauth:
  username: admin
  passwordhash: '$2y$12$REPLACE_WITH_BCRYPT_HASH'
```

Generate bcrypt password hash on Debian: `apt install apache2-utils && htpasswd -nBC 12 admin`.
Copy only the string after the colon. Do not put a plaintext password into config.
Existing SQLite DB is reused, and original OAuth login remains available by
switching `oauth2.type` back to `github`, `gitee`, `oidc`, etc., with original credentials.

Use Nginx reverse proxy with `X-Forwarded-Proto https` and trusted `nz-realip`.
The local login form uses CSRF protection, bcrypt, a 15-minute 5-failure IP limit,
HttpOnly cookies, and clears local tokens on dashboard startup.
Changing the password currently requires changing the bcrypt hash in YAML and restarting Dashboard.
No web UI for password rotation is included in this initial version.

## Build

Push to branch `v0.20.13-local-auth`; the dedicated GitHub Actions
workflow tests and builds a Linux AMD64 binary, then publishes it as a GitHub Release.
Each successful run has its own version tag (v0.20.13-local-rN).
Download the latest binary and checksum from:

- https://github.com/onecat/nezha/releases/latest/download/nezha-app
- https://github.com/onecat/nezha/releases/latest/download/SHA256SUMS

This workflow does not build Agent or Docker images.

Local build: `go test ./model ./pkg/mygin ./cmd/dashboard/controller && go build -o dashboard ./cmd/dashboard`.

Warning: V0 is EOL; protect the dashboard and terminal routes through HTTPS
and strict access controls. Build success alone is not a production security audit.
