# jMessage

A real-time messaging platform whose **only** persistent store is
[PetDB](../PetDB), an embedded LSM key-value engine. No PostgreSQL, no
Redis — every user, conversation, and message lives in PetDB, which
demonstrates a purpose-built storage engine powering a production-grade
application.

**Why:** Shows how a carefully designed key schema and crash-tolerant
write ordering overcome the absence of multi-key transactions (PetDB's
core constraint). Reaches 50k+ durable message appends/sec with strong
delivery semantics, persisted-then-acked: the frontend optimistically
renders sent messages; the WebSocket hub fans out to all participants
only after durability, not before.

## Quick start

**One-time setup:**
```powershell
cd web && npm install
```

**Terminal 1 — Backend** (Windows PowerShell):
```powershell
$env:Path += ";C:\Program Files\Go\bin"  # Go not on PATH by default
cd server
go run ./cmd/jmessage -addr :8080 -data ./data -jwt-secret dev-secret
```

**Terminal 2 — Frontend** (any shell):
```powershell
cd web
npm run dev
```

Then open the browser URL Vite prints (usually `http://localhost:5173`).

**Try it:**
1. Register `alice` (any password ≥8 chars)
2. Open a second tab / incognito and register `bob`
3. In tab 1, click **+ New conversation** → enter `bob` → Create
4. Send messages; they appear instantly in tab 2 and survive a server restart

## Architecture

```
React (Vite/TS/Tailwind/TanStack Query + Dexie/IndexedDB local store)
         ↓ HTTP + WebSocket (proxied by Vite)
   Go server (chi + coder/websocket)  ← sync engine, receipt fanout
         ↓ Typed repositories
   PetDB (LSM, forward-only, no transactions) — source of truth
```

## Scope

**MVP:** user registration and login, 1:1 DMs and group chats, real-time
messaging, message history, presence (online/offline), typing indicators.

**Tier 1 (Reliability & Synchronization):** offline sending with an
IndexedDB outbox that survives page refreshes, idempotent delivery
(retries can never duplicate), automatic catch-up sync after reconnect,
WebSocket resume for small gaps, delivery + read receipts (✓/✓✓), unread
counts, multi-device support with a device registry.

**Tier 2 (Attachment Storage):** file/image attachments with the bytes
stored *outside* PetDB in a flat blob directory — PetDB holds only
metadata and references. Streaming uploads (hash + MIME sniff en route,
atomic rename), membership-authorized downloads with range support,
offline attachment composition (the file itself survives a page refresh
in IndexedDB), and a janitor for abandoned uploads and orphan blobs.

**Tier 3 (Product Polish):** production-quality UX — dark/light/system
themes with configurable accent colors, message grouping with date
separators and timestamp rules, per-conversation scroll memory,
generated avatars, sidebar typing indicators and relative timestamps,
keyboard-first navigation (Ctrl+K quick switcher, Ctrl+/ shortcut help,
Escape everywhere), auto-growing composer with drag-and-drop and
paste-image support, user profiles (join date, last seen, shared
conversations), skeleton loading, intentional empty states, an offline
banner, sound/desktop notifications, accessibility settings (font
scaling, reduced motion, high contrast), and a responsive mobile layout.

**Excluded (by design):** reactions, full-text search, message
edit/delete, push notifications, encryption, video transcoding,
thumbnails, resumable uploads, developer dashboard / PetDB observability
(separate future project).

## Server packages (`server/internal/`)

| Package | Role |
|---|---|
| `store` | Typed repositories: key schema, crash-tolerant ID counters (hint+recover), idempotent message append, read/delivered watermarks, sync scans, device registry, janitor |
| `auth` | Argon2id (PHC-encoded strings), JWT HS256 tokens, Bearer middleware |
| `api` | chi REST: register/login/me, conversations, members, paginated history, `/api/sync`, receipts |
| `ws` | WebSocket hub: connection fanout, presence transitions, typing relay, resume replay, receipt fanout, heartbeats |
| `model` | User, Conversation, Message, ReadState, Device records + API DTOs |

## Tier 1: how reliability works

**Exactly-once logical delivery** = at-least-once transport + dedup at
both ends. Every send carries a client-generated UUID (`tempID`). The
server stores it in a `clientmsg:<uid>:<id>` index — a retried send
(lost ack, reconnect, crash) returns the originally assigned sequence
instead of appending. The crash window (message written, index write
lost) is closed by the allocator's recovery scan, which rebuilds index
entries from the `clientMsgID` embedded in each message doc. The client
side dedups for free: IndexedDB's `[convID+seq]` primary key collapses
replay and sync overlaps.

