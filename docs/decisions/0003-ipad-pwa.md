# ADR 0003: iPad PWA before native iOS

Status: accepted

## Decision

The first iPad client is an installable PWA that connects to a trusted ReFlow Server. A native iOS binary is deferred, while the versioned API remains suitable for one.

## Rationale

iPad cannot run the normal ReFlow Go process. A PWA provides touch, external keyboard, home-screen installation, responsive layout, recent-entry caching, and fast iteration without introducing a second UI implementation.

## Consequences

- An iPad requires a reachable desktop or standalone server.
- LAN mode, pairing, and offline behavior are MVP requirements.
- Deep native integrations can be added later through an Expo client.

