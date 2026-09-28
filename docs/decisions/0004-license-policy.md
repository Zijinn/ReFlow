# ADR 0004: GPL core and source separation

Status: accepted

## Decision

ReFlow is licensed GPL-3.0-only. MrRSS-derived work retains notices. Folo is a product and interaction reference only; its AGPL source is not copied. Fluent Reader BSD code may be ported when its copyright notice and license are retained.

## Rationale

The selected backend foundation is GPL-licensed. Avoiding direct Folo source reuse keeps the project from acquiring AGPL obligations. Explicit provenance makes later distribution auditable.

## Consequences

- Every ported file records its origin when applicable.
- Third-party notices ship with release artifacts.
- A future proprietary relicensing would require replacing all GPL-derived implementation.

