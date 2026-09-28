# ADR 0002: React PWA client

Status: accepted

## Decision

The client uses React 19, TypeScript, Vite, TanStack Query, Zustand, Radix interaction primitives, Phosphor icons, and ReFlow-owned CSS variables. The same client is embedded by desktop and installed as a PWA on iPad.

## Rationale

This supports Folo-inspired information architecture without importing Folo code. React has mature virtualization, query caching, PWA, and accessibility tooling. One responsive client shortens the path to an iPad release.

## Consequences

- The MrRSS Vue frontend is not migrated.
- Server data stays in TanStack Query rather than a duplicate global store.
- Offline state is explicit and bounded rather than making the browser database authoritative.

