# ReFlow logical data model

## Identity and devices

### profiles

One default profile exists in the initial release. Keeping a profile boundary avoids embedding a global singleton into every future API.

- `id`
- `display_name`
- `created_at`
- `updated_at`

### devices

- `id`
- `profile_id`
- `name`
- `platform`
- `token_hash`
- `last_seen_at`
- `created_at`
- `revoked_at`

## Sources and subscriptions

### feeds

Represents a fetched source independent of personal organization.

- `id`
- `url`
- `canonical_url`
- `site_url`
- `title`
- `description`
- `icon_url`
- `format`
- `etag`
- `last_modified`
- `last_checked_at`
- `last_success_at`
- `next_check_at`
- `failure_count`
- `last_error_code`
- `last_error_message`
- `created_at`
- `updated_at`

`canonical_url` is unique after normalization.

### subscriptions

Represents the profile's relationship to a feed.

- `id`
- `profile_id`
- `feed_id`
- `folder_id`
- `title_override`
- `view_mode`
- `refresh_policy`
- `refresh_interval_minutes`
- `hide_from_timeline`
- `created_at`
- `updated_at`

The pair `(profile_id, feed_id)` is unique.

### folders

- `id`
- `profile_id`
- `parent_id`
- `name`
- `position`
- `created_at`
- `updated_at`

Folder cycles are rejected by the application service.

## Entries

### entries

Contains compact fields needed for timeline queries.

- `id`
- `feed_id`
- `guid`
- `canonical_url`
- `title`
- `author`
- `summary`
- `published_at`
- `discovered_at`
- `content_hash`
- `identity_hash`
- `lead_image_url`
- `audio_url`
- `video_url`
- `language`
- `created_at`
- `updated_at`

Deduplication keys are evaluated in this order:

1. `(feed_id, guid)` when GUID is present;
2. `(feed_id, canonical_url)` when the canonical URL is present;
3. `(feed_id, content_hash)` for exact content matches;
4. `(feed_id, identity_hash)` for feeds whose items do not expose a stable GUID or URL.

URLs are normalized before comparison. Known campaign parameters such as `utm_*`, `fbclid`, and `gclid` are removed, while functional feed parameters are preserved. The identity hash is derived from normalized title, author, publication time, and a short body prefix, so a later edit at the end of an article does not create a second article without making same-day articles with identical titles indistinguishable.

### entry_contents

- `entry_id`
- `source_html`
- `sanitized_html`
- `plain_text`
- `readability_html`
- `readability_fetched_at`
- `updated_at`

### entry_states

- `profile_id`
- `entry_id`
- `is_read`
- `is_starred`
- `is_read_later`
- `read_at`
- `updated_at`
- `updated_by_device_id`

The pair `(profile_id, entry_id)` is the primary key.

### tags, feed_tags, entry_tags

Tags belong to a profile. Join tables use composite primary keys and cascade on deletion.

## Jobs and events

### jobs

- `id`
- `kind`
- `state`
- `payload_json`
- `progress_current`
- `progress_total`
- `scheduled_at`
- `started_at`
- `finished_at`
- `error_code`
- `error_message`
- `created_at`
- `updated_at`

### job_attempts

- `id`
- `job_id`
- `attempt`
- `started_at`
- `finished_at`
- `result`
- `error_code`
- `error_message`

## Rules

### rules

- `id`
- `profile_id`
- `name`
- `enabled`
- `priority`
- `conditions_json`
- `actions_json`
- `created_at`
- `updated_at`

Rules are evaluated deterministically by priority and ID.

## Synchronization

### sync_accounts

- `id`
- `profile_id`
- `provider`
- `name`
- `endpoint`
- `encrypted_credentials`
- `cursor_json`
- `enabled`
- `allow_private_network`
- `sync_interval_minutes`
- `last_sync_at`
- `next_sync_at`
- `last_attempt_at`
- `last_error_code`
- `last_error_message`
- `created_at`
- `updated_at`

Credentials are encrypted with AES-256-GCM. The account ID is authenticated as associated data, so an encrypted value cannot be moved to another account. The master key is stored separately from SQLite with owner-only permissions.

### sync_mappings

- `account_id`
- `local_kind`
- `local_id`
- `remote_id`
- `updated_at`

Feed and entry mappings are unique in both local and remote directions per account. They make retries idempotent and allow local state changes to be pushed without re-matching every article.

For WebDAV and iCloud Drive accounts, `cursor_json` stores the last local and remote portable-snapshot fingerprints instead of a service cursor. These accounts can be enabled concurrently and never include provider credentials or device tokens in the synchronized snapshot.

## AI

### ai_profiles

- `id`
- `profile_id`
- `provider`
- `name`
- `endpoint`
- `model`
- `encrypted_api_key`
- `settings_json`
- `enabled`
- `allow_private_network`
- `remote_content_approved`
- `is_default`
- `last_used_at`
- `last_error_code`
- `last_error_message`
- `created_at`
- `updated_at`

The API key is encrypted with AES-256-GCM and the AI profile ID as associated data. Remote article transmission is rejected unless `remote_content_approved` is true. Private and loopback destinations additionally require `allow_private_network`; the Ollama form enables that setting by default for its loopback endpoint.

### ai_results

- `id`
- `profile_id`
- `entry_id`
- `operation`
- `language`
- `input_hash`
- `result_text`
- `usage_json`
- `created_at`

The tuple `(profile_id, entry_id, operation, language, input_hash)` is unique.

### ai_chat_sessions and ai_chat_messages

Sessions are optionally attached to an entry and an AI profile. Messages record role, content, provider metadata, job ID, status, usage, and timestamps. One assistant message is allowed per job so retrying a recovered job remains idempotent.

### ai_usage

- `id`
- `profile_id`
- `ai_profile_id`
- `entry_id`
- `job_id`
- `operation`
- `provider`
- `model`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `created_at`

Usage rows are committed in the same transaction as AI results or assistant messages. Prompts and article content are never copied into this table.

## Search

An FTS5 virtual table indexes `entry_id`, title, author, summary, and sanitized plain text. Triggers or explicit repository updates keep it consistent with `entries` and `entry_contents`. Search results join through `entry_states` so profile filters remain authoritative.
