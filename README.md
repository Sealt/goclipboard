# goclipboard

A lightweight, single-binary temporary cloud clipboard. Share text snippets via unique URLs with configurable TTL.

## Features

- Auto-generated random clipboard URLs
- Auto-save with configurable TTL
- **Real-time collaborative editing** via character-level CRDT (insert-after RGA)
- Remote carets / selections over WebSocket
- **File paste** per room — stored on disk; admin password for upload/delete, per-file password for download; list is open
- In-memory storage with automatic cleanup
- Per-IP rate limiting
- Health check endpoint
- Structured JSON logging
- Graceful shutdown
- Docker support with minimal scratch image

## Quick Start

```sh
go run .
```

Open http://localhost:8080 — you'll be redirected to a new clipboard.

```sh
make run
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT`   | `8080`  | Server listen port |
| `MAX_ROOMS` | `10000` | Max live clipboard rooms (in-memory) |
| `MAX_MEMORY_MB` | `256` | Estimated store budget for content + CRDT atoms |
| `UPLOAD_PASSWORD` | _(empty)_ | **Admin password** for file upload and delete. Empty disables file features |
| `FILE_DIR` | `data/files` | On-disk root for uploaded files (`{FILE_DIR}/{room}/{id}.bin`) |

When text-clipboard capacity is exceeded, writes return **HTTP 507** with `Retry-After: 60`. Existing rooms can still shrink or refresh TTL; new rooms and growth that would exceed the budget are rejected.

Set `UPLOAD_PASSWORD` to enable file paste. Files are stored **on disk** under `FILE_DIR` (no per-file / count caps). Anyone with the room URL can **list** metadata.

- **Per-room upload switch**: each space defaults to **upload off**. An admin **triple-clicks the room name** (path) to toggle it (admin password required). Setting is persisted under `{FILE_DIR}/{room}/settings.json`.
- **Upload**:
  - room **open** → only a **file password**
  - room **closed** → **admin password** + file password (one-shot; does not open the room for others)
- **Download**: requires that file's password.
- **Delete** / **toggle room upload**: require admin password.

File passwords are stored only as salt+SHA-256 hash next to the blob. Files still expire with the TTL you set at upload.

## Collaboration model

Concurrent edits merge at **Unicode code-point** granularity (not last-write-wins on the whole string).

- Each character is a CRDT atom with id `clientId:clock` and parent `after`.
- `clock` is a **Lamport** timestamp (must exceed any clock already in the document).
- Sibling order: higher clock first (so mid-string inserts stay next to the caret), then site id; document order is DFS from the root.
- Deletes are tombstones (cleared when the room TTL expires).
- Live typing is sent as WebSocket **ops**; late joiners receive a full **state** snapshot.
- REST `PUT` still works as a **full document replace** (rebuilds the CRDT chain) for simple clients / offline fallback.

## API

### Get clipboard
```http
GET /api/clipboard/{key}
```

### Save clipboard (full replace)
```http
PUT /api/clipboard/{key}
Content-Type: application/json

{"content": "...", "ttlSeconds": 3600, "clientId": "optional"}
```

### Delete clipboard
```http
DELETE /api/clipboard/{key}
```

### Real-time (WebSocket)
```http
GET /api/clipboard/{key}/ws?clientId={id}
Upgrade: websocket
```

**Server → client**

| type | meaning |
|------|---------|
| `state` | Full CRDT snapshot (`items`, linearized `content`, version, TTL) |
| `ops` | Applied op batch + `version` / `content` |
| `cursor` | Remote carets / selections (code-point offsets) |
| `files` | File list metadata for the room (`files[]`, `filesVersion`) after upload/delete/expiry |

**Client → server**

```json
{"type":"ops","ops":[{"op":"ins","id":"a:1","after":"","ch":"x"}],"ttlSeconds":3600}
{"type":"cursor","cursorPos":0,"selectionEnd":0,"color":"#61afef"}
```

Op shapes:

- Insert: `{"op":"ins","id":"site:clock","after":"parentId-or-empty","ch":"一"}` — `ch` is exactly one code point
- Delete: `{"op":"del","id":"site:clock"}`

Limits: content ≤ 1 MiB; ≤ 4096 ops per batch; WebSocket messages ≤ 256 KiB; global room count + estimated memory budget (see Configuration).

### File list
```http
GET /api/clipboard/{key}/files
```

### Room settings (admin)
```http
GET /api/clipboard/{key}/settings
→ {"key":"...","fileUploadEnabled":false}

PUT /api/clipboard/{key}/settings
Content-Type: application/json
X-Admin-Password: <UPLOAD_PASSWORD>

{"fileUploadEnabled":true,"adminPassword":"<UPLOAD_PASSWORD>"}
```

### Upload file
```http
POST /api/clipboard/{key}/files
Content-Type: multipart/form-data

file: <binary>
filePassword: <per-file download password>
ttlSeconds: 3600
# When room is closed, also send admin password:
# X-Admin-Password: <UPLOAD_PASSWORD>
# adminPassword: <UPLOAD_PASSWORD>
```

If `fileUploadEnabled=true`, only `filePassword` is required. If the room is closed, admin password is required for that upload (does not auto-enable the room). Response is `201` with file metadata (`id`, `name`, `size`, …).

### Download file (file password)
```http
GET /api/clipboard/{key}/files/{id}
X-File-Password: <per-file download password>
```

File password may also be passed as query `?filePassword=...` or `?password=...`.

### Delete file (admin password)
```http
DELETE /api/clipboard/{key}/files/{id}
X-Admin-Password: <UPLOAD_PASSWORD>
```

### Health check
```http
GET /healthz
```

## Docker

```sh
docker compose up -d
```

## Build

```sh
make build
```

## Test

```sh
make test
make test-cover
```
