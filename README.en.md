<div align="center">

<img src="docs/logo.svg" width="120" alt="GoClipboard" />

# GoClipboard

**A dead-simple, real-time collaborative clipboard you can self-host with one binary.**

Share text and files through a random URL. It edits itself while you watch — then expires when you say so.

[简体中文](README.md) | English

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-scratch%20image-2496ed?logo=docker&logoColor=white)
![CRDT](https://img.shields.io/badge/sync-RGA%20CRDT-7c3aed)
![PRs](https://img.shields.io/badge/PRs-welcome-brightgreen)
[![LinuxDo](https://img.shields.io/badge/LinuxDo-Friend%20Link-fd4d2b)](https://linux.do)

</div>

---

## Why?

Passwords, SSH keys, one-liners, config snippets, screenshots-in-a-hurry — copying something from one machine to another shouldn't require an account, a login flow, or yet another note-taking app.

GoClipboard gives you a **URL**. Paste into the room, share the link, and the content syncs live to everyone watching. It auto-expires, it has no database, and the whole thing is a single static binary that runs on anything.

## ✨ Features

- ⚡ **One binary, no runtime deps** — a ~10 MB Go static build; Docker image is `scratch`
- 🔗 **URL-first sharing** — every clipboard gets a random key; set a TTL in minutes, hours, or days and it cleans itself up
- 👥 **Real-time collaborative editing** — character-level **RGA CRDT** over WebSocket: concurrent edits merge, never clobber each other, even on flaky networks
- 🖱️ **Remote carets & selections** — see exactly where your teammates are typing, each in their own color
- 🔑 **Room passwords** — type your own or auto-generate; pick the scope: **edit only** (viewing stays open) or **view** (the password is required to see any content at all)
- ↩️ **Undo / redo** — collaborative undo (Ctrl+Z / Ctrl+Shift+Z) built on CRDT inverse ops, immune to concurrent edits
- 🕘 **Version history** — server-side trail (auto-captured, max 20); preview / restore / clear; password-gated when the room is locked; history retains deleted text — clear or lock before sharing secrets
- 📝 **Markdown preview** — rendered preview with code highlighting (client-side, sanitized output)
- 🔗 **QR share** — in-app QR code for the room (encodes the room link, optional `mode` param), scan with your phone
- 📁 **File paste per room** — drag a file in, get a link; disk-backed, per-file download passwords, admin-gated upload/delete
- 🔐 **Password hashing** — room and file passwords store salt + SHA-256 only (never plaintext on disk)
- 💾 **Persistence** — rooms are snapshotted as CRDT docs (default `data/rooms`) and restored on restart; set `PERSIST_DIR=off` for a pure in-memory store
- ⌨️ **CLI client** — the same binary pushes/pulls: `echo hi | goclipboard push` prints a URL
- 🌙 **Dark theme + bilingual UI** — follows the system or toggles manually; Chinese/English with one click
- 🚦 **Abuse resistance** — per-IP rate limiting plus an adaptive blocklist that bans scanners for 30 minutes
- 🧹 **Self-limiting memory** — budget-capped in-memory store; when full, writes get `HTTP 507` + `Retry-After`
- 📊 **Operability** — structured JSON logs, `/healthz`, graceful shutdown
- 🐳 **Effortless deploy** — multi-arch Docker build, one-command `docker compose up`
- 🇨🇳 **Polished single-page frontend** — vanilla JS, no build step (Chinese/English UI)

## 🖼️ Screenshot

<img src="docs/screenshot.png" width="720" alt="GoClipboard editor: live CRDT content, TTL picker, peer presence and sync status" />

## 🚀 Quick Start

```sh
go run .
# or
make run
```

Open **http://localhost:8080** — you'll be redirected to a fresh clipboard. Done.

```sh
docker compose up -d          # self-host with Docker
```

### Command-line client

The same binary is both server and client (`-url` flag or `GOCLIPBOARD_URL` env; default `http://localhost:8080`):

```sh
echo "hello" | goclipboard push            # → prints the room URL
goclipboard push -ttl 2h notes.txt         # read a file, expire in 2 hours
goclipboard push -password 'pw' -key AbC123 notes.txt  # write to a locked room
goclipboard pull https://host/AbC123       # print room content to stdout
goclipboard pull -o out.txt AbC123         # write to a file
goclipboard pull -password 'pw' AbC123     # view-scoped password-protected room
```

Without `-key`, the server generates the room key (`POST /api/clipboard`).

## 🧠 How it works: CRDT, not last-write-wins

Text editing is normally a fight: two people type, one overwrites the other. GoClipboard sidesteps this with a **character-level CRDT** (insert-after RGA):

- Every character is an atom: `id = clientId:clock` (Lamport timestamp), with a `parent` pointer.
- Concurrent inserts merge at **Unicode code-point** granularity — no whole-string last-write-wins.
- Sibling order is deterministic: higher clock first, then site id, so mid-string inserts stay next to your caret.
- Deletes are tombstones (garbage-collected when the room TTL expires).
- Live typing ships as WebSocket **ops**; late joiners get a full **state** snapshot; reconnecting clients merge from the server.
- REST `PUT` still works as a full-document replace (with optional optimistic concurrency via `baseVersion` → `409` on conflict), for simple clients and offline fallback.

This is also why weak networks behave: ops are idempotent, order-independent, and cheap to rebroadcast.

## 🔌 API

### Clipboard (REST)

```http
POST   /api/clipboard                  # create a room (server-generated key)
GET    /api/clipboard/{key}            # fetch content + version
PUT    /api/clipboard/{key}            # full replace
DELETE /api/clipboard/{key}            # delete
GET    /api/clipboard/{key}/history    # version history (password required when room is locked)
POST   /api/clipboard/{key}/history    # force-capture current content
DELETE /api/clipboard/{key}/history    # clear history
```

```sh
curl -X PUT localhost:8080/api/clipboard/AbC123 \
  -H 'Content-Type: application/json' \
  -d '{"content":"hello 世界","ttlSeconds":3600,"clientId":"my-site"}'
```

The response is the room's content and metadata (`content` / `ttlSeconds` / `expiresAt` / `version` / `passwordScope`, etc.).

`PUT` accepts an optional `baseVersion` for optimistic concurrency — a stale overwrite is rejected with `409` plus current state, so offline/REST clients can merge instead of clobbering.

### Room passwords

The share dialog sets a room password (**type your own or regenerate**) and picks its scope:

- **Edit** (default) — content viewing stays open; editing (writes, delete, password changes, **reading/clearing history**) requires the password
- **View** — viewing *and* editing require the password: unauthenticated sessions receive no content, and the file list / downloads / uploads / deletes are protected too

The password lives only with whoever set it — the server never returns it (responses carry only `passwordSet` / `passwordScope`). On an unlocked room, any link holder can set a password and claim the lock — set one before sharing if that matters.

```http
GET /api/clipboard/{key}/password          # → {"passwordSet":true,"scope":"view"}
PUT /api/clipboard/{key}/password          # set / rotate / clear
# rotating or clearing a locked room requires currentPassword to match;
# scope: "edit" | "view"
```

Reading a **view-scoped** room requires the password (prefer `X-Goclip-Password`; `?password=` works for curl but may hit access logs) — otherwise `GET` returns `401`. A WebSocket session first receives a locked state frame (`passwordRequired: true`, no content), then sends `{"type":"auth","password":"..."}` to unlock; a wrong password gets an `invalid view password` error frame.

### Version history

Up to 20 server-side content snapshots per room (auto-captured with a 5s throttle; manual capture available). **Snapshots retain text that was later deleted or overwritten**, so:

- Any password-protected room requires the room password for `GET/POST/DELETE …/history`
- Clear history (or use a view-scoped password) before sharing sensitive rooms
- History counts toward `MAX_MEMORY_MB`; near the cap, growth can return `507`

### Real-time (WebSocket)

```http
GET /api/clipboard/{key}/ws?clientId={id}
```

**Server → client**

| type     | meaning                                             |
|----------|-----------------------------------------------------|
| `state`  | Full CRDT snapshot: `items`, linearized `content`, version |
| `ops`    | Applied op batch + `version` / `content`                   |
| `cursor` | Remote carets / selections (code-point offsets)            |
| `files`  | File list metadata after upload/delete/expiry              |

**Client → server**

```json
{"type":"ops","ops":[{"op":"ins","id":"a:1","after":"","ch":"x"}],"ttlSeconds":3600,"password":"room password (locked rooms)"}
{"type":"cursor","cursorPos":0,"selectionEnd":0,"color":"#61afef"}
{"type":"auth","password":"room password"}   // view-scoped rooms: unlock content delivery
```

Ops: `ins` (`id`, `after`, `ch` — exactly one code point) and `del` (`id`).

**Limits:** content ≤ 1 MiB · 4096 ops/batch · 256 KiB per WS message.

### File sharing

| action                                  | auth                                  |
|-----------------------------------------|---------------------------------------|
| `GET  /files`                           | open (anyone with the room URL)       |
| `POST /files` (multipart)               | room open → file password; closed → admin + file password |
| `GET  /files/{id}`                      | that file's password (`X-File-Password` or `?filePassword=`; `?password=` is room-only) |
| `DELETE /files/{id}`                    | admin password                        |
| `GET/PUT /settings` (upload toggle)     | admin password                        |

Rooms default to **upload off**. An admin triple-clicks the room name (path) to toggle it — the setting persists on disk. Files expire with the room TTL. The admin password comes from `UPLOAD_PASSWORD`; leave it empty to disable file features entirely.

### Health

```http
GET /healthz
```

## ⚙️ Configuration

All via environment variables:

| Variable          | Default       | Description                                             |
|-------------------|---------------|---------------------------------------------------------|
| `PORT`            | `8080`        | Listen port                                             |
| `MAX_ROOMS`       | `10000`       | Max live clipboard rooms (in-memory)                    |
| `MAX_MEMORY_MB`   | `256`         | Estimated budget for content + CRDT atoms + history → `507` when exceeded |
| `UPLOAD_PASSWORD` | _(empty)_     | Admin password for file upload/delete; empty disables file features |
| `FILE_DIR`        | `data/files`  | On-disk root for uploaded files (`{FILE_DIR}/{room}/{id}.bin`) |
| `PERSIST_DIR`     | `data/rooms`  | Room snapshots on disk (one file per room, ~250 ms debounce); restored on restart. Set to `off` / `none` / `-` for a purely in-memory/ephemeral store |

Operational defaults: rate limit 10 req/s (burst 20), adaptive blocklist (hard threshold 5, scan threshold 10, 30 s window, 30 min ban), 1-minute cleanup sweep, 10 s graceful shutdown.

## 🐳 Docker

```sh
docker compose up -d
```

Multi-arch (`linux/amd64`, `linux/arm64`, …) scratch image — no shell, no libc, just the binary and CA certs. Healthcheck wired up; the named volume persists uploads and room snapshots (compose.yaml uses `PERSIST_DIR=/data/rooms`).

## 🛠️ Development

```sh
make run          # dev server on :8080
make test         # go test ./...
make test-js      # crdt.js cross-convergence tests (needs node)
make test-cover   # coverage report → coverage.html
make build        # single static binary
```

The codebase is small and deliberately boring:

```
main.go                wiring: config, CLI dispatch (push/pull), middleware, shutdown
internal/crdt/         RGA sequence CRDT (insert / delete / materialize / snapshot)
internal/store/        in-memory room store + on-disk file store, optional persistence, TTL cleanup
internal/handler/      HTTP + WebSocket routes, file upload/download
internal/middleware/   rate limit, blocklist, security headers, request logging
internal/cli/          push/pull client mode (same binary)
static/                frontend: app.js + crdt.js (vanilla JS, no build step; vendor/ = MIT single-file libs)
```

## 📜 License

Released under the [MIT](LICENSE) license — see the [LICENSE](LICENSE) file.
