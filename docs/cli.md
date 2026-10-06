# ReFlow command line client (`reflow`)

`reflow` is a JSON-first client for ReFlow Server's own HTTP API, shaped so an AI
agent (or a script) can work the 科研工作台 from outside the app: create a 在投 paper,
build the label palette, edit any field, manage the RSS subscriptions themselves — add one,
file it, retune how it refreshes, move a whole list in and out as OPML — and turn what
those subscriptions pull in into tracked literature.

It talks to a running server instead of opening the SQLite file. That is deliberate:
the server owns the WAL connection, the job queue, and the encrypted AI credentials,
and a second writer would contend with all three. Going over HTTP also means the CLI
inherits the server's validation and its auth rules rather than duplicating them.

The REST contract stays the source of truth for anything the CLI does not cover; see
[../api/openapi.yaml](../api/openapi.yaml).

## Build and point it at a server

```sh
make cli                     # -> bin/reflow (Go only, no web bundle)
reflow status                # defaults to http://127.0.0.1:7381
REFLOW_URL=http://192.168.1.20:7381 REFLOW_TOKEN=… reflow paper list --kind submitted
```

To type `reflow` rather than a path, put the binary on PATH — `go install ./cmd/reflow`
drops it in `$GOPATH/bin`, or copy it into a directory already on PATH:

```sh
go build -o ~/.local/bin/reflow ./cmd/reflow
```

`--url` and `--token` override the two environment variables. Options may be written
before or after the command path, and an id that begins with a dash goes after `--`.

Whatever answers on that address is the server: the standalone `reflow-server`, or the
desktop app, which serves the same API over `REFLOW_ADDR` (default `127.0.0.1:7381`) so
`reflow` can drive the library the app has open. If another process already holds the
address the app logs `API listener stopped` and carries on, so a dev server keeps its
port. Whichever process answers, it owns the SQLite file and the fetch scheduler, so
run one, not both.

The subscription commands in particular are meant to be used that way: `feed add`,
`folder set` and `opml import` against `REFLOW_URL=http://127.0.0.1:7381` write into the
library the user is looking at. The app is subscribed to the server's event stream and
refetches when a write publishes `subscriptions.updated`, so a subscription added from a
terminal shows up in its sidebar without a reload — the CLI never subscribes itself, it
just benefits from whichever process does.

## Output contract

stdout is exactly one JSON document per run, for failures as well as successes, so a
caller never has to guess whether to parse:

```json
{"ok":true,"data":{"id":"p-1","kind":"submitted","title":"…"}}
{"ok":false,"error":{"class":"api","message":"PATCH /api/v1/research/papers/p-1: 400 invalid_request: field notes exceeds 2000 characters","code":"invalid_request","status":400,"path":"/api/v1/research/papers/p-1","request_id":"…"}}
```

`--format pretty` indents the same shape. Help text is the one exception: `--help`
prints prose on stdout and the root command list on stderr.

Branch on the exit code, not on the message:

| Exit | Class | Meaning | What to do |
| --- | --- | --- | --- |
| 0 | ok | the document in `data` is the result | — |
| 2 | `usage` | bad flags, an unknown `--set` field, or a delete without `--yes` | fix the argv; nothing was written |
| 3 | `api` | the server answered 4xx/5xx | read `error.code`, `error.status`, `error.detail` |
| 4 | `transport` | no server answer: unreachable, timed out, or not JSON | check `--url` and that the server is running |

Two failures leave a row behind, and the envelope says so instead of going silent:

- `paper create` whose follow-up PATCH is rejected answers exit 3 with
  `data.paper_id`, `data.created` and `data.fields_undone`. The paper exists; the
  listed fields did not land.
- `literature scan --import` answers exit 0 with one `imported[]` entry per attempt,
  including `status:"failed"`, `failed_at:"create"|"patch"` and the server's `code`.

## Auth

A call to `127.0.0.1` is the machine's own traffic, so it needs no token — that is why
`make dev-server` plus `reflow status` works with no setup. `GET /api/v1/status`
and `POST /api/v1/devices/pair` are the two endpoints that answer before pairing.

Off-machine calls only exist when the server runs with `REFLOW_LAN_MODE=true`, and
they carry a paired device token:

```sh
reflow device pairing-code                    # run this on the host
reflow device pair --code 123456 --name "workdesk agent"   # prints the token
export REFLOW_TOKEN=…                             # or pass --token
```

The token is printed on stdout, so treat that output as a secret. A 401 comes back as
exit 3 with `error.hint` naming these two commands.

## Discovery

