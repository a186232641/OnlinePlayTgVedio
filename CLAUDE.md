# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

A self-hosted web app for browsing, searching, favoriting, and streaming the videos and images
from the Telegram channels and groups a user has joined (including forum groups, whose media is
browsed per topic). Go backend (gotd/td MTProto client) + React SPA + Postgres.
The README.md is comprehensive (in Chinese) — read it for deployment and env-var detail. This
file focuses on the things that require reading multiple files to understand.

## Commands

Backend (Go 1.25+, module `github.com/hanfeilong/onlineplaytgvideo`):
```bash
make build        # go build -> bin/server
make run          # go run ./cmd/server   (needs env loaded — see below)
make test         # go test ./...
make vet          # go vet ./...
go test ./internal/video/ -run TestParseRange   # run a single test
```
The Makefile auto-`include`s `.env` and exports it, so `make run`/`make dev-server` see
`TG_API_ID`, `MASTER_KEY`, `DB_DSN`, etc. Running the binary directly requires exporting those
yourself. `make test` is pure-unit (no DB/network) and covers the fiddly math: `video/stream_test.go`
(Range parsing + chunk alignment), `db/videos_test.go` (keyset cursor + ORDER BY SQL),
`cache/cache_test.go` (disk scan), `config/config_test.go` (`TG_DC_OVERRIDES` parsing).
`db/integration_test.go` is the one exception and **skips itself unless `TEST_DB_DSN` is set**
(it wipes every table, so point it at a throwaway database). It covers what unit tests can't:
the merged video+image pagination walked at several page sizes, forum-topic rows, the
`(kind, id)` cache keys, and favorites/pin bookkeeping across both tables:
```bash
createdb tgvtest
TEST_DB_DSN='postgres:///tgvtest?sslmode=disable' go test ./internal/db/ -run TestIntegration
```

Frontend (Node 20+, in `web/`):
```bash
cd web && npm install && npm run dev      # Vite dev server :5173, proxies /api -> :8080
npm run build                             # tsc + vite build -> web/dist
```

Local dev loop: `make dev-db-up` (Postgres in docker) → `make dev-server` (backend :8080) →
`make dev-web` (Vite :5173). Open http://localhost:5173.

Deployment: `make compose-up` builds images locally; `make prod-up` pulls pre-built GHCR images
(`.github/workflows/build-and-publish.yml` publishes server+web images on push to `main` and on
`v*.*.*` tags). Caddy fronts both in `deploy/`.

## Architecture

Request lifecycle is wired in `cmd/server/main.go`: it opens the DB, runs migrations,
constructs the `tgmanager` → `indexer` → `cache` → `video.StreamServer` → `tglogin` graph, then
mounts everything through `api.NewRouter`. Components are connected by callbacks, not a DI
framework — e.g. favorites trigger caching via `OnFavAdd`/`OnFavRemove` function pointers, and
TG login completion calls back into `tgMgr.Start` + `idx.TriggerDiscover`.

**Multi-account model.** A web user (Postgres `users`) may bind multiple TG accounts. Each bound
account is a `tg_session` row, and `tgmanager.Manager` owns one persistent `*telegram.Client` per
**session id** (not per user). `bg.Connect` keeps each client alive; a `floodwait` middleware
auto-backs-off on FLOOD_WAIT. `RestoreActive` rebuilds all clients on startup. Most code paths key
off `tg_session_id` — a channel knows which session can fetch it via `Channel.TGSessionID`.

