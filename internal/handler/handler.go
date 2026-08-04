package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"goclipboard/internal/crdt"
	"goclipboard/internal/middleware"
	"goclipboard/internal/model"
	"goclipboard/internal/store"

	"github.com/gorilla/websocket"
)

const (
	defaultTTL      = time.Hour
	maxRequestBytes = 1 << 20
	// Offline if no cursor/heartbeat for this long (client heartbeats ~5s).
	// Must be >= client peerStaleMs (14s) so the client prunes first.
	cursorStaleMs  = 15_000
	maxClientIDLen = 64
	maxColorLen    = 32
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 60 * time.Second
	wsPingPeriod   = 25 * time.Second
	// How often to push a pruned cursor snapshot to each client.
	wsPresencePeriod = 5 * time.Second
	// Large enough for op batches and CRDT state snapshots (content still capped at 1MiB).
	wsMaxMessage = 256 << 10

	// DoS guards for the WebSocket channel. Defaults leave headroom for
	// legitimate multi-user sessions (a client flushes ops ~17 msg/s and
	// cursors ~13 msg/s) while cutting off floods; a cut connection is
	// self-healing because clients resync from the state snapshot on
	// reconnect.
	DefaultMaxWSConns      = 512
	DefaultMaxWSConnsPerIP = 32
	DefaultWSMsgRate       = 50.0 // tokens per second
	DefaultWSMsgBurst      = 100
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

type Handler struct {
	store          *store.Store
	files          *store.FileStore
	uploadPassword string
	logger         *slog.Logger
	broker         *broker
	cursors        *cursorStore
	static         fs.FS
	ipResolver     *middleware.IPResolver
	wsConns        *wsConnLimiter
	wsMsgRate      float64
	wsMsgBurst     int
	// passFails budgets wrong room-password attempts per (IP, room).
	passFails *passFailTracker

	// lastContentEvent lets WS subscribers send compact ops when possible.
	eventMu sync.Mutex
	// key -> last content event (ops or full state flag)
	lastEvent map[string]contentEvent
}

type contentEvent struct {
	version    int64
	generation int64
	updatedBy  string
	// if ops non-nil, prefer broadcasting ops; otherwise full state
	ops     []crdt.Op
	content string
	full    bool
}

// Options configures optional handler features (file uploads, etc.).
type Options struct {
	Files          *store.FileStore
	UploadPassword string
	// IPResolver decides the client IP for per-IP WebSocket limits. Nil uses
	// the direct peer address (no forwarded headers are trusted).
	IPResolver *middleware.IPResolver
	// WebSocket DoS guards. Zero or negative values keep the defaults.
	MaxWSConns      int
	MaxWSConnsPerIP int
	WSMsgRate       float64 // per-connection inbound message tokens per second
	WSMsgBurst      int     // per-connection inbound message burst
}

func New(sto *store.Store, static fs.FS, logger *slog.Logger, opts ...Options) *Handler {
	h := &Handler{
		store:      sto,
		logger:     logger,
		broker:     newBroker(),
		cursors:    newCursorStore(),
		static:     static,
		lastEvent:  make(map[string]contentEvent),
		wsMsgRate:  DefaultWSMsgRate,
		wsMsgBurst: DefaultWSMsgBurst,
		passFails:  &passFailTracker{m: make(map[string]*passFailSlot)},
	}
	maxWSConns, maxWSConnsPerIP := DefaultMaxWSConns, DefaultMaxWSConnsPerIP
	if len(opts) > 0 {
		h.files = opts[0].Files
		h.uploadPassword = opts[0].UploadPassword
		h.ipResolver = opts[0].IPResolver
		if opts[0].MaxWSConns > 0 {
			maxWSConns = opts[0].MaxWSConns
		}
		if opts[0].MaxWSConnsPerIP > 0 {
			maxWSConnsPerIP = opts[0].MaxWSConnsPerIP
		}
		if opts[0].WSMsgRate > 0 {
			h.wsMsgRate = opts[0].WSMsgRate
		}
		if opts[0].WSMsgBurst > 0 {
			h.wsMsgBurst = opts[0].WSMsgBurst
		}
	}
	if h.ipResolver == nil {
		// Trust no proxies: forwarded headers never affect limits.
		h.ipResolver, _ = middleware.NewIPResolver("")
	}
	h.wsConns = newWSConnLimiter(maxWSConns, maxWSConnsPerIP)
	return h
}

func (h *Handler) PingExpired(key string) {
	h.forgetEvent(key)
	h.broker.ping(key)
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/", h.handlePage)
	mux.HandleFunc("/api/clipboard", h.handleCreateClipboard)
	mux.HandleFunc("/api/clipboard/", h.handleClipboardAPI)
	// Static assets: long cache when URL is versioned (?v=…); revalidate
	// unversioned paths so deploys are not stuck on old JS/CSS.
	mux.Handle("/static/", staticCacheHeaders(http.FileServer(http.FS(h.static))))
	return mux
}

func staticCacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only versioned URLs (e.g. app.js?v=20260804u) get the long
		// immutable cache. Any other query string would otherwise pin a
		// stale asset for a year on the first request that carries one.
		if strings.HasPrefix(r.URL.RawQuery, "v=") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	if r.URL.Path == "/" {
		http.Redirect(w, r, "/"+randomKey(), http.StatusFound)
		return
	}

	key, err := keyFromPagePath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = key

	// HTML must always revalidate — it points at versioned JS/CSS.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, h.static, "static/index.html")
}

// handleCreateClipboard creates a room with a server-generated key (same body
// as PUT /api/clipboard/{key}; used by the CLI client and simple integrations).
func (h *Handler) handleCreateClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	h.handleSaveClipboard(w, r, randomKey())
}

