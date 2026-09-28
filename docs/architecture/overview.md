# ReFlow architecture

## Context

ReFlow must provide one coherent reading system across Windows, macOS, iPad Safari, and future mobile clients. The desktop application can run a local Go process. iPad cannot, so the backend is designed as an independently runnable service from the first commit.

## Containers

### ReFlow Server

The server owns all durable state and business rules:

- feed discovery, fetch, parse, normalize, and deduplicate;
- subscriptions, folders, tags, entry states, and search;
- persistent scheduling and job execution;
- third-party synchronization;
- AI provider calls and caches;
- device pairing and authentication;
- REST and SSE interfaces.

The server can run inside the desktop application or as a standalone process. It serves the same embedded web assets in both modes.

### ReFlow Web

The React PWA is an API client. It does not parse feeds and does not contain authoritative business state. TanStack Query owns remote cache state. Zustand owns ephemeral layout, selection, view, and shortcut state. IndexedDB contains an explicitly bounded offline cache and a mutation outbox. Desktop navigation exposes library scopes directly; iPad and mobile use the same scopes through a touch-sized Library panel.

### ReFlow Desktop

The desktop adapter controls lifecycle, native menus, tray behavior, file dialogs, notifications, and local server startup. No domain or storage package imports desktop framework APIs.

## Backend package boundaries

```text
transport/http --> application services --> domain
       |                   |                  |
       |                   +--> repositories-+
       |                   +--> job queue
       |                   +--> feed/sync/ai ports
       |
       +--> auth middleware and SSE

infrastructure/sqlite implements repositories
infrastructure/feed implements feed ports
infrastructure/sync implements sync ports
infrastructure/ai implements AI ports
```

Dependencies point inward. Domain types do not import HTTP, SQLite, Wails, feed parser, or AI SDK packages.

## Data ownership

SQLite is authoritative. WAL mode supports concurrent readers and a bounded writer workload. Foreign keys are enabled. All schema changes use ordered migrations stored in the binary. Failed migrations abort startup and preserve the previous schema transactionally.

FTS5 indexes normalized title, author, summary, and sanitized plain text. Large raw and sanitized bodies live outside the timeline row to keep list queries compact.

## Fetch pipeline

```text
schedule/manual request
  -> persistent refresh job
  -> bounded worker pool
  -> URL policy and SSRF validation
  -> conditional HTTP request
  -> response limits and charset decode
  -> format parse
  -> normalize content and identifiers
  -> deduplicate by GUID, tracking-free canonical URL, content hash, and stable identity hash
  -> transactionally upsert feed and entries
  -> emit job and entry events
```

Feed errors are classified as temporary, permanent, authentication, rate-limit, parse, or policy errors. Retry behavior depends on the class.

RSS is pull-based by default: the scheduler polls due feeds every minute, while each feed defaults to a 30-minute next check. A subscription can select a fixed interval or disable automatic refresh; a manual refresh always queues an immediate conditional request. ETag and Last-Modified validators avoid downloading unchanged bodies, and failures use exponential backoff. Push delivery such as WebSub can be added later for providers that advertise it, but it cannot be assumed for arbitrary RSSHub routes.

## API rules

- All product endpoints are under `/api/v1`.
- IDs are opaque strings in the wire format.
- Pagination uses a stable time-and-ID cursor.
- PATCH endpoints support explicit nullable fields.
- Errors use a stable code, message, request ID, and optional field details.
- Long work returns a job resource rather than holding the request open.
- SSE resumes with `Last-Event-ID` when possible.

## Authentication modes

Loopback mode accepts a short-lived bootstrap secret injected into the desktop webview. LAN mode requires a paired device token and should use HTTPS for installable PWA clients. The server can terminate TLS directly with `REFLOW_TLS_CERT_PATH` and `REFLOW_TLS_KEY_PATH`, or sit behind a trusted reverse proxy. Pairing codes are one-time, expire quickly, and are only displayed on an already trusted client. Device tokens are random, shown once, stored hashed on the server, and revocable.

## Content safety

Feed and webpage content is untrusted. The backend applies a maintained HTML allow-list sanitizer, rewrites links and media URLs, removes active content, and stores sanitized and plain-text forms. The client renders sanitized content inside a constrained reader surface with a restrictive Content Security Policy.

Fetcher policy blocks loopback, link-local, private, multicast, metadata-service, and unsupported protocol targets unless a local feed is explicitly approved. Redirect targets are validated at every hop.

## Cross-device consistency

All durable entry state is server-side. Every state mutation includes a client mutation ID and device timestamp. The server applies idempotency and records the authoritative server timestamp. Offline clients replay their outbox in order. For boolean state, the latest accepted server mutation wins; destructive operations require online confirmation.

## External synchronization

FreshRSS, Google Reader compatible services, Miniflux, Fever, Feedbin, and Nextcloud News implement one adapter contract. Each account owns an opaque incremental cursor plus feed and entry mappings. On the first run ReFlow imports remote subscriptions and remote state; later runs push locally changed read/starred state before pulling remote deltas. A local change after the previous successful cursor wins over stale remote state.

WebDAV and iCloud Drive accounts use a separate portable library-snapshot path while sharing the encrypted account store and scheduler. The snapshot excludes device tokens, provider credentials, jobs, and local sync metadata. Each target stores independent local and remote fingerprints, so WebDAV and iCloud can run together. One-sided changes synchronize automatically; two-sided changes stop with a conflict until the user explicitly uploads the local library or restores the cloud copy.

Credentials are encrypted at rest with an installation master key stored outside SQLite. REST responses, logs, jobs, and events never contain credentials. Sync endpoints use the same SSRF policy as feed fetching; private network endpoints require an explicit per-account opt-in. Sync jobs and feed refresh jobs share the persistent queue but have independent handlers, so a remote adapter failure cannot block local feed refresh.

## AI assistance

OpenAI-compatible and Ollama providers implement one completion contract. Provider profiles contain an endpoint, model, encrypted API key, bounded generation settings, network policy, and explicit remote-content approval. Loopback providers do not require transmission approval; every non-loopback endpoint does. Provider redirects and resolved addresses pass through the same URL policy used by feed and sync requests.

Title translation, summary, full translation, key-point extraction, and article chat run in the persistent job queue. Title translation sends only the article title. Other jobs contain only opaque profile, entry, operation, language, and content-hash identifiers; API keys and article bodies never enter job payloads or events. A stable hash of the profile, model, settings, operation, language, and bounded article content prevents unchanged work from being submitted twice. Results and token usage are committed atomically.

The AI prompt boundary is read-only. Article content is wrapped as untrusted quoted material, provider requests expose no tools or callbacks, and AI output has no path to subscription or content mutation services. Cancelling a running job cancels its provider request context.

## Observability

Logs are structured and redact credentials, cookies, authorization headers, feed passwords, and article bodies. Health output separates liveness from readiness. Jobs record attempts and summarized errors. Debug logging is opt-in.
