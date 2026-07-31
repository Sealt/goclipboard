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
- 📁 **File paste per room** — drag a file in, get a link; disk-backed, per-file download passwords, admin-gated upload/delete
- 🔐 **Password hashing** — file passwords stored as salt + SHA-256 only
- 🚦 **Abuse resistance** — per-IP rate limiting plus an adaptive blocklist that bans scanners for 30 minutes
- 🧹 **Self-limiting memory** — budget-capped in-memory store; when full, writes get `HTTP 507` + `Retry-After`
- 📊 **Operability** — structured JSON logs, `/healthz`, graceful shutdown
- 🐳 **Effortless deploy** — multi-arch Docker build, one-command `docker compose up`
- 🇨🇳 **Polished single-page frontend** — vanilla JS, no build step (Chinese UI)

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
GET    /api/clipboard/{key}              # fetch content + version
PUT    /api/clipboard/{key}              # full replace
DELETE /api/clipboard/{key}              # delete
```

```sh
curl -X PUT localhost:8080/api/clipboard/AbC123 \
  -H 'Content-Type: application/json' \
  -d '{"content":"hello 世界","ttlSeconds":3600,"clientId":"my-site"}'
```

`PUT` accepts an optional `baseVersion` for optimistic concurrency — a stale overwrite is rejected with `409` plus current state, so offline/REST clients can merge instead of clobbering.

### Real-time (WebSocket)

```http
GET /api/clipboard/{key}/ws?clientId={id}
```

**Server → client**

| type     | meaning                                                    |
|----------|------------------------------------------------------------|
| `state`  | Full CRDT snapshot: `items`, linearized `content`, version |
| `ops`    | Applied op batch + `version` / `content`                   |
| `cursor` | Remote carets / selections (code-point offsets)            |
| `files`  | File list metadata after upload/delete/expiry              |

**Client → server**

```json
{"type":"ops","ops":[{"op":"ins","id":"a:1","after":"","ch":"x"}],"ttlSeconds":3600}
{"type":"cursor","cursorPos":0,"selectionEnd":0,"color":"#61afef"}
```

Ops: `ins` (`id`, `after`, `ch` — exactly one code point) and `del` (`id`).

**Limits:** content ≤ 1 MiB · 4096 ops/batch · 256 KiB per WS message.

### File sharing

| action                                  | auth                                  |
|-----------------------------------------|---------------------------------------|
| `GET  /files`                           | open (anyone with the room URL)       |
| `POST /files` (multipart)               | room open → file password; closed → admin + file password |
| `GET  /files/{id}`                      | that file's password (`X-File-Password` or `?filePassword=` / `?password=`) |
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
| `MAX_MEMORY_MB`   | `256`         | Estimated budget for content + CRDT atoms → `507` when exceeded |
| `UPLOAD_PASSWORD` | _(empty)_     | Admin password for file upload/delete; empty disables file features |
| `FILE_DIR`        | `data/files`  | On-disk root for uploaded files (`{FILE_DIR}/{room}/{id}.bin`) |

Operational defaults: rate limit 10 req/s (burst 20), adaptive blocklist (hard threshold 5, scan threshold 10, 30 s window, 30 min ban), 1-minute cleanup sweep, 10 s graceful shutdown.

## 🐳 Docker

```sh
docker compose up -d
```

Multi-arch (`linux/amd64`, `linux/arm64`, …) scratch image — no shell, no libc, just the binary and CA certs. Healthcheck wired up, data persisted in a named volume.

## 🛠️ Development

```sh
make run          # dev server on :8080
make test         # go test ./...
make test-cover   # coverage report → coverage.html
make build        # single static binary
```

The codebase is small and deliberately boring:

```
main.go                wiring: config, middleware, shutdown
internal/crdt/         RGA sequence CRDT (insert / delete / materialize / snapshot)
internal/store/        in-memory room store + on-disk file store, TTL cleanup
internal/handler/      HTTP + WebSocket routes, file upload/download
internal/middleware/   rate limit, blocklist, security headers, request logging
static/                frontend: app.js + crdt.js (vanilla JS, no build step)
```

## 📜 License

Released under the [MIT](LICENSE) license — see the [LICENSE](LICENSE) file.