func (h *Handler) handleClipboardAPI(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/clipboard/")
	if suffix == r.URL.Path || suffix == "" {
		writeError(w, http.StatusNotFound, "clipboard not found")
		return
	}

	// /api/clipboard/{key}/ws
	if strings.HasSuffix(suffix, "/ws") {
		key := strings.TrimSuffix(suffix, "/ws")
		if key == "" || strings.Contains(key, "/") {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		if _, err := model.ValidateKey(key); err != nil {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		h.handleWebSocket(w, r, key)
		return
	}

	// /api/clipboard/{key}/files[/{id}]
	if idx := strings.Index(suffix, "/files"); idx >= 0 {
		keyPart := suffix[:idx]
		rest := suffix[idx+len("/files"):]
		if keyPart == "" || strings.Contains(keyPart, "/") {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		key, err := model.ValidateKey(keyPart)
		if err != nil {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		h.handleFilesAPI(w, r, key, rest)
		return
	}

	// /api/clipboard/{key}/settings
	if strings.HasSuffix(suffix, "/settings") {
		keyPart := strings.TrimSuffix(suffix, "/settings")
		if keyPart == "" || strings.Contains(keyPart, "/") {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		key, err := model.ValidateKey(keyPart)
		if err != nil {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		h.handleRoomSettings(w, r, key)
		return
	}

	// /api/clipboard/{key}/password
	if strings.HasSuffix(suffix, "/password") {
		keyPart := strings.TrimSuffix(suffix, "/password")
		if keyPart == "" || strings.Contains(keyPart, "/") {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		key, err := model.ValidateKey(keyPart)
		if err != nil {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		h.handleEditPassword(w, r, key)
		return
	}

	// /api/clipboard/{key}/history — server-side version trail (shared browsers).
	if strings.HasSuffix(suffix, "/history") {
		keyPart := strings.TrimSuffix(suffix, "/history")
		if keyPart == "" || strings.Contains(keyPart, "/") {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		key, err := model.ValidateKey(keyPart)
		if err != nil {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		h.handleHistory(w, r, key)
		return
	}

	if strings.Contains(suffix, "/") {
		writeError(w, http.StatusNotFound, "clipboard not found")
		return
	}

	key, err := model.ValidateKey(suffix)
	if err != nil {
		writeError(w, http.StatusNotFound, "clipboard not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetClipboard(w, r, key)
	case http.MethodPut:
		h.handleSaveClipboard(w, r, key)
	case http.MethodDelete:
		h.handleDeleteClipboard(w, r, key)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (h *Handler) handleGetClipboard(w http.ResponseWriter, r *http.Request, key string) {
	item, ok := h.store.Get(key)
	if !ok {
		writeJSON(w, http.StatusOK, model.ClipboardResponse{
			Key:        key,
			Content:    "",
			TTLSeconds: int64(defaultTTL.Seconds()),
			Version:    model.VersionNotExists,
			Generation: item.Generation,
			Exists:     false,
		})
		return
	}

	// View-protected rooms withhold content until the room password is
	// presented (X-Goclip-Password header or ?password= query).
	if h.viewProtected(key) {
		if !h.allowPasswordAttempt(r, key) {
			writeError(w, http.StatusTooManyRequests, "too many password attempts")
			return
		}
		pw := roomPasswordFromRequest(r)
		if !h.store.PasswordOK(key, pw) {
			h.recordPasswordFailure(r, key)
			msg := "view password required"
			if pw != "" {
				msg = "invalid view password"
			}
			writeJSON(w, http.StatusUnauthorized, struct {
				Error         string `json:"error"`
				PasswordScope string `json:"passwordScope"`
			}{msg, model.PasswordScopeView})
			return
		}
		h.recordPasswordSuccess(r, key)
	}

	writeJSON(w, http.StatusOK, model.ResponseFromClipboard(key, item, true))
}

func (h *Handler) handleSaveClipboard(w http.ResponseWriter, r *http.Request, key string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	var req model.SaveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !utf8.ValidString(req.Content) {
		writeError(w, http.StatusBadRequest, "content must be valid UTF-8")
		return
	}

	ttl, err := model.TTLFromSeconds(req.TTLSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Full document replace (LWW at document level); rebuilds CRDT chain.
	// baseVersion > 0 makes it conditional so stale offline clients get a
	// 409 with current state to merge against instead of clobbering peers.
	// Password is verified under the store lock (see store.Auth) so a
	// concurrent lock/rotate cannot race past a handler-only check.
	// SetPassword claim-locks an unlocked room under that same write so CLI
	// "push -password" never leaves content unlocked between two requests.
	clientID := sanitizeClientID(req.ClientID)
	if !h.allowPasswordAttempt(r, key) {
		writeError(w, http.StatusTooManyRequests, "too many password attempts")
		return
	}
	auth := store.Auth{Password: req.Password}
	if req.SetPassword {
		if pw := strings.TrimSpace(req.Password); pw != "" {
			auth.ClaimPassword = pw
			auth.ClaimScope = strings.TrimSpace(req.PasswordScope)
		}
	}
	item, err := h.store.SaveWithBase(key, req.Content, ttl, clientID, req.BaseVersion, auth)
	if errors.Is(err, store.ErrPasswordMismatch) {
		h.recordPasswordFailure(r, key)
		writeError(w, http.StatusForbidden, "edit password required")
		return
	}
	if errors.Is(err, store.ErrVersionConflict) {
		cur, exists := h.store.Get(key)
		writeJSON(w, http.StatusConflict, struct {
			model.ClipboardResponse
			Error string `json:"error"`
		}{model.ResponseFromClipboard(key, cur, exists), "version conflict"})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordPasswordSuccess(r, key)
	resp := model.ResponseFromClipboard(key, item, true)
	h.noteFullState(key, item.Version, item.Generation, item.UpdatedBy, item.Content)
	h.broker.ping(key)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteClipboard(w http.ResponseWriter, r *http.Request, key string) {
	if !h.allowPasswordAttempt(r, key) {
		writeError(w, http.StatusTooManyRequests, "too many password attempts")
		return
	}
	pw := roomPasswordFromRequest(r)
	if err := h.store.DeleteAuth(key, store.Auth{Password: pw}); err != nil {
		if errors.Is(err, store.ErrPasswordMismatch) {
			h.recordPasswordFailure(r, key)
			writeError(w, http.StatusForbidden, "edit password required")
			return
		}
		writeStoreError(w, err)
		return
	}
	h.recordPasswordSuccess(r, key)
	if h.files != nil {
		h.files.DeleteRoom(key)
	}
	// Subscribers will fetch the authoritative empty state after the broker
	// notification. Do not retain the deleted room's content in lastEvent.
	h.forgetEvent(key)
	h.broker.ping(key)
	w.WriteHeader(http.StatusNoContent)
}

// handleHistory lists (GET), force-captures (POST), or clears (DELETE) the
// room's version history. Any password-protected room requires the room
// password to read history (snapshots retain prior content, including text
// later deleted from the editor). Manual capture and clear need edit rights
// (password when locked). Prefer X-Goclip-Password; ?password= is accepted
// for convenience but may appear in access logs.
func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request, key string) {
	auth := store.Auth{Password: roomPasswordFromRequest(r)}
	switch r.Method {
	case http.MethodGet:
		if !h.allowPasswordAttempt(r, key) {
			writeError(w, http.StatusTooManyRequests, "too many password attempts")
			return
		}
		hist, ok, err := h.store.History(key, auth)
		if errors.Is(err, store.ErrPasswordMismatch) {
			h.recordPasswordFailure(r, key)
			msg := "password required"
			if auth.Password != "" {
				msg = "invalid password"
			}
			writeJSON(w, http.StatusUnauthorized, struct {
				Error         string `json:"error"`
				PasswordScope string `json:"passwordScope"`
			}{msg, model.PasswordScopeView})
			return
		}
		if err != nil {
			writeStoreError(w, err)
			return
		}
		h.recordPasswordSuccess(r, key)
		if !ok {
			// Empty room: return an empty trail rather than 404 so the UI
			// can open history before the first edit lands.
			writeJSON(w, http.StatusOK, struct {
				Snapshots []model.HistoryEntry `json:"snapshots"`
			}{Snapshots: []model.HistoryEntry{}})
			return
		}
		if hist == nil {
			hist = []model.HistoryEntry{}
		}
		writeJSON(w, http.StatusOK, struct {
			Snapshots []model.HistoryEntry `json:"snapshots"`
		}{Snapshots: hist})
	case http.MethodPost:
		if !h.allowPasswordAttempt(r, key) {
			writeError(w, http.StatusTooManyRequests, "too many password attempts")
			return
		}
		hist, err := h.store.CaptureHistory(key, auth)
		if errors.Is(err, store.ErrPasswordMismatch) {
			h.recordPasswordFailure(r, key)
			writeJSON(w, http.StatusForbidden, struct {
				Error         string `json:"error"`
				PasswordScope string `json:"passwordScope"`
			}{"edit password required", model.PasswordScopeView})
			return
		}
		if err != nil {
			if errors.Is(err, store.ErrRoomNotFound) {
				writeError(w, http.StatusNotFound, "clipboard not found")
				return
			}
			if errors.Is(err, store.ErrMemoryLimit) || errors.Is(err, store.ErrTooManyRooms) {
				writeError(w, http.StatusInsufficientStorage, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "capture failed")
			return
		}
		h.recordPasswordSuccess(r, key)
		if hist == nil {
			hist = []model.HistoryEntry{}
		}
		writeJSON(w, http.StatusOK, struct {
			Snapshots []model.HistoryEntry `json:"snapshots"`
		}{Snapshots: hist})
	case http.MethodDelete:
		if !h.allowPasswordAttempt(r, key) {
			writeError(w, http.StatusTooManyRequests, "too many password attempts")
			return
		}
		if err := h.store.ClearHistory(key, auth); err != nil {
			if errors.Is(err, store.ErrPasswordMismatch) {
				h.recordPasswordFailure(r, key)
				writeJSON(w, http.StatusForbidden, struct {
					Error         string `json:"error"`
					PasswordScope string `json:"passwordScope"`
				}{"edit password required", model.PasswordScopeView})
				return
			}
			if errors.Is(err, store.ErrRoomNotFound) {
				// Nothing to clear — treat as success for the UI.
				writeJSON(w, http.StatusOK, struct {
					Snapshots []model.HistoryEntry `json:"snapshots"`
				}{Snapshots: []model.HistoryEntry{}})
				return
			}
			writeError(w, http.StatusInternalServerError, "clear failed")
			return
		}
		h.recordPasswordSuccess(r, key)
		writeJSON(w, http.StatusOK, struct {
			Snapshots []model.HistoryEntry `json:"snapshots"`
		}{Snapshots: []model.HistoryEntry{}})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
	}
}

// handleEditPassword sets, rotates or clears a room password.
// GET reports whether the room is locked and what the password gates; PUT
// applies the change. The password itself is never returned, only its
// presence and scope — link holders cannot learn it from any response.
func (h *Handler) handleEditPassword(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := h.store.Peek(key); !ok {
			writeError(w, http.StatusNotFound, "clipboard not found")
			return
		}
		set, scope := h.store.PasswordInfo(key)
		writeJSON(w, http.StatusOK, struct {
			PasswordSet bool   `json:"passwordSet"`
			Scope       string `json:"scope,omitempty"`
		}{set, scope})
	case http.MethodPut:
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var req model.EditPasswordRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := requireSingleJSONValue(decoder); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		next := strings.TrimSpace(req.Password)
		if len(next) > 64 {
			writeError(w, http.StatusBadRequest, "password too long")
			return
		}
		scope := strings.TrimSpace(req.Scope)
		if next != "" && scope != "" && scope != model.PasswordScopeEdit && scope != model.PasswordScopeView {
			writeError(w, http.StatusBadRequest, "invalid password scope")
			return
		}
		// Locking a room that does not exist yet (fresh page, nothing typed)
		// must still work: create an empty room so the password has somewhere
		// to live. Later edits keep the lock because the password is stored
		// on the room. An empty unlock request on a missing room is a no-op
		// success instead (nothing to unlock).
		if _, ok := h.store.Peek(key); !ok {
			if next == "" {
				set, sc := h.store.PasswordInfo(key)
				writeJSON(w, http.StatusOK, struct {
					PasswordSet bool   `json:"passwordSet"`
					Scope       string `json:"scope,omitempty"`
				}{set, sc})
				return
			}
			if _, err := h.store.SaveWithBase(key, "", defaultTTL, "", 0, store.Auth{}); err != nil {
				// Creating an empty room to hold a password does not need auth.
				writeStoreError(w, err)
				return
			}
		}
		// Rotating/clearing requires the current password — budget wrong
		// guesses the same way as GET/save so this endpoint cannot be used
		// to brute-force past passFailTracker.
		wasLocked := h.store.HasPassword(key)
		if wasLocked {
			if !h.allowPasswordAttempt(r, key) {
				writeError(w, http.StatusTooManyRequests, "too many password attempts")
				return
			}
		}
		if err := h.store.SetPassword(key, req.CurrentPassword, next, scope); err != nil {
			if errors.Is(err, store.ErrPasswordMismatch) {
				h.recordPasswordFailure(r, key)
				writeError(w, http.StatusForbidden, "invalid edit password")
				return
			}
			writeStoreError(w, err)
			return
		}
		if wasLocked {
			h.recordPasswordSuccess(r, key)
		}
		h.broker.ping(key)
		set, sc := h.store.PasswordInfo(key)
		writeJSON(w, http.StatusOK, struct {
			PasswordSet bool   `json:"passwordSet"`
			Scope       string `json:"scope,omitempty"`
		}{set, sc})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

// --- Room password read gating ---------------------------------------------

// viewProtected reports whether reading the room requires its password
// (scope "view").
func (h *Handler) viewProtected(key string) bool {
	set, scope := h.store.PasswordInfo(key)
	return set && scope == model.PasswordScopeView
}

// roomPasswordFromRequest extracts the room password from a request.
// Prefer X-Goclip-Password (not logged by typical reverse proxies); the
// ?password= query form is accepted for CLI/curl convenience but may appear
// in access logs and Referer headers.
func roomPasswordFromRequest(r *http.Request) string {
	if p := strings.TrimSpace(r.Header.Get("X-Goclip-Password")); p != "" {
		return p
	}
	return strings.TrimSpace(r.URL.Query().Get("password"))
}

// --- WebSocket realtime ------------------------------------------------------

type wsOutbound struct {
	Type string `json:"type"`
	// Ack fields (type=ack): echo of the client's batch seq, plus the
	// resulting version/expiry, or an error when the batch was rejected.
	Seq    int64  `json:"seq,omitempty"`
	ErrMsg string `json:"error,omitempty"`
	// Content fields (type=state | ops | legacy update)
	Key        string      `json:"key,omitempty"`
	Content    string      `json:"content,omitempty"`
	TTLSeconds int64       `json:"ttlSeconds,omitempty"`
	ExpiresAt  string      `json:"expiresAt,omitempty"`
	Version    int64       `json:"version,omitempty"`
	Generation int64       `json:"generation,omitempty"`
	Exists     *bool       `json:"exists,omitempty"`
	UpdatedBy  string      `json:"updatedBy,omitempty"`
	Items      []crdt.Item `json:"items,omitempty"`
	Ops        []crdt.Op   `json:"ops,omitempty"`
	// Cursor snapshot (type=cursor)
	Cursors []model.CursorInfo `json:"cursors,omitempty"`
	// File list snapshot (type=files) — metadata only, not blobs
	Files             []model.FileInfo `json:"files,omitempty"`
	FilesVersion      int64            `json:"filesVersion,omitempty"`
	FileUploadEnabled *bool            `json:"fileUploadEnabled,omitempty"`
	// EditPasswordSet reports whether the room is locked (never the password).
	EditPasswordSet *bool `json:"editPasswordSet,omitempty"`
	// PasswordScope reports what the room password gates ("edit" | "view").
	PasswordScope string `json:"passwordScope,omitempty"`
	// PasswordRequired marks a state frame for a view-protected room sent to
	// an unauthenticated session: content/items are withheld until the
	// client presents the room password via an "auth" message.
	PasswordRequired *bool `json:"passwordRequired,omitempty"`
}

type wsInbound struct {
	Type             string    `json:"type"`
	CursorPos        int       `json:"cursorPos"`
	SelectionEnd     int       `json:"selectionEnd"`
	AfterID          string    `json:"afterId,omitempty"`
	SelectionAfterID string    `json:"selectionAfterId,omitempty"`
	Color            string    `json:"color"`
	Ops              []crdt.Op `json:"ops,omitempty"`
	TTLSeconds       int64     `json:"ttlSeconds,omitempty"`
	// Password authenticates writes to rooms locked with an edit password.
	Password string `json:"password,omitempty"`
	// Seq is a client-chosen batch id echoed back in the ack so senders can
	// track unacked ops across flaky connections.
	Seq int64 `json:"seq,omitempty"`
}

// wsSession tracks per-connection state for view-protected rooms: until the
// client presents the room password via an "auth" message, content, files
// and cursors are withheld. Auth is bound to PasswordCredential so rotating
// or clearing the password invalidates the session without closing the
// socket. All fields are guarded by mu because the read loop writes them and
// the main select loop reads them.
type wsSession struct {
	mu       sync.Mutex
	authed   bool
	authCred string // store.PasswordCredential at successful auth time
}

// markAuthed records that this session has proven the room password.
// cred must be the current PasswordCredential (non-empty).
func (s *wsSession) markAuthed(cred string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authed = true
	s.authCred = cred
}

// isAuthed reports whether a prior auth still matches the room's current
// password credential. A rotated password yields a new credential and
// returns false; an unlocked room (empty cred) also returns false so the
// next lock requires a fresh auth.
func (s *wsSession) isAuthed(currentCred string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authed || currentCred == "" || s.authCred == "" {
		return false
	}
	return secureEqual(s.authCred, currentCred)
}

// sessionAuthed is the handler-side check used on every gated WS path.
func (h *Handler) sessionAuthed(sess *wsSession, key string) bool {
	return sess.isAuthed(h.store.PasswordCredential(key))
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request, key string) {
	clientID := sanitizeClientID(r.URL.Query().Get("clientId"))

	// DoS guard: cap concurrent sockets globally and per IP. The check runs
	// before the upgrade so a flood is rejected with a cheap HTTP response
	// instead of consuming per-connection goroutines and buffers.
	clientIP := h.ipResolver.ClientIP(r)
	if !h.wsConns.acquire(clientIP) {
		writeError(w, http.StatusServiceUnavailable, "too many connections")
		return
	}
	defer h.wsConns.release(clientIP)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if h.logger != nil {
			h.logger.Debug("websocket upgrade failed", "error", err, "key", key)
		}
		return
	}
	defer conn.Close()

	conn.SetReadLimit(wsMaxMessage)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	ch, unsub := h.broker.subscribe(key)
	defer unsub()
	if clientID != "" {
		defer func() {
			h.cursors.remove(key, clientID)
			h.broker.ping(key)
		}()
	}

	// Serialized writes: one goroutine owns all WriteMessage calls.
	send := make(chan any, 16)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		h.wsWriteLoop(conn, send)
		// Force read loop to exit if writes fail (half-open socket).
		_ = conn.Close()
	}()

	// Initial snapshot.
	var (
		lastVersion    int64 = -1
		lastGeneration int64 = -1
		lastExists     bool
		lastFilesRev   int64 = -1
	)
	sess := &wsSession{}
	// Fired by the read loop once an "auth" message unlocks a view-protected
	// room; the main loop then pushes the real room state.
	authedCh := make(chan struct{}, 1)
	lastAuthed := h.sessionAuthed(sess, key)
	lastVersion, lastGeneration, lastExists, lastFilesRev = h.enqueueRoomState(send, key, clientID, lastVersion, lastGeneration, lastExists, lastFilesRev, true, lastAuthed)

	// Per-connection inbound message budget (flood guard); see tokenBucket.
	bucket := newTokenBucket(h.wsMsgRate, float64(h.wsMsgBurst))

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		// clientIP keys the password fail budget (same as HTTP paths).
		h.wsReadLoop(conn, send, key, clientID, clientIP, bucket, sess, authedCh)
	}()
	defer func() {
		// Stop both producers before closing send. The read loop may still be
		// finishing an op batch when the writer or request context exits.
		_ = conn.Close()
		<-readDone
		close(send)
		<-writeDone
	}()

	pingTicker := time.NewTicker(wsPingPeriod)
	defer pingTicker.Stop()
	presenceTicker := time.NewTicker(wsPresencePeriod)
	defer presenceTicker.Stop()

	// pushState re-evaluates session auth. Password rotate/clear does not bump
	// document version, so we force a full state when auth flips (re-lock or
	// unlock) even though contentChanged would otherwise be false.
	pushState := func(force bool) {
		authed := h.sessionAuthed(sess, key)
		if authed != lastAuthed {
			force = true
			lastAuthed = authed
		}
		lastVersion, lastGeneration, lastExists, lastFilesRev = h.enqueueRoomState(send, key, clientID, lastVersion, lastGeneration, lastExists, lastFilesRev, force, authed)
	}

	for {
		select {
		case <-readDone:
			return
		case <-writeDone:
			return
		case <-r.Context().Done():
			return
		case <-authedCh:
			// Session unlocked a view-protected room: push the full state.
			pushState(true)
		case <-ch:
			pushState(false)
		case <-pingTicker.C:
			select {
			case send <- wsControl{ping: true}:
			default:
			}
		case <-presenceTicker.C:
			// Push pruned presence so idle/left peers drop without waiting for edits.
			// Also re-check file list in case TTL expiry dropped files without a write path.
			// Re-check auth: a password rotate may have invalidated the session.
			pushState(false)
		}
	}
}

type wsControl struct {
	ping bool
}

func (h *Handler) wsWriteLoop(conn *websocket.Conn, send <-chan any) {
	for msg := range send {
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		switch m := msg.(type) {
		case wsControl:
			if m.ping {
				if err := conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
					return
				}
			}
		default:
			if err := conn.WriteJSON(m); err != nil {
				return
			}
		}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func (h *Handler) wsReadLoop(conn *websocket.Conn, send chan<- any, key, clientID, clientIP string, bucket *tokenBucket, sess *wsSession, authedCh chan<- struct{}) {
	for {
		// Exceeding the per-connection budget cuts the socket; the client's
		// reconnect path resyncs from the authoritative state snapshot, so
		// this is self-healing for legit users and a hard stop for floods.
		if !bucket.allow() {
			if h.logger != nil {
				h.logger.Debug("ws message rate exceeded; closing connection", "key", key, "clientId", clientID)
			}
			return
		}
		var msg wsInbound
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "auth":
			// View-protected rooms: unlock this session once the room
			// password checks out; the main loop then pushes the real state.
			// Auth is bound to PasswordCredential so a later rotate/clear
			// invalidates this session without closing the socket.
			if h.viewProtected(key) {
				if !h.allowPasswordAttemptIP(clientIP, key) {
					h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: "too many password attempts"})
					continue
				}
				// AuthCredential validates and returns the credential in one
				// atomic step — PasswordOK + PasswordCredential in sequence
				// would let a concurrent rotation stamp this session with a
				// credential it never presented.
				if cred, ok := h.store.AuthCredential(key, msg.Password); ok && cred != "" {
					h.recordPasswordSuccessIP(clientIP, key)
					sess.markAuthed(cred)
					select {
					case authedCh <- struct{}{}:
					default:
					}
				} else {
					h.recordPasswordFailureIP(clientIP, key)
					h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: "invalid view password"})
				}
			}
		case "cursor":
			if clientID == "" {
				continue
			}
			if msg.CursorPos < 0 || msg.SelectionEnd < 0 {
				continue
			}
			// View-protected rooms: presence is part of the sealed surface —
			// do not accept caret updates until the session is authenticated.
			if h.viewProtected(key) && !h.sessionAuthed(sess, key) {
				continue
			}
			h.cursors.update(key, model.CursorInfo{
				ClientID:         clientID,
				CursorPos:        msg.CursorPos,
				SelectionEnd:     msg.SelectionEnd,
				AfterID:          sanitizeCursorAnchor(msg.AfterID),
				SelectionAfterID: sanitizeCursorAnchor(msg.SelectionAfterID),
				Color:            sanitizeColor(msg.Color),
				Timestamp:        time.Now().UnixMilli(),
			})
			h.broker.ping(key)
		case "ops":
			if clientID == "" {
				continue
			}
			// View-scope: accept password on the first ops batch as implicit
			// auth (same as an "auth" message) so non-SPA clients that only
			// attach password to ops still unlock. Session-bound credential
			// is re-checked under the store lock in ApplyOps.
			if h.viewProtected(key) && !h.sessionAuthed(sess, key) {
				if !h.allowPasswordAttemptIP(clientIP, key) {
					h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: "too many password attempts"})
					continue
				}
				if cred, ok := h.store.AuthCredential(key, msg.Password); ok && cred != "" {
					h.recordPasswordSuccessIP(clientIP, key)
					sess.markAuthed(cred)
					select {
					case authedCh <- struct{}{}:
					default:
					}
				} else {
					h.recordPasswordFailureIP(clientIP, key)
					h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: "view password required"})
					continue
				}
			}
			h.handleWSOps(send, key, clientID, clientIP, msg, sess)
		case "sync":
			// Client detected a version gap (dropped/coalesced updates) and
			// wants an authoritative snapshot.
			cur, exists := h.store.Peek(key)
			select {
			case send <- h.stateMessage(key, cur, exists, h.sessionAuthed(sess, key)):
			default:
			}
		case "pong", "ping":
			// ignore app-level heartbeats; protocol pings handled by gorilla
		default:
			// ignore unknown
		}
	}
}

func (h *Handler) handleWSOps(send chan<- any, key, clientID, clientIP string, msg wsInbound, sess *wsSession) {
	// Password is re-checked under the store lock via Auth. Cred binds a
	// previously authenticated WS session; Password covers edit-scope batches
	// and first-time view unlock via ops.password.
	var ttl time.Duration
	if msg.TTLSeconds > 0 {
		d, err := model.TTLFromSeconds(msg.TTLSeconds)
		if err != nil {
			h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: err.Error()})
			return
		}
		ttl = d
	}
	// Empty batch + ttl is a TTL update/refresh (no full-content PUT needed).
	if len(msg.Ops) == 0 && ttl <= 0 {
		return
	}
	auth := store.Auth{Password: msg.Password}
	if sess != nil {
		// Bind the session credential when still valid so rotate invalidates
		// under the same lock as ApplyOps.
		if cred := h.store.PasswordCredential(key); sess.isAuthed(cred) {
			auth.Cred = cred
		}
	}
	// Edit-scope (and any path that still presents a password without a
	// bound session cred) shares the HTTP fail budget so WS is not a
	// side-channel for online guessing.
	needsPassTry := h.store.HasPassword(key) && auth.Cred == ""
	if needsPassTry && !h.allowPasswordAttemptIP(clientIP, key) {
		h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: "too many password attempts"})
		return
	}
	item, changed, err := h.store.ApplyOps(key, msg.Ops, ttl, clientID, auth)
	if err != nil {
		if h.logger != nil {
			h.logger.Debug("ws ops rejected", "key", key, "clientId", clientID, "error", err)
		}
		errMsg := err.Error()
		if errors.Is(err, store.ErrPasswordMismatch) {
			if needsPassTry {
				h.recordPasswordFailureIP(clientIP, key)
			}
			if h.viewProtected(key) {
				errMsg = "view password required"
			} else {
				errMsg = "edit password required"
			}
		}
		// Error ack first so the sender drops the bad batch, then a snapshot
		// so its document converges back to server state.
		h.enqueueAck(send, wsOutbound{Type: "ack", Seq: msg.Seq, ErrMsg: errMsg})
		// Password rejections must not force-drop local ops with a full state
		// that wipes unacked typing; only capacity/validation errors need a
		// convergent snapshot. The client re-auths and re-flushes pending ops.
		if !errors.Is(err, store.ErrPasswordMismatch) {
			cur, exists := h.store.Peek(key)
			select {
			case send <- h.stateMessage(key, cur, exists, true):
			default:
			}
		}
		return
	}
	if needsPassTry {
		h.recordPasswordSuccessIP(clientIP, key)
	}
	// Direct ack: the broadcast path can coalesce or skip (idempotent
	// re-applies do not bump the version), so the sender needs an explicit
	// confirmation to release its unacked buffer.
	existsTrue := true
	h.enqueueAck(send, wsOutbound{
		Type:       "ack",
		Seq:        msg.Seq,
		Version:    item.Version,
		Generation: item.Generation,
		TTLSeconds: int64(item.TTL.Seconds()),
		ExpiresAt:  item.ExpiresAt.UTC().Format(time.RFC3339),
		Exists:     &existsTrue,
	})
	if changed {
		h.noteOps(key, item.Version, item.Generation, item.UpdatedBy, msg.Ops, item.Content)
	} else {
		// TTL-only change (version bumped) or idempotent re-apply: subscribers
		// that need it get a full state snapshot instead of an ops diff.
		h.noteFullState(key, item.Version, item.Generation, item.UpdatedBy, item.Content)
	}
	h.broker.ping(key)
}

func (h *Handler) enqueueAck(send chan<- any, ack wsOutbound) {
	select {
	case send <- ack:
	default:
		// Send buffer full: client is stalled; its ack-timeout watchdog will
		// reconnect and resync via the initial state snapshot.
	}
}

func (h *Handler) noteOps(key string, version, generation int64, updatedBy string, ops []crdt.Op, content string) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	cp := make([]crdt.Op, len(ops))
	copy(cp, ops)
	h.lastEvent[key] = contentEvent{
		version:    version,
		generation: generation,
		updatedBy:  updatedBy,
		ops:        cp,
		content:    content,
		full:       false,
	}
}

func (h *Handler) noteFullState(key string, version, generation int64, updatedBy, content string) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	h.lastEvent[key] = contentEvent{
		version:    version,
		generation: generation,
		updatedBy:  updatedBy,
		content:    content,
		full:       true,
	}
}