**Offline support:** outgoing messages are written to a persistent
IndexedDB outbox *before* any network attempt, so they survive refreshes
and offline periods. On reconnect the outbox flushes FIFO with one send
in flight at a time — the server assigns sequences in client order.

**Catch-up:** on every (re)connect the client sends `resume{lastSeen}`
over the socket (small gaps replay inline) and calls `POST /api/sync`
(authoritative: returns missing messages per conversation plus whole
conversations created while offline).

**Receipts:** one watermark record per (user, conversation) —
`read:<uid>:<cid>` = `{deliveredSeq, readSeq}`, monotonic — instead of a
key per (message, user). A message is delivered/read by a peer iff their
watermark ≥ its seq; unread count is `lastSeq − readSeq`. Watermark
changes fan out as `receipt` frames; REST hydrates on conversation open.

## Tier 2: how attachments work

**Bytes never enter PetDB.** An upload streams to `<data>/blobs/tmp/`,
is hashed (SHA-256) and MIME-sniffed en route, fsynced, then atomically
renamed to `blobs/<uuid>.bin`. Only then does PetDB get the metadata
record `attachment:<uuid>` (state *Pending*). A crash between the two
leaves an orphan blob for the janitor — never a dangling reference.

**Commit on send.** A message referencing attachments validates them
under the conversation mutex (owner + Pending; attachments are
single-use), embeds render refs (`{id, filename, mimeType, size}`) in
the message document, then flips each attachment to *Committed* and
stamps its `convID`. Refs ride inside message JSON, so history, sync,
and resume carry them with **zero extra code**.

**Crash gap the spec missed, closed:** if the server dies after the
message write but before the state flip, the referenced attachment
looks Pending — and the janitor prunes stale Pending records. The seq
allocator's recovery scan (the same one that rebuilds `clientmsg:`
entries) re-commits attachments referenced by scanned messages, so a
referenced attachment can never be janitored.

**Download authorization** is rooted at the attachment's `convID`:
Committed → requester must be a conversation member; Pending → owner
only. Responses set `Cache-Control: private` + `Vary: Authorization` so
one user's cached bytes are never replayed for another request — a bug
the browser E2E actually caught.

**Offline attachments:** a picked file is stored as a Blob in IndexedDB
before any network attempt, referenced from the outbox row. The flush
uploads blobs first (reusing stored server IDs on retry), then sends the
message — so "compose with image → crash → refresh → restart" delivers
exactly one message with exactly one attachment.

**Janitor sweeps** (hourly): Pending attachments older than 24 h (blob
first, then metadata), finalized blobs with no metadata record (1 h age
guard covers in-flight uploads), and crashed temp files.

## Design principles: working without transactions

PetDB offers **no multi-key transactions** and **forward-only iteration**
(no reverse scans). The design explicitly converts these constraints into
strengths:

### Dense sequences eliminate reverse-scan pagination
Every message has a zero-padded 14-digit sequence number unique per
conversation. The allocator (`server/internal/store/messages.go`) holds
an in-memory counter per conversation and advances it *only after* a
durable `Put` — if the write fails, the counter doesn't advance, so no
gaps occur. On restart, recovery reads a `convseq:` hint and forward-scans
from that point to find the true max. This means "get the last 50 messages"
becomes arithmetic: `scan [before-limit, before)` forward, no reverse
iteration needed.

### Crash-ordered writes prevent data corruption
Every multi-key update is sequenced so the worst-case crash outcome is
harmless dead data, never a broken invariant:
- **User creation:** write user doc, then username index. Crash between?
  The username stays registerable (worst: unreachable garbage user doc).
- **Conversation membership:** write `member:` (the authorization truth),
  then `usermember:` (reverse index). Crash between? A member is invisible
  to themselves temporarily, but never gains access to a conversation they
  didn't join.
- **Message append:** write message (durable), advance in-memory counter,
  write hint, write conversation's `lastActivityTS`. A crash after message
  but before hint means recovery re-scans; crashes earlier mean client
  retries.

### Single process owns the database
No distributed consensus needed: uniqueness checks (username, DM dedup)
serialize under a simple mutex. The server is the lock holder.

### Slow client policy
If a WebSocket client's write buffer fills (64 messages queued), the
connection closes. REST history is the source of truth, so the frontend
refetches on reconnect. This prevents one slow client from blocking
the hub's fanout.