**Per-DC connection pool + resilience middlewares.** Telegram files live on a specific data center;
issuing file reads through a client connected to a different DC forces repeated DC migration and IO
timeouts. `internal/dcpool` keeps a lazily-created connection pool per DC on top of the one
authenticated client, and callers fetch via `cli.APIForDC(v.DCID)` so reads go straight to the
file's DC (`videos.dc_id`, populated on locator resolve in `refresh.go`). Both the high-level client
and every per-DC pool invoker are wrapped with `internal/tgmw` middlewares: `NewRecovery`
(exponential-backoff reconnect on any non-business, non-cancel error) and `NewRetry` (bounded retry
on transient server errors like `Timedout`/`RPC_CALL_FAIL`, mirroring tdl's list). `TG_DC_OVERRIDES`
(env) pins DC IPs when Telegram rotates them — pool invokers don't inherit the high-level client's
middleware chain, so middlewares are passed into `dcpool.NewPool` explicitly.

**Session secrecy.** `tgsession.Storage` implements gotd's `session.Storage` but encrypts the
session blob with AES-256-GCM (`internal/tgsession/crypto.go`) before writing to Postgres. The key
is `MASTER_KEY` from env. Losing `MASTER_KEY` bricks every stored session → all users must re-bind.

**TG login** is a 3-step HTTP flow (`/api/tg/login/{start,code,password}`) orchestrated in
`internal/tglogin/flow.go` via a channel-driven `UserAuthenticator` — the HTTP handlers feed
phone/code/2FA-password into the in-flight gotd auth goroutine. SignUp is not supported.

**Forum groups and topics.** A forum supergroup keeps every message inside a *topic*, so the group
itself is never browsed directly. Discovery marks it `channels.is_forum` (its `dialog_kind` stays
`megagroup`, so every existing list filter keeps working) and `messages.getForumTopics` writes one
child row per topic: `dialog_kind='topic'`, `parent_channel_id`, `topic_id` — columns migration
0002 already created. **A topic row copies the parent's `tg_channel_id` and `access_hash`** on
purpose: a topic is a message thread, not a peer, so every path that builds an `InputPeer` or
re-fetches a message (`refresh.go`, the cache downloader) works on a topic row unchanged. Syncing a
forum group (`runForumSync`) re-enumerates its topics and then syncs them **sequentially** — they
share one TG session, and parallel history walks are the shortest path to a FLOOD_WAIT.

**The forum flag is re-checked on every sync run** (`refreshForumFlag`, one `channels.getChannels`
call). Discovery is the only other thing that writes it, so a row created before forum support —
or a group converted to a forum afterwards — would still say `false`, and walking a forum as a
plain channel is not a smaller version of the right thing, it's the wrong thing: `getHistory` on a
forum returns **every topic's messages flattened onto the group row**, so all the media piles into
one bucket and the topic structure is lost. When that has already happened the group's own
`video_count`/`photo_count` are non-zero while `is_forum` is true; the topic list surfaces those as
orphans with a view/clear action rather than silently hiding them.

Because a topic row *is* a channel row, `POST /channels/{topicID}/sync` syncs one topic on its own,
which is the normal way to use this: a group can hold dozens of topics with hundreds of thousands
of messages each, so the group-level "sync everything" is the batch option, not the only one. The
topic list endpoint attaches each topic's live `SyncState` to its row (`topicDTO`) so the page
polls one URL instead of one per topic.

**Photos live in their own table.** `photos` mirrors `videos` column for column (same TG-export
naming, same lazily-resolved locator idea) but the locator is a *photo* locator
(`tg.InputPhotoFileLocation`: photo id + access_hash + file_reference + a size letter), which is a
different id space from documents — hence a separate table, a separate `photo_favorites`, and
`kind` in `cache_entries`. `size_type`/`thumb_size` are the size letters picked at sync time from
`Photo.Sizes` (`internal/tgmedia`); videos get the same treatment from `Document.Thumbs` into
`videos.thumb_size`. Everything else — album caption propagation, keyset pagination, search,
favorites — is the videos implementation repeated.

**Merged media lists** (`internal/db/media.go`): a channel or topic view interleaves both tables by
date. `ListMedia` queries each table for its own top-N after **its own** cursor, merges, and
truncates back to N; the returned cursor points at the last row of each kind that made it into the
page, so the next call re-reads whatever was fetched but not returned. The global top-N is always a
subset of (top-N of videos ∪ top-N of photos), so this is exact — and it never compares ids across
tables, which would be meaningless (two independent BIGSERIALs). The API carries the two halves as
`offset_video` / `offset_photo` and answers with `has_more`: after a merge-and-truncate, a short
page no longer means "the end", so the frontend must not infer it.

**Sync cursors span both tables** (`MaxMsgIDForChannel` / `MinMsgIDForChannel`). A videos-only MAX
would make every incremental sync re-walk everything newer than the last *video*. Widening what
sync stores also means already-`history_complete` channels never see the older messages again —
`POST /channels/{id}/backfill` resets that flag (for a forum group, on each of its topics) and
re-runs sync. That is the migration path for images in channels synced before image support.

**Two ingest paths** populate `videos`/`photos`, both producing the same row shape:
- `internal/api/handlers/import.go` — upload a Telegram JSON export (`messages.json`).
- `internal/indexer/sync.go` — pull live history via `messages.getHistory`, or, for a topic, via
  `messages.search` with `top_msg_id` (`fetcherFor` picks; the filter is deliberately
  `InputMessagesFilterEmpty`, not `PhotoVideo`, because plenty of channels post videos as plain
  documents that the server-side filter would drop). **Streaming + resumable** (`walkHistory`):
  writes each batch to the DB as it pages, so a crash/timeout leaves
  partial progress instead of losing everything. Two passes, both driven by the stored cursor so a
  resume needs no extra bookkeeping: **Phase A incremental** = `MinID=MAX(tg_msg_id)` pulls messages
  newer than what we have; **Phase B backfill** = `OffsetID=MIN(tg_msg_id)` walks older history to
  the very bottom, then sets `channels.history_complete` so later sweeps skip the backfill. Write
  order doesn't matter — queries sort by `date`. A probe call first flags "stale access_hash / lost
  membership" (0 messages). One run is bounded by `SYNC_RUN_TIMEOUT` (env, default **24h**, `0`/
  `off` = unlimited) and each individual page by `pageTimeout` (2 min, retried) — the per-page
  bound is the real protection against a wedged connection, so the overall one can be generous.
  It was a hard-coded 30 minutes, which a million-message channel hit every single round.
  Hitting either limit is **not** an error: progress is already persisted, and `SyncState.Note`
  (informational) says so — distinct from `SyncState.LastError` (an actual failure), because the
  two used to share a field and a timeout was being shown to users as "已是最新?". **Writes are batched per page** (`pageWrite` / `flushPage`): a page's
  videos, its photos and its distinct album-caption propagations go out as three pgx batches — one
  network round trip each — instead of one (often two) per message. On a million-message channel
  that is the difference between hours and days; the round trips were never Telegram's fault. pgx
  runs a batch in an implicit transaction, so a page lands whole or not at all, which is what the
  resumable cursor wants. **Don't "simplify" this back to a per-message write.** Sync state is
  **in-memory only** (`Indexer.syncs` map), surfaced via
  `GET /channels/{id}/sync` with live `walked`/`imported`/`videos`/`photos`/`skipped`. A background scheduler
  (`indexer/scheduler.go`, started in `main.go`) re-runs sync every `SYNC_INTERVAL` (env, default
  30m; 0/off disables) for every channel that is `last_indexed_at IS NOT NULL AND auto_sync`
  (per-channel opt-in, default off; manual `SyncStart` ignores it), via the same idempotent
  `SyncStart`. `MarkChannelIndexed` recomputes `video_count` via `COUNT(*)` — never
  a per-run delta, or incremental syncs would clobber the total.