func (h *Handler) forgetEvent(key string) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	delete(h.lastEvent, key)
}

func (h *Handler) takeEvent(key string, version, generation int64) (contentEvent, bool) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	ev, ok := h.lastEvent[key]
	if !ok || ev.version != version || ev.generation != generation {
		return contentEvent{}, false
	}
	return ev, true
}

func (h *Handler) enqueueRoomState(send chan<- any, key, clientID string, lastVersion, lastGeneration int64, lastExists bool, lastFilesRev int64, forceContent bool, authed bool) (int64, int64, bool, int64) {
	// Peek (no deep clone): this runs per connection every presence tick
	// (~5s) even when nothing changed; cloning the whole document each time
	// would be pure waste on large rooms.
	item, exists := h.store.Peek(key)
	version := model.VersionNotExists
	generation := item.Generation
	if exists {
		version = item.Version
	} else {
		// Lazy expiry can remove a room without invoking the cleanup callback.
		// Once the authoritative state is gone, retaining its content event is
		// both unnecessary and an avoidable memory leak.
		h.forgetEvent(key)
	}

	// View-protected rooms withhold content (and presence) until the session
	// presents the room password via an "auth" message.
	viewLocked := h.viewProtected(key) && !authed

	contentChanged := forceContent || exists != lastExists || version != lastVersion || generation != lastGeneration
	if contentChanged {
		var msg wsOutbound
		if forceContent || !exists {
			msg = h.stateMessage(key, item, exists, authed)
		} else if ev, ok := h.takeEvent(key, version, generation); ok && !ev.full && len(ev.ops) > 0 && generation == lastGeneration && version == lastVersion+1 && lastExists && !viewLocked {
			// Compact ops diff is only valid when this client saw the
			// immediately preceding version; any gap (coalesced wakeups,
			// earlier dropped send) requires a full snapshot, otherwise the
			// client silently misses the intermediate ops and diverges.
			msg = wsOutbound{
				Type:       "ops",
				Key:        key,
				Version:    version,
				Generation: generation,
				UpdatedBy:  ev.updatedBy,
				Ops:        ev.ops,
				Content:    ev.content,
			}
			existsTrue := true
			msg.Exists = &existsTrue
			msg.TTLSeconds = int64(item.TTL.Seconds())
			msg.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
		} else {
			msg = h.stateMessage(key, item, exists, authed)
		}
		select {
		case send <- msg:
			lastVersion = version
			lastGeneration = generation
			lastExists = exists
		default:
			// Client too slow — drop, but keep lastVersion stale so the next
			// broker ping / presence tick actually retries the update.
		}
	}

	if viewLocked {
		// No file list, no cursors: both leak room content.
		return lastVersion, lastGeneration, lastExists, lastFilesRev
	}

	// File list sync (metadata only). forceContent also pushes initial files snapshot.
	if h.files != nil {
		files, filesRev, uploadOn := h.files.ListWithSettings(key)
		if forceContent || filesRev != lastFilesRev {
			select {
			case send <- h.filesMessage(key, files, filesRev, uploadOn):
				lastFilesRev = filesRev
			default:
				// Drop; next presence/ping will retry while lastFilesRev stays stale.
			}
		}
	}

	select {
	case send <- h.cursorMessage(key, clientID):
	default:
	}
	return lastVersion, lastGeneration, lastExists, lastFilesRev
}

