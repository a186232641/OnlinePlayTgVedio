# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

A self-hosted web app for browsing, searching, favoriting, and streaming videos from the
Telegram channels a user has joined. Go backend (gotd/td MTProto client) + React SPA + Postgres.
The README.md is comprehensive (in Chinese) — read it for deployment and env-var detail. This
file focuses on the things that require reading multiple files to understand.

## Commands

Backend (Go 1.25+, module `github.com/hanfeilong/onlineplaytgvideo`):
```bash
make build        # go build -> bin/server
make run          # go run ./cmd/server   (needs env loaded — see below)
make test         # go test ./...
make vet          # go vet ./...
go test ./internal/video/ -run TestName   # run a single test
```
The Makefile auto-`include`s `.env` and exports it, so `make run`/`make dev-server` see
`TG_API_ID`, `MASTER_KEY`, `DB_DSN`, etc. Running the binary directly requires exporting those
yourself. The only Go tests live in `internal/video/stream_test.go` (Range/chunk-alignment math).

Frontend (Node 20+, in `web/`):
```bash
cd web && npm install && npm run dev      # Vite dev server :5173, proxies /api -> :8080
npm run build                             # tsc + vite build -> web/dist
```

Local dev loop: `make dev-db-up` (Postgres in docker) → `make dev-server` (backend :8080) →
`make dev-web` (Vite :5173). Open http://localhost:5173.

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

**Two ingest paths** populate the `videos` table, both producing the same row shape:
- `internal/api/handlers/import.go` — upload a Telegram JSON export (`messages.json`).
- `internal/indexer/sync.go` — pull live history via `messages.getHistory`. **Streaming +
  resumable** (`walkHistory`): writes each batch to the DB as it pages, so a crash/timeout leaves
  partial progress instead of losing everything. Two passes, both driven by the stored cursor so a
  resume needs no extra bookkeeping: **Phase A incremental** = `MinID=MAX(tg_msg_id)` pulls messages
  newer than what we have; **Phase B backfill** = `OffsetID=MIN(tg_msg_id)` walks older history to
  the very bottom, then sets `channels.history_complete` so later sweeps skip the backfill. Write
  order doesn't matter — queries sort by `date`. A probe call first flags "stale access_hash / lost
  membership" (0 messages). Sync state is **in-memory only** (`Indexer.syncs` map), surfaced via
  `GET /channels/{id}/sync` with live `walked`/`imported`/`skipped`. A background scheduler
  (`indexer/scheduler.go`, started in `main.go`) re-runs sync every `SYNC_INTERVAL` (env, default
  30m; 0/off disables) for every channel that is `last_indexed_at IS NOT NULL AND auto_sync`
  (per-channel opt-in, default off; manual `SyncStart` ignores it), via the same idempotent
  `SyncStart`. `MarkChannelIndexed` recomputes `video_count` via `COUNT(*)` — never
  a per-run delta, or incremental syncs would clobber the total.

**Streaming** (`internal/video/stream.go`): on each request, if a complete cached file exists for
`tg_doc_id` (`Cache.CompletePathFor`) it serves straight off disk via `http.ServeFile` and returns.
Otherwise `serveFromTelegram`: browser `Range: bytes=…` → backend aligns to 4KB boundaries and loops
`tg.Client.UploadGetFile(Precise=true)` (bounded parallel prefetch) in 1MiB chunks via the file's-DC
client (`cli.APIForDC`), trimming the alignment prefix on the first chunk and overflow on the last.
JSON-imported rows (`TGDocID=0`) resolve their locator on first play; on `FILE_REFERENCE_EXPIRED` it
lazily re-fetches via `channels.getMessages` (`refresh.go`), updates the DB, and retries once.
CDN-redirected files are **not** supported (returns 500). Each Telegram-served play also fires
`Cache.EnsureCached` so the next play/seek hits the disk fast path.

**Caching** (`internal/cache/cache.go`): **edge cache** — *every played* video is enqueued for a
background full-file download (tdl-style multi-threaded; thread count scales with file size,
`bestThreads`) to `<CACHE_DIR>/videos/<doc_id>.bin`, unpinned so the LRU can evict it. **Favoriting**
enqueues the same download but **pinned** so GC never drops it. An LRU GC runs every ~5 min to evict
unpinned entries over `CACHE_CAP_GB` (env, default **50**). Downloads dedup by `tg_doc_id` (one copy
on disk no matter how many users), write to a `tmp/` file then atomically promote. `cache_entries`
table tracks state; `cleanPartials` clears stale temp files on startup.

**DB layer** (`internal/db/`): thin wrapper over `pgxpool`; one file per table-repo. Migrations are
embedded SQL (`migrations/*.sql`, `//go:embed`) applied in lexical order at startup, tracked in a
`schema_migrations` table. To add a schema change, drop a new numbered `NNNN_name.sql` file — do
not edit applied migrations. Full-text search is a Postgres `tsvector` (simple config) on
`videos.caption`, maintained by a trigger.

**API** (`internal/api/router.go`): chi router. All `/api/*` except `/auth/{register,login,logout}`
require a JWT via `web.RequireUser` middleware. Web auth uses argon2id (`internal/auth/web/`).

**Frontend** (`web/src/`): Vite + React + TypeScript + Tailwind SPA. `api/client.ts` is the single
fetch wrapper; pages under `pages/` map to routes (Channels, ChannelDetail, Player, Search,
Favorites, TgAccounts, TgBind, etc.).

## Conventions

- Commits follow Conventional Commits with **Chinese** descriptions (`feat: …`, `fix: …`) — see
  the global git rules. Run `npm run build` before committing if `web/` changed and include `dist`.
- Structured logging via `slog` JSON throughout the backend; sync/stream paths log progress every
  N items with `channel_id` etc. — match that style when adding hot-path logging.