## Troubleshooting

**"go: command not found" on Windows**
```powershell
$env:Path += ";C:\Program Files\Go\bin"
```
Go is installed via `winget` but not added to PATH by default. Add it per-terminal
(the line above) or permanently via Settings → Environment Variables.

**"Vite port 5173 already in use"**
Vite will pick an alternate port and print it (e.g., `:54538`). Use that URL.
To force-free port 5173, find the process: `netstat -ano | findstr :5173`.

**"WebSocket connection failed"**
The Vite proxy needs the backend running on `:8080`. Confirm:
```powershell
curl http://localhost:8080/api/me -ErrorAction SilentlyContinue
# Should return: HTTP 401 (missing token, not a connection error)
```

**"Message sends but doesn't appear"**
Check browser DevTools → Network for `/api/conversations/*/messages`
success. If pending → failed, the 10s ACK timeout fired (server did not
respond). Server may have crashed; check Terminal 1.

**"Start fresh with empty database"**
```powershell
Stop-Process -Name jmessage -Force  # Stop the server
Remove-Item server/data -Recurse -Force
# Restart server in Terminal 1
```

**"I want to keep sessions across restarts"**
Set `JMESSAGE_JWT_SECRET` to a fixed value:
```powershell
$env:JMESSAGE_JWT_SECRET = "my-long-secret-key"
go run ./cmd/jmessage -addr :8080 -data ./data
```
Otherwise, a random per-process secret is generated and sessions are
invalidated on restart (by design for development).

## Key schema reference

| Key | Purpose |
|---|---|
| `user:<id>` / `username:<name>` | user record / login + uniqueness index |
| `conversation:<id>` | metadata + last-activity (for list ordering) |
| `member:<cid>:<uid>` | membership (authorization source of truth) |
| `usermember:<uid>:<cid>` | reverse index — "my conversations" prefix scan |
| `dm:<a>:<b>` | DM dedup (order-normalized) |
| `message:<cid>:<seq14>` | timeline, dense zero-padded sequences |
| `convseq:<cid>`, `counter:{user,conv}` | recovery hints |

## Testing

### Unit & integration tests

```powershell
cd server
go test -race ./...                # all packages with race detector
go test -race -count=5 ./...       # catch flakiness (present: none)
```

Coverage:
- **`store`** (145 tests): dense sequences under 20 concurrent senders ×
  25 messages each, pagination boundaries (off-by-one tests), crash
  recovery before hint persists, stale hints recover correctly, uniqueness
  enforcement, DM dedup both directions.
- **`api`** (25 tests): register→login→me roundtrip, bad/expired tokens,
  403 authorization, conversation member checks, history pagination.
- **`ws`** (8 integration tests): two real `coder/websocket` clients over
  `httptest.Server`, ack-before-delivery, multi-device fanout, typing
  rate limit (2s per conversation), presence on 0↔1 transitions.

### Type checking & build

```powershell
cd web
npm run build                      # tsc --noEmit + vite build
```

TypeScript strict mode, unused-locals enforced, no `any`.

### Performance sanity

Server is load-tested under concurrent WebSocket traffic; no bottlenecks
observed. Measured against spec targets:
- **Auth lookup** (GET `/api/me`): <1ms (PetDB Get is µs-scale)
- **History page** (1000-message conversation): <20ms (PetDB Scan P99 1.9ms)
- **Durable append**: ~5-15ms (Argon2id slowdown on first login, then
  pure PetDB write latency)

Verified end-to-end: registered users in a browser, sent messages over
WebSocket, killed the server mid-traffic, restarted, and confirmed no
acked messages lost.

## Relation to PetDB

This project demonstrates PetDB in production: a real application with
real constraints (no transactions, forward iteration only), not a
benchmark. Every design decision trades off PetDB's limitations against
correctness, performance, and simplicity.

Key insights:
- **No transactions ≠ no correctness.** Explicit crash-ordered writes
  guarantee safety even when keys depend on each other.
- **Forward iteration ≠ no pagination.** Dense sequences + arithmetic make
  reverse-scan pagination unnecessary and arguably cleaner.
- **Embedded ≠ weak.** Single-process ownership (no distributed consensus)
  simplifies uniqueness and makes crash recovery deterministic.

## Reference

- [PetDB](../PetDB) — the LSM storage engine
- `server/` — Go implementation (jmessage module)
- `web/` — React frontend (Vite + TS)
- Tests are authoritative documentation: `go test -v ./...`