func (h *Handler) filesMessage(key string, files []model.FileInfo, rev int64, uploadEnabled bool) wsOutbound {
	if files == nil {
		files = []model.FileInfo{}
	}
	enabled := uploadEnabled
	return wsOutbound{
		Type:              "files",
		Key:               key,
		Files:             files,
		FilesVersion:      rev,
		FileUploadEnabled: &enabled,
	}
}

func (h *Handler) stateMessage(key string, item model.Clipboard, exists bool, authed bool) wsOutbound {
	msg := wsOutbound{Type: "state", Key: key, Generation: item.Generation}
	locked := exists && item.RoomPasswordSet()
	msg.EditPasswordSet = &locked
	scope := model.PasswordScopeOf(item.PasswordScope)
	if locked {
		msg.PasswordScope = scope
	}
	if locked && !authed && scope == model.PasswordScopeView {
		// View-protected room, session not authenticated yet: withhold all
		// content until the client presents the room password.
		required := true
		existsTrue := true
		msg.PasswordRequired = &required
		msg.Exists = &existsTrue
		msg.Content = ""
		msg.TTLSeconds = int64(defaultTTL.Seconds())
		msg.Version = item.Version
		msg.Generation = item.Generation
		msg.Items = []crdt.Item{}
		return msg
	}
	if exists {
		resp := model.ResponseFromClipboard(key, item, true)
		msg.Content = resp.Content
		msg.TTLSeconds = resp.TTLSeconds
		msg.ExpiresAt = resp.ExpiresAt
		msg.Version = resp.Version
		msg.Generation = resp.Generation
		existsTrue := true
		msg.Exists = &existsTrue
		msg.UpdatedBy = resp.UpdatedBy
		if item.Doc != nil {
			msg.Items = item.Doc.Items()
		} else {
			msg.Items = []crdt.Item{}
		}
	} else {
		msg.Content = ""
		msg.TTLSeconds = int64(defaultTTL.Seconds())
		msg.Version = model.VersionNotExists
		existsFalse := false
		msg.Exists = &existsFalse
		msg.Items = []crdt.Item{}
	}
	return msg
}

