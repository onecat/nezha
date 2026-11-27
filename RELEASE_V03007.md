# Dashboard v0.30.07

Based on the released v0.30.06 and its release-asset cleanup. No Agent source changes. The existing Agent v0.30.00 protocol, signing, upgrade and rollback behavior remain unchanged.

- Optional httpbindhost and grpcbindhost: numeric IPv4/IPv6 addresses. Empty retains the legacy default; explicit loopback is recommended when using same-host Nginx. Managed upgrades still require a loopback gRPC backend; a conflicting explicit address fails startup instead of silently exposing it.
- grpchost and proxygrpcport remain the advertised Agent endpoint, not listener settings.
- Missing browser sessions no longer generate record-not-found SQL errors. GORM logging uses parameterized SQL and suppresses routine missing-record warnings while retaining real errors and slow queries. Debug logging retains parameter placeholders. Raw SQL literals and driver-generated errors are not generically sanitized.
- Obsolete OAuth/OIDC fields remain readable for configuration compatibility but are omitted on save; local authentication remains mandatory.
- HTTP/2 PRI requests reaching the ordinary HTTP endpoint are a proxy/protocol mismatch; this release does not add h2c or suppress such errors.
- Native paired tests explicitly bind both listeners to 127.0.0.1 and verify kernel listener addresses. The arm64 native test is retained; only the amd64 Dashboard binary and SHA256SUMS are packaged. No bundled Agent archives or arm64 Dashboard release attachment.

## Example

```yaml
httpbindhost: 127.0.0.1
httpport: 8008
grpcbindhost: 127.0.0.1
grpcport: 5555
grpchost: agent.example.invalid
proxygrpcport: 4433
tls: true
```

Merge these fields into existing configuration; keep local credentials private and unchanged. Restart is required. Nginx and Dashboard must share the network namespace when using loopback. Back up config and database; verify actual listener addresses after restart. Agent package signing and deployment remain operator-controlled. This development submission does not publish a tag or change existing releases.