`reflow describe` prints the whole catalog — every command, flag, type, choice list
and endpoint, plus the paper field table, the tag scopes and the exit codes —
generated from the same registry that parses argv, so it cannot drift from the
binary. `reflow help` is the same table as prose, and `reflow help <group> <verb>`
(or `<group> <verb> --help`) is the human slice of one command. The subscription and
folder field tables are not printed as separate catalogs the way the paper one is; they
ride inside `feed set` and `folder set`, in the description of their `--set` flag, which
is the cheapest place to read the exact accepted shapes.

## Commands

The whole surface: the 37 commands `reflow describe` lists, with the endpoint each one
calls. A row that names several verbs groups commands that share one flag set.

| Command | Endpoint | Notes |
| --- | --- | --- |
| `describe` | no request | the catalog as JSON; see Discovery above |
| `paper list --kind <bucket>` | `GET /research/papers` | buckets are `research` 在研, `submitted` 在投, `published` 已发表 |
| `paper get <id>` | `GET /research/papers/{id}` | stage tree, labels, submission history |
| `paper create --kind --title [--author]… [--set f=v]…` | `POST`, then `PATCH` | the create route only accepts kind/title/authors, so `--set` is a follow-up PATCH |
| `paper set <id> --set f=v…` | `PATCH /research/papers/{id}` | partial update |
| `paper move <id> --kind` | `POST …/move` | how 在投 becomes 已发表 |
| `paper delete <id>` | `DELETE …/{id}` | needs `--yes` |
| `paper citation <id>` | `POST …/citation` | Crossref fills journal, year, volume, pages, citations |
| `paper reorder --kind --paper-id…` | `POST /research/papers/reorder` | writes the row order |
| `tag list/create/set/delete/reorder [--scope research\|reader]` | `/research/tags` or `/tags` | see Tag scopes |
| `entry list [--kind --state --query --feed --folder --tag-id --since --days --limit --cursor]` | `GET /entries` | one timeline page |
| `entry get <id> [--ai-language]` | `GET /entries/{id}` | carries `doi`, `author`, `canonical_url` |
| `feed list` | `GET /feeds`, `GET /subscriptions` | every feed with its subscription row attached as `subscription` |
| `feed get <id>` | `GET /feeds/{id}`, `GET /subscriptions` | one feed row, same `subscription` attachment |
| `feed add <feed-url> [--folder --title]` | `POST /feeds`, then `GET /subscriptions` | fetches and parses the address before it answers; see Subscriptions and folders |
| `feed discover <page-url>` | `POST /feeds/discover` | the feed URLs one page publishes |
| `feed set <id> --set f=v…` | `PATCH /feeds/{id}`, then `GET /feeds/{id}` | the 7 writable subscription fields, `url` not among them |
| `feed delete <id>` | `DELETE /feeds/{id}` | needs `--yes`, and the feed's articles leave with it |
| `feed refresh <id>` | `POST /feeds/{id}/refresh` | 202 with a job; 409 `refresh_pending` if one is already queued |
| `folder list` | `GET /folders` | the ids `--folder` and `folder_id=` expect |
| `folder add <name> [--parent]` | `POST /folders` | an ensure: same name under the same parent answers the same id |
| `folder set <id> --set f=v…` | `PATCH /folders/{id}` | `name`, `parent_id`, `position`; nesting under your own descendant answers 409 `folder_cycle` |
| `folder delete <id>` | `DELETE /folders/{id}` | needs `--yes`; subscriptions inside stay, unfiled |
| `opml import --file <path\|->` | `POST /imports/opml` | the document is the body itself; 202 with a job |
| `opml export [--out <path>]` | `GET /exports/opml` | `--out` answers `{path,bytes}`, no `--out` answers `{document}` |
| `literature scan …` | reads feeds, papers, entries | writes only with `--import` |
| `ai profiles` / `ai fill --raw …` | `GET /ai/profiles`, `POST /ai/metadata-fill` | `--raw -` reads a pasted reference from stdin |
| `status` | `GET /status` | whether the server is up and what it accepts from this caller |
| `job get <id>` | `GET /jobs/{id}` | how a caller waits for a refresh or an import |
| `job cancel <id>` | `POST /jobs/{id}/cancel` | no `--yes`: it destroys no stored row |
| `device pairing-code` / `device pair --code --name` | `POST /devices/…` | pairing |

## `--set` fields

`paper create`, `paper set`, `feed set` and `folder set` take repeatable `field=value`
assignments, each against its own field table: 29 fields for a paper, 7 for a
subscription, 3 for a folder. `reflow describe` prints every shape, and a command's
`--help` prints only the table its own command accepts:

```sh
reflow paper create --kind submitted --title "董事会规模与勤勉度" \
  --author 张三 --author 李四 \
  --set target_journal='金融研究' --set submission_date=2026-10-01 \
  --set 'tag_ids=t-1,t-2' --set next_action='回复审稿意见'
```

- Lists accept `a,b` or a JSON array; `stages` and `history` accept JSON verbatim.
- `citations=<int>` writes a count and `citations=null` clears it. The distinction
  matters: the server reads an absent `citations` as "leave alone" and an explicit
  JSON `null` as "clear", so the CLI sends the key rather than dropping it. The two RSS
  tables have three columns with the same reading — `folder_id=null` unfiles a
  subscription, `title_override=null` hands the displayed name back to the feed, and
  `parent_id=null` makes a folder top-level — and each goes out as JSON null, never as
  the four-letter string.
- A later `--set` for the same field wins.
- **An unknown field name is refused locally with exit 2 and no request.** The PATCH
  handler decodes into a struct of pointers and Go discards keys it does not
  recognise, so a misspelled field would otherwise answer 200 while writing nothing —
  the worst possible outcome for a caller that reads a status code as proof. The check
  runs against the table for the command you used, and the refusal names that group, so
  a paper field does not pass for a subscription one.
- Length limits are the server's: 500 characters for most text fields, 2000 for
  `notes` and `abstract`, and at most 12 labels per paper. They come back as exit 3.

## Tag scopes

There are two label namespaces, stored apart, and `--scope` picks one. The default is
`research`.

- `research` is the 工作台 palette: position is the priority order, colour comes from
  the eight named tints the badges ship (`amber blue gray green orange red teal violet`),
  and `color_enabled` is the "color the head of my rows" switch — off keeps the
  chip's colour and only hands the row head to the next label. Full CRUD plus
  `tag reorder`.
- `reader` labels RSS entries. Only create and delete exist, so `tag set --scope
  reader` and `tag reorder --scope reader` are refused with exit 2; delete and
  recreate instead. Its colour column is nullable, so the CLI omits `color` rather
  than storing an empty string.

Papers reference labels by id, so a rename never touches the papers wearing it.

Each verb answers one shape whatever scope it read: `tag list` is
`{"scope":…,"items":[…]}` and `tag create` / `tag set` are `{"scope":…,"tag":{…}}`. The
server's own routes differ (`tags` vs `items`, a wrapped object vs a bare one), and a
caller that reads the wrong key concludes the palette is empty and creates a duplicate.

## Subscriptions and folders

Two rows stand behind one RSS source, and this group splits along that seam. The **feed**
row is the document: `url`, `canonical_url`, `site_url`, the feed's own `title`, `format`,
`content_kind`, and the fetch bookkeeping (`etag`, `last_checked_at`, `last_success_at`,
`failure_count`, `last_error_code`). The **subscription** row is what this profile wants
done with it: `folder_id`, `title_override`, `view_mode`, `refresh_policy`,
`refresh_interval_minutes`, `hide_from_timeline`, `position`.

So the CLI joins them: every `feed` command answers the feed row with its subscription row
attached under `subscription`, and `id` at the top level stays the **feed** id — the handle
`feed get`, `feed set`, `feed delete` and `feed refresh` all expect. A feed with no
subscription row answers `"subscription": null` rather than dropping the key, and the join
costs one extra request for the whole list, not one per feed.

```sh
reflow feed list --format json | jq '.data.items[] | {id, folder: .subscription.folder_id,
  display: .subscription.title, unread: .subscription.unread_count}'
```

`subscription.title` is the display name — the override if one is set, the feed's own title
if not — so it is what the app's sidebar shows, while the top-level `title` stays what the
publisher sent. `feed set` re-reads the feed row after the write so its answer has the same
shape as `feed get`; the settings it just wrote are the attached `subscription`.

**`feed add` fetches before it answers.** The server downloads the address, parses it,
upserts both rows, imports the entries and runs the library's rules over them, and only
then answers 201. A dead host or a document that is not a feed therefore fails here, with
exit 3, rather than surfacing a week later as a feed with nothing in it. Two consequences
for the caller:

- The fetch is bounded server-side at 300s while the CLI bounds every request at the
  default `--timeout 30s`, so a slow-but-valid address is the one case where a
  subscription command can time out on this side. Exit 4 then says *no answer arrived*, not
  *nothing happened* — raise `--timeout 5m` for an address you expect to be slow, and if
  you did hit the default, re-check with `feed list` before retrying, because the feed may
  well have been saved.
