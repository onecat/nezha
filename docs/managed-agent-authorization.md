# Managed Agent upgrade authorization boundary (development)

This branch now includes a **pure fail-closed policy check**, not an enabled endpoint.
The default `AuthorizationPolicy{}` denies every request.

Before any transfer, the Dashboard must independently establish:
- authenticated operator identity and a dedicated upgrade permission;
- operator's access to the specific ServerID, from the server-side database;
- a server-side, explicit managed-upgrade opt-in (default off);
- authenticated Agent identity, protocol capability and version (v0.20.5 must migrate manually);
- immutable package bytes already verified by VerifyPackage against pinned trusted release keys;
- durable high-water release sequence, not a client-supplied value;
- a single pending task with package digest, version, sequence and deadline bound to ServerID.

AuthorizationPolicy does **not** authenticate sessions, provision signing keys,
persist high-water marks, prevent concurrent scheduling, or implement transport.
Its boolean inputs must be set only after the above independent checks. Do not
treat this helper as a substitute for an endpoint's authorization middleware.
In particular, package verification must happen before invoking this helper.

No runtime download URL, third-party updater, private signing key, or production
enablement is introduced by this change.