func (h *Handler) cursorMessage(key, clientID string) wsOutbound {
	return wsOutbound{
		Type:    "cursor",
		Cursors: h.cursors.getCursors(key, clientID),
	}
}

type cursorStore struct {
	mu   sync.Mutex
	data map[string]map[string]model.CursorInfo
}

func newCursorStore() *cursorStore {
	return &cursorStore{
		data: make(map[string]map[string]model.CursorInfo),
	}
}

func (c *cursorStore) update(key string, cursor model.CursorInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data[key] == nil {
		c.data[key] = make(map[string]model.CursorInfo)
	}
	c.data[key][cursor.ClientID] = cursor
}

func (c *cursorStore) remove(key, clientID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data[key] != nil {
		delete(c.data[key], clientID)
		if len(c.data[key]) == 0 {
			delete(c.data, key)
		}
	}
}

func (c *cursorStore) getCursors(key, excludeClientID string) []model.CursorInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	var cursors []model.CursorInfo
	if c.data[key] != nil {
		for id, cursor := range c.data[key] {
			if now-cursor.Timestamp > cursorStaleMs {
				delete(c.data[key], id)
				continue
			}
			if cursor.ClientID == excludeClientID {
				continue
			}
			cursors = append(cursors, cursor)
		}
		if len(c.data[key]) == 0 {
			delete(c.data, key)
		}
	}
	if cursors == nil {
		cursors = []model.CursorInfo{}
	}
	// Stable order so clients don't reshuffle overlapping carets on each broadcast.
	sort.Slice(cursors, func(i, j int) bool {
		return cursors[i].ClientID < cursors[j].ClientID
	})
	return cursors
}