**Album grouping.** Telegram albums share a `grouped_id`; the caption lives on exactly one member.
`videos.grouped_id` is NULL for non-album rows (`nilIfZero64`), and after importing an album member
`sync.go` calls `PropagateGroupCaption` to copy the group's caption onto its silent siblings, so
search and list views aren't blind to album items.

**Streaming** (`internal/video/stream.go`): on each request, if a complete cached file exists for
`tg_doc_id` (`Cache.CompletePathFor`) **and its on-disk size matches `videos.file_size`**, it serves
straight off disk via `http.ServeFile` and returns. A size mismatch means a truncated/corrupt cache
(mobile decoders fail hard on these): it calls `Cache.InvalidateCorrupt` (delete file + mark the
entry incomplete, preserving `pinned`) and falls through to Telegram.
Otherwise `serveFromTelegram`: browser `Range: bytes=…` → backend aligns to 4KB boundaries and loops
`tg.Client.UploadGetFile(Precise=true)` (bounded parallel prefetch) in 1MiB chunks via the file's-DC
client (`cli.APIForDC`), trimming the alignment prefix on the first chunk and overflow on the last.
JSON-imported rows (`TGDocID=0`) resolve their locator on first play; on `FILE_REFERENCE_EXPIRED` it
lazily re-fetches via `channels.getMessages` (`refresh.go`), updates the DB, and retries once.
CDN-redirected files are **not** supported (returns 500). Each Telegram-served play also fires
`Cache.EnsureCached` so the next play/seek hits the disk fast path.

**Images and thumbnails are served by `internal/media`**, not the Range-streaming path: an image is
small enough that the handler just makes sure the file is on disk (downloading it inline the first
time, into `<CACHE_DIR>/photos/<photo_id>.bin`) and hands it to `http.ServeFile`. Thumbnails work
the same way but land in `<CACHE_DIR>/thumbs/<kind>_<row id>.jpg` and are fetched **lazily on first
request**, so only what someone actually scrolls past costs an RPC. Concurrent requests for the
same file are serialised by an in-process mutex map — a media grid asks for dozens of thumbs at
once.

Thumbnails get their **own slice of `CACHE_CAP_GB`** (10%, floor 1 GiB) reclaimed oldest-first by
`evictThumbs` before media eviction runs. They are individually tiny but unbounded in count — an
800k-image channel browsed end to end would otherwise crowd out every video on disk, and since
nothing would reclaim them the GC could only keep logging "above cap". "Oldest" is mtime (fetch
time), not last use: serving doesn't touch mtime and atime is unreliable, and the cost of guessing
wrong is one re-download of a 20 KB file. Files in the thumb dir that don't match
`<kind>_<row id>.jpg` are ignored rather than deleted.

**Caching** (`internal/cache/cache.go`): **edge cache** — *every played* video is enqueued for a
background full-file download (tdl-style multi-threaded; thread count scales with file size,
`bestThreads`) to `<CACHE_DIR>/videos/<doc_id>.bin`, unpinned so the LRU can evict it. **Favoriting**
enqueues the same download but **pinned** so it survives normal GC. Downloads dedup by `tg_doc_id`
(one copy on disk no matter how many users), write to a `tmp/` file then atomically promote.
`cache_entries` table tracks state; `cleanPartials` clears stale temp files on startup.

`cache_entries` is keyed by `(kind, tg_doc_id)` — `tg_doc_id` holds a document id for videos and a
photo id for images, two id spaces that could collide.

`evictIfNeeded` (every ~5 min) treats **the disk, not the DB, as the source of truth** for usage:
`scanCacheFiles` sums the actual `.bin` files under both `videos/` and `photos/`, then reconciles
both directions — files with no
`cache_entries` row are deleted as orphans, rows with no file are deleted as stale. Eviction is LRU
with unpinned first, but `CACHE_CAP_GB` (env, default **50**) is a hard ceiling: if pinned favorites
alone exceed it, least-recently-used **pinned** files get reclaimed too. Don't "fix" that back into
a DB-sum accounting or a pinned-is-sacred rule — both regressions let the disk grow unbounded.

**DB layer** (`internal/db/`): thin wrapper over `pgxpool`; one file per table-repo. Migrations are
embedded SQL (`migrations/*.sql`, `//go:embed`) applied in lexical order at startup, tracked in a
`schema_migrations` table. To add a schema change, drop a new numbered `NNNN_name.sql` file — do
not edit applied migrations. Full-text search is a Postgres `tsvector` (simple config) on
`videos.caption`, maintained by a trigger.

**Keyset pagination** (`orderClauseOn` / `keysetCursorOn` in `internal/db/videos.go`, shared with
`photos` via the table/alias parameters): every list is
`ORDER BY <col> <dir> NULLS LAST, id <dir>` and paged by a single `offset_id` query param — the
boundary row's sort key is looked up server-side by id, so the API/frontend never carry a compound
cursor. A plain `id < $n` cursor is **wrong** for the default date ordering: `id` is BIGSERIAL
(insert order) while sync writes incremental-at-top and backfill-at-bottom, so id order ≠ date
order and pages come up short, silently stopping pagination early. Sort keys: `date_desc` (default),
`date_asc`, `name_asc`, `name_desc`, `duration` (the last keeps the legacy plain-id cursor).

**"我的频道" shows a channel only once it holds media.** For a forum group that means media summed
across its topics (`TopicStats` → `topic_video_count`/`topic_photo_count` on the list DTO), **not**
merely having topics: discovery enumerates the topics of every forum the account has joined,
synced or not, so a "has topics" check lists groups nobody ever pulled a message from.

Favorites span every channel and topic, so the favorites response carries a `sources` side map
(channel id → title, plus the parent group for a topic; `ChannelSources`) and the grid renders a
"来自 群组 › 话题" line with links to both levels. It's a per-page map rather than fields on each
item because a page usually comes from a handful of channels. In `MediaGrid` those links sit
**beside** the clickable tile, never inside it — an `<a>` nested in an `<a>`/`<button>` is invalid
and the inner click gets swallowed.

