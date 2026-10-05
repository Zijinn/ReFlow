# ReFlow command line client (`reflow`)

`reflow` is a JSON-first client for ReFlow Server's own HTTP API, shaped so an AI
agent (or a script) can work the 科研工作台 from outside the app: create a 在投 paper,
build the label palette, edit any field, and turn RSS subscriptions into tracked
literature.

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
(or `<group> <verb> --help`) is the human slice of one command.

## Commands

| Command | Endpoint | Notes |
| --- | --- | --- |
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
| `feed list` / `feed refresh <id>` | `GET /feeds`, `POST /feeds/{id}/refresh` | refresh queues a fetch job |
| `literature scan …` | reads feeds, papers, entries | writes only with `--import` |
| `ai profiles` / `ai fill --raw …` | `GET /ai/profiles`, `POST /ai/metadata-fill` | `--raw -` reads a pasted reference from stdin |
| `status`, `device pairing-code`, `device pair` | `/status`, `/devices/…` | pairing |

## `--set` fields

`paper create` and `paper set` take repeatable `field=value` assignments over the 29
PATCH fields (`reflow describe` prints them with their shapes):

```sh
reflow paper create --kind submitted --title "董事会规模与勤勉度" \
  --author 张三 --author 李四 \
  --set target_journal='金融研究' --set submission_date=2026-10-01 \
  --set 'tag_ids=t-1,t-2' --set next_action='回复审稿意见'
```

- Lists accept `a,b` or a JSON array; `stages` and `history` accept JSON verbatim.
- `citations=<int>` writes a count and `citations=null` clears it. The distinction
  matters: the server reads an absent `citations` as "leave alone" and an explicit
  JSON `null` as "clear", so the CLI sends the key rather than dropping it.
- A later `--set` for the same field wins.
- **An unknown field name is refused locally with exit 2 and no request.** The PATCH
  handler decodes into a struct of pointers and Go discards keys it does not
  recognise, so a misspelled field would otherwise answer 200 while writing nothing —
  the worst possible outcome for a caller that reads a status code as proof.
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

The CLI covers the workbench, the timeline read path and literature import. Entry
state writes, annotation editing, OPML, backup/restore, sync accounts, AI chat and
daily digest are REST-only; the SSE stream at `GET /api/v1/events` is not modelled
either, so a long-running agent polls rather than subscribes.