type broker struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func newBroker() *broker {
	return &broker{
		subs: make(map[string]map[chan struct{}]struct{}),
	}
}

func (b *broker) subscribe(key string) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subs[key] == nil {
		b.subs[key] = make(map[chan struct{}]struct{})
	}
	b.subs[key][ch] = struct{}{}

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[key], ch)
		if len(b.subs[key]) == 0 {
			delete(b.subs, key)
		}
	}
}

func (b *broker) ping(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// secureEqual compares two secrets in constant time (length-checked).
func secureEqual(expected, provided string) bool {
	if expected == "" || provided == "" {
		return false
	}
	if len(expected) != len(provided) {
		// Burn comparable time to avoid a length-based timing oracle.
		_ = subtle.ConstantTimeCompare([]byte(expected), []byte(expected))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// wsConnLimiter caps concurrent WebSocket connections globally and per client
// IP. Connections are the expensive resource (two goroutines + buffers each),
// so floods must be stopped before the upgrade handshake.
type wsConnLimiter struct {
	mu        sync.Mutex
	global    int
	perIP     map[string]int
	maxGlobal int
	maxPerIP  int
}

func newWSConnLimiter(maxGlobal, maxPerIP int) *wsConnLimiter {
	return &wsConnLimiter{
		perIP:     make(map[string]int),
		maxGlobal: maxGlobal,
		maxPerIP:  maxPerIP,
	}
}

// acquire reserves one connection slot. Callers must release exactly once for
// every successful acquire.
func (l *wsConnLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.global >= l.maxGlobal {
		return false
	}
	if l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.global++
	l.perIP[ip]++
	return true
}

func (l *wsConnLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.global--
	if l.global < 0 {
		l.global = 0
	}
	if n := l.perIP[ip] - 1; n <= 0 {
		delete(l.perIP, ip)
	} else {
		l.perIP[ip] = n
	}
}

// tokenBucket is a per-connection inbound-message limiter. It is owned by the
// single wsReadLoop goroutine and must not be shared.
type tokenBucket struct {
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{tokens: burst, last: time.Now(), rate: rate, burst: burst}
}

// allow consumes one token if available. Tokens refill continuously at rate,
// capped at burst, so idle connections accumulate a short burst budget.
func (b *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func keyFromPagePath(path string) (string, error) {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/static/") || path == "/healthz" {
		return "", errors.New("reserved path")
	}
	key := strings.TrimPrefix(path, "/")
	if strings.Contains(key, "/") {
		return "", errors.New("key must be a single path segment")
	}
	return model.ValidateKey(key)
}

func requireSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func sanitizeClientID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			if b.Len() >= maxClientIDLen {
				break
			}
		}
	}
	return b.String()
}