- Retrying costs a fetch, not a duplicate row: `feeds.canonical_url` and
  `subscriptions(profile_id, feed_id)` are both unique and both upsert, so the same address
  lands on the same feed id. Confirming with `feed list` first is what saves you the second
  wait.

`rsshub://…` is accepted exactly as it is in the app's add dialog. The server rewrites it
against its own `REFLOW_RSSHUB_BASE` (default `https://rsshub.app`) before fetching, so the
caller supplies a route and the server's configuration decides which instance serves it.
`feed discover` sends `{url}` and reads back `{"items":[{url,title,site_url}]}`, which is
how you turn a site's homepage into something to subscribe to; it fetches server-side too,
so it shares the timeout caveat above.

**`feed set` writes the subscription row, seven fields and no more:**

- `folder_id=<text|null>` — files the subscription in a folder from `folder list`; `null`
  takes it out of one.
- `title_override=<text|null>` — the name shown in place of the feed's own title; `null`
  hands the naming back to the feed.
- `view_mode=<text>` — `compact` 紧凑, `standard` 标准, `card` 卡片, `magazine` 杂志,
  `image` 图片.
- `refresh_policy=<text>` — `inherit`, `fixed`, `intelligent`, `never`. `never` pushes the
  next check a year out, which is how a subscription comes off the scheduler without
  unsubscribing it.
- `refresh_interval_minutes=<int>` — 0 to 10080, consulted only under `fixed`. Writing
  either of these two reschedules the feed right then, so the next fetch follows the new
  policy instead of the old `next_check_at`.
- `hide_from_timeline=<true|false>` — takes the feed's entries out of the timeline query.
  The filter is unconditional, so `entry list` will not show them even with `--feed <id>`,
  and `literature scan` — which reads through that same list — stops seeing the feed as
  well; only `entry get` on an id you already hold still answers.
- `position=<int>` — a non-negative sidebar order.

A name outside that table refuses at exit 2 before a request leaves, for the same reason
papers do (see `--set` fields): the handler decodes into a struct of pointers, Go drops the
keys it does not know, and the endpoint would answer 200 while writing nothing. The two
enums are the server's check, not the CLI's, so a wrong `view_mode` is exit 3 carrying the
list the handler wanted rather than a second copy of that list kept here where it can drift.

`url` is deliberately not writable. The server never repoints a subscription at another
address, so a feed whose address moved is `feed add` of the new one plus `feed delete` of
the old — and because the delete below takes that feed's articles with it, changing an
address is not a cheap rename: it costs whatever the old one had collected.

**`feed delete` is the one command whose blast radius is wider than the row it names.** It
removes the subscription, drops the feed row once nothing is subscribed to it — with the
app's single default profile, always — and `entries.feed_id`, `entry_states` and
`feed_tags` all `ON DELETE CASCADE` from that row. The articles the feed pulled in go with
it, and so do their read and starred states. No API keeps them. When the goal is a quieter
library rather than an erased one, `refresh_policy=never` or `hide_from_timeline=true` is
the edit you meant; `--yes` on this command should be a decision, not a reflex.

**Folders are drawers, not containers.** `folder add` is an ensure rather than a blind
insert: `POST /folders` looks for that name under that parent and answers with the folder it
found or the one it made, so the same call twice returns the same id and `reflow folder add
文献` belongs in a setup script. `folder set --set parent_id=<id>` moves a folder, and
moving one under its own descendant answers 409 `folder_cycle` at exit 3 — the nesting is
stored as a `parent_id` chain, and a cycle would leave it with no root to render from.

`folder delete` does not touch the subscriptions inside it: `subscriptions.folder_id` is
`ON DELETE SET NULL`, so they survive and simply become unfiled. Sub-folders are the
exception worth knowing before you confirm — `folders.parent_id` is `ON DELETE CASCADE`, so
deleting a parent deletes its children too, and their subscriptions go unfiled in turn.
Delete bottom-up when you mean to lose only one drawer.

## OPML and the job queue

**The document travels as itself.** `opml import --file subs.opml` puts the file's bytes in
the request body with content type `text/xml` instead of wrapping an XML string inside a
JSON object: the server parses the bytes it was handed, and the endpoint carries its own
16 MiB ceiling rather than the small-JSON limit every other write shares, so a real export
from another reader fits — `request_body_too_large` at exit 3 is the answer when it does
not. `--file -` reads the document off a pipe the way `ai fill --raw -` does, for a caller
that has the OPML on stdin rather than on disk. A path that does not resolve is the
caller's typo: exit 2, no request.

**The answer is a job, not a count.** The endpoint only checks that the document parses,
then puts it on the queue and answers 202 with the job, so `data.id` is a job id and the
subscriptions appear over the next while. `feed refresh` answers the same way, which is what
`job get` is for:

```sh
job=$(reflow opml import --file subs.opml | jq -r .data.id)
reflow job get "$job"        # state, progress_current / progress_total
```

A job's `state` is `queued`, `running`, `succeeded`, `failed` or `cancelled`, and a failure
carries `error_code` and `error_message` — which is where an import that died on one bad
outline explains itself. The CLI does not subscribe to the event stream, so polling `job get`
is how a caller waits.

`job cancel` asks for a job that is still `queued` or `running`; one that already finished
answers 409 `job_not_cancellable` at exit 3, because whether a job may still be stopped is
the server's call about a row it owns, not something the CLI can know from argv. That is
also why this command is not gated behind `--yes`: it stops work and destroys no stored
row, whereas the four commands that are gated — `paper delete`, `tag delete`, `feed delete`,
`folder delete` — all remove rows an agent cannot put back.

**`opml export` answers XML, which is not the envelope.** `GET /exports/opml` serves
`text/xml`, so the CLI has to decide where the document goes rather than print it as if it
were JSON. With `--out <path>` it writes the file and reports `{"path":…,"bytes":…}` — stdout
stays one small document and never carries the whole library — and without `--out` the
document comes back inline as `{"document":"<?xml …"}` for a caller that wants to read the
list without touching the disk. An empty answer is a transport failure (exit 4), not a
successful export of nothing.

## Literature identification

`literature scan` reads the subscriptions and reports which entries look like papers,
matched against what the workspace already tracks:

```sh
reflow literature scan --days 14
reflow literature scan --feed f-1 --min-confidence high --hide-tracked
reflow literature scan --days 7 --import --target-kind published --tag-id t-1
```

Classification is deterministic and every candidate carries the reasons it was
accepted, because the caller is expected to be an AI agent that can read evidence and
override the tier — not a score it has to trust:

| Confidence | Evidence | Why |
| --- | --- | --- |
| `high` | `doi` | the entry has a DOI after stripping a `doi:` label or a resolver URL |
| `medium` | `feed_is_literature`, `authors` | the server classified the feed as scholarly by URL and DOI density, and the entry names authors |
| `low` | `feed_is_literature` or `title_only` | scholarly feed without authors, or a feed that is not scholarly at all |

A `10.xxxx/…` placeholder — what an unregistered feed puts in every item — is dropped
rather than counted as a DOI.

Dedupe runs against all three buckets: first by normalised DOI, then by a title with
case and punctuation collapsed, so "Does \"Board\" Gender, Change Pay?" matches the
row already in 在研. A matched candidate reports `tracked{paper_id,kind,match}` and is
never re-imported.

Writing is opt-in. Without `--import` the scan issues only GETs. With it, each
candidate above `--min-confidence` (default `medium`) becomes a `POST` plus a `PATCH`
that writes the normalised DOI, any `--tag-id` labels, and a 备注 line naming the
source entry and URL — a bulk import has to say where its titles came from. One
rejection does not abort the run.

`--target-kind` defaults to `published` (已发表). Feed literature is someone else's
finished paper, and that bucket is the one carrying DOI, journal, year, volume, pages
and the citation count. `submitted` (在投) is for the user's own manuscripts;
importing found papers there would fill the submission board with work the user is not
writing.

Several publisher feeds (Elsevier's `rss_sd_all` among them) put no authors and no DOI
in the item, so every entry from them lands at `low` with `feed_is_literature` as its
only evidence and the default `--min-confidence medium` imports nothing. That is the
feed's shape, not a failed lookup — read one with `reflow entry get <entry-id>` and the
`author` column is null. To pull those in anyway, lower the floor and let the server
backfill what the RSS omitted:

```sh
reflow literature scan --days 7 --min-confidence low --import --tag-id t-1
reflow paper citation <paper-id>   # Crossref fills journal, year, volume, pages
reflow ai fill --raw -             # or paste the reference line from the entry
```

## What is not here

The CLI covers the workbench, the whole subscription surface — feeds, folders, OPML in and
out, and the jobs those writes queue — the timeline read path and literature import. Entry
state writes, annotation editing, backup/restore, sync accounts, AI chat and daily digest
are REST-only. The SSE stream at `GET /api/v1/events` is not modelled either, so a
long-running agent polls rather than subscribes, which is what `job get` exists for.