`channels.video_count` / `photo_count` are what the list endpoints report as totals, not a live
`COUNT(*)`: on a million-row channel counting twice per first page costs hundreds of milliseconds
to render a number that only changes when a sync finishes. `MarkChannelIndexed` recomputes them
after every sync/import/clear; the figure is stale mid-sync, which the progress line covers.

**API** (`internal/api/router.go`): chi router. The media-facing routes are
`GET /channels/{id}/media` (merged list, `?kind=video|photo`), `GET /channels/{id}/topics` +
`POST /channels/{id}/topics/refresh`, `POST /channels/{id}/backfill`, `GET /media/search`,
`GET /photos/{id}` + `/file` + `/thumb`, `GET /videos/{id}/thumb`, and favorites that take either
`{"video_id":n}` (legacy) or `{"kind":"photo","id":n}` with `DELETE /favorites/photo/{id}`.
`GET /channels/{id}/videos` and `/videos/search` stay as the videos-only shape. All `/api/*` except `/auth/{register,login,logout}`
require a JWT via `web.RequireUser` middleware. Web auth uses argon2id (`internal/auth/web/`).

**Frontend** (`web/src/`): Vite + React + TypeScript + Tailwind v3 SPA. `api/client.ts` is the single
fetch wrapper; pages under `pages/` map to routes. Media lists go through `api/media.ts`
(`useMediaPages` — the two-cursor infinite query; "is there more" comes from the server's
`has_more`, never from a short page) and render via `components/MediaGrid.tsx` +
`MediaBrowser.tsx`; clicking an image opens `Lightbox.tsx` in place rather than navigating, and
←/→ there walk only the images of the current list. A topic IS a channel row, so `/channels/:id`
renders both — `ChannelDetail` shows the topic list when `is_forum`, the media grid otherwise.
`Player.tsx` picks a playback path per container — native `<video>` by default, `mpegts.js` for
FLV/MPEG-TS — and builds its playlist from the same media endpoints with `kind=video`.

**Frontend design system** — the UI implements the "Overseas Channel Workbench" language documented
in `DESIGN-overseas-channel-workbench.md` at the repo root. Read that doc before any visual change.
Its rules, as implemented:
- **Tokens live in `web/tailwind.config.js`** (the design doc describes a Tailwind v4 `@theme`
  block; this project is v3, so the tokens are in the config's `extend`). Components must use the
  token utilities — `brand-500`, `gray-200`, `text-theme-sm`, `shadow-theme-xs` — **never a raw hex**.
- **Component classes in `web/src/index.css`** under `@layer components`: `card`, `card-header`,
  `btn`/`btn-primary`/`btn-secondary`/`btn-ghost`/`btn-danger`/`btn-sm`, `input`, `select`, `nav-item`,
  `badge-*`. Reach for these before writing a new utility soup.
- **Shared React primitives in `components/ui.tsx`** (`PageHeader`, `Card`, `FilterBar`, `EmptyState`,
  `LoadingState`, `AlertStrip`, `MoreFooter`, `Toggle`, `cx`) and inline SVGs in `components/icons.tsx`
  — no icon library dependency.
- **Dark mode is class-based and full-parity.** A pre-paint script in `web/index.html` applies the
  stored polarity before first render; `src/theme.ts` owns the toggle. The `tgv-theme` localStorage
  key is duplicated across those two files — change both together. Every new surface needs its
  `dark:` variant.
- Typeface is Outfit (`@fontsource/outfit`), which ships no CJK glyphs — the `font-outfit` stack
  falls through to platform UI faces for Chinese text. That's intended.

## Conventions

- Commits follow Conventional Commits with **Chinese** descriptions (`feat: …`, `fix: …`) — see
  the global git rules. Run `npm run build` before committing if `web/` changed and include `dist`.
- Structured logging via `slog` JSON throughout the backend; sync/stream paths log progress every
  N items with `channel_id` etc. — match that style when adding hot-path logging.
- All user-facing UI copy is **Chinese**; code comments and log messages are English.
- `Video` field names in `api/client.ts` deliberately mirror TG Desktop's JSON export (snake_case
  as-is) so the import path and the live-sync path produce one shape — don't camelCase them.