func sanitizeColor(color string) string {
	color = strings.TrimSpace(color)
	if len(color) > maxColorLen {
		color = color[:maxColorLen]
	}
	if len(color) == 4 || len(color) == 7 {
		if color[0] == '#' {
			ok := true
			for i := 1; i < len(color); i++ {
				c := color[i]
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					ok = false
					break
				}
			}
			if ok {
				return strings.ToLower(color)
			}
		}
	}
	return "#61afef"
}

// CRDT item ids look like "site:clock". Empty means document start.
func sanitizeCursorAnchor(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) > 96 {
		id = id[:96]
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == ':' || c == '.' {
			continue
		}
		return ""
	}
	return id
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrContentTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, store.ErrMemoryLimit), errors.Is(err, store.ErrTooManyRooms):
		// 507: temporary capacity; clients can retry after other rooms expire.
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusInsufficientStorage, err.Error())
	case errors.Is(err, store.ErrRoomNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrPasswordMismatch):
		writeError(w, http.StatusForbidden, "edit password required")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// --- Per-room password attempt budget --------------------------------------
// Complements the global IP rate limiter: short shared passwords should not
// be online-bruteforceable just by spreading requests across endpoints.

const (
	passFailMax    = 12
	passFailWindow = time.Minute
	passFailBlock  = 30 * time.Second
)

