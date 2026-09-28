# ADR 0005: trusted loopback and paired LAN devices

Status: accepted

## Decision

ReFlow listens on loopback by default. LAN mode is explicit and requires device pairing. Device tokens are random bearer credentials stored hashed on the server. CORS is restricted to configured origins. Feed fetching enforces SSRF and response policies.

## Rationale

The iPad client makes the local API a network service. MrRSS server mode exposes sensitive operations without a completed authentication implementation, which is not acceptable for ReFlow.

## Consequences

- Authentication and device management are core domain concerns.
- Remote access over the public internet requires TLS through a documented reverse proxy or the optional built-in certificate/key configuration (`REFLOW_TLS_CERT_PATH` and `REFLOW_TLS_KEY_PATH`). Reverse proxy peers are trusted only when their exact IP or CIDR is listed in `REFLOW_TRUSTED_PROXIES`; classification uses the immediate TCP peer and never `X-Forwarded-For`. Proxy ingress may use an external `Host`, but protected APIs always require a paired device token.
- Swagger security declarations must match implemented middleware.