type passFailSlot struct {
	n            int
	windowStart  time.Time
	blockedUntil time.Time
}

// allowPasswordAttempt reports whether this client IP may still try a password
// for the room. Missing tracker (tests constructing Handler by hand) allows.
func (h *Handler) allowPasswordAttempt(r *http.Request, key string) bool {
	return h.allowPasswordAttemptIP(h.clientIP(r), key)
}

// allowPasswordAttemptIP is the IP-keyed form used by both HTTP and WebSocket
// auth paths so WS cannot bypass the fail budget.
func (h *Handler) allowPasswordAttemptIP(ip, key string) bool {
	if h == nil || h.passFails == nil {
		return true
	}
	h.passFails.mu.Lock()
	defer h.passFails.mu.Unlock()
	slot := h.passFails.m[passFailKey(ip, key)]
	if slot == nil {
		return true
	}
	now := time.Now()
	if now.Before(slot.blockedUntil) {
		return false
	}
	return true
}

func (h *Handler) recordPasswordFailure(r *http.Request, key string) {
	h.recordPasswordFailureIP(h.clientIP(r), key)
}

func (h *Handler) recordPasswordFailureIP(ip, key string) {
	if h == nil {
		return
	}
	if h.passFails == nil {
		h.passFails = &passFailTracker{m: make(map[string]*passFailSlot)}
	}
	k := passFailKey(ip, key)
	h.passFails.mu.Lock()
	defer h.passFails.mu.Unlock()
	now := time.Now()
	slot := h.passFails.m[k]
	if slot == nil {
		slot = &passFailSlot{windowStart: now}
		h.passFails.m[k] = slot
	}
	if now.Sub(slot.windowStart) > passFailWindow {
		slot.n = 0
		slot.windowStart = now
	}
	slot.n++
	if slot.n >= passFailMax {
		slot.blockedUntil = now.Add(passFailBlock)
		slot.n = 0
		slot.windowStart = now
	}
}

func (h *Handler) recordPasswordSuccess(r *http.Request, key string) {
	h.recordPasswordSuccessIP(h.clientIP(r), key)
}

func (h *Handler) recordPasswordSuccessIP(ip, key string) {
	if h == nil || h.passFails == nil {
		return
	}
	k := passFailKey(ip, key)
	h.passFails.mu.Lock()
	defer h.passFails.mu.Unlock()
	delete(h.passFails.m, k)
}

func (h *Handler) clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if h.ipResolver != nil {
		return h.ipResolver.ClientIP(r)
	}
	return r.RemoteAddr
}

func passFailKey(ip, key string) string {
	return ip + "\x00" + key
}

type passFailTracker struct {
	mu sync.Mutex
	m  map[string]*passFailSlot
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func randomKey() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}
