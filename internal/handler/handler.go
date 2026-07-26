package handler

import (
	"crypto/rand"
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
	"goclipboard/internal/model"
	"goclipboard/internal/store"

	"github.com/gorilla/websocket"
)

const (
	defaultTTL      = time.Hour
	maxRequestBytes = 1 << 20
	// Offline if no cursor/heartbeat for this long (client heartbeats ~5s).
	cursorStaleMs  = 12_000
	maxClientIDLen = 64
	maxColorLen    = 32
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 60 * time.Second
	wsPingPeriod   = 25 * time.Second
	// How often to push a pruned cursor snapshot to each client.
	wsPresencePeriod = 5 * time.Second
	// Large enough for op batches and CRDT state snapshots (content still capped at 1MiB).
	wsMaxMessage = 256 << 10
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

	// lastContentEvent lets WS subscribers send compact ops when possible.
	eventMu sync.Mutex
	// key -> last content event (ops or full state flag)
	lastEvent map[string]contentEvent
}

type contentEvent struct {
	version   int64
	updatedBy string
	// if ops non-nil, prefer broadcasting ops; otherwise full state
	ops     []crdt.Op
	content string
	full    bool
}

// Options configures optional handler features (file uploads, etc.).
type Options struct {
	Files          *store.FileStore
	UploadPassword string
}

func New(sto *store.Store, static fs.FS, logger *slog.Logger, opts ...Options) *Handler {
	h := &Handler{
		store:     sto,
		logger:    logger,
		broker:    newBroker(),
		cursors:   newCursorStore(),
		static:    static,
		lastEvent: make(map[string]contentEvent),
	}
	if len(opts) > 0 {
		h.files = opts[0].Files
		h.uploadPassword = opts[0].UploadPassword
	}
	return h
}

func (h *Handler) PingExpired(key string) {
	h.broker.ping(key)
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/", h.handlePage)
	mux.HandleFunc("/api/clipboard/", h.handleClipboardAPI)
	// Static assets: allow long browser cache when URL is versioned (?v=...);
	// revalidate by default so unversioned requests still pick up deploys.
	mux.Handle("/static/", staticCacheHeaders(http.FileServer(http.FS(h.static))))
	return mux
}

func staticCacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			// Versioned URL (e.g. app.js?v=20260714b) — safe to cache long.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Unversioned — revalidate so deploys are not stuck on old JS/CSS.
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
		h.handleGetClipboard(w, key)
	case http.MethodPut:
		h.handleSaveClipboard(w, r, key)
	case http.MethodDelete:
		h.handleDeleteClipboard(w, key)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (h *Handler) handleGetClipboard(w http.ResponseWriter, key string) {
	item, ok := h.store.Get(key)
	if !ok {
		writeJSON(w, http.StatusOK, model.ClipboardResponse{
			Key:        key,
			Content:    "",
			TTLSeconds: int64(defaultTTL.Seconds()),
			Version:    model.VersionNotExists,
			Exists:     false,
		})
		return
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
	clientID := sanitizeClientID(req.ClientID)
	item, err := h.store.Save(key, req.Content, ttl, clientID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := model.ResponseFromClipboard(key, item, true)
	h.noteFullState(key, item.Version, item.UpdatedBy, item.Content)
	h.broker.ping(key)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteClipboard(w http.ResponseWriter, key string) {
	h.store.Delete(key)
	if h.files != nil {
		h.files.DeleteRoom(key)
	}
	h.noteFullState(key, model.VersionNotExists, "", "")
	h.broker.ping(key)
	w.WriteHeader(http.StatusNoContent)
}

// --- WebSocket realtime ------------------------------------------------------

type wsOutbound struct {
	Type string `json:"type"`
	// Content fields (type=state | ops | legacy update)
	Key        string      `json:"key,omitempty"`
	Content    string      `json:"content,omitempty"`
	TTLSeconds int64       `json:"ttlSeconds,omitempty"`
	ExpiresAt  string      `json:"expiresAt,omitempty"`
	Version    int64       `json:"version,omitempty"`
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
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request, key string) {
	clientID := sanitizeClientID(r.URL.Query().Get("clientId"))

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
	defer func() {
		close(send)
		<-writeDone
	}()

	// Initial snapshot.
	var (
		lastVersion  int64 = -1
		lastExists   bool
		lastFilesRev int64 = -1
	)
	lastVersion, lastExists, lastFilesRev = h.enqueueRoomState(send, key, clientID, lastVersion, lastExists, lastFilesRev, true)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		h.wsReadLoop(conn, send, key, clientID)
	}()

	pingTicker := time.NewTicker(wsPingPeriod)
	defer pingTicker.Stop()
	presenceTicker := time.NewTicker(wsPresencePeriod)
	defer presenceTicker.Stop()

	for {
		select {
		case <-readDone:
			return
		case <-writeDone:
			return
		case <-r.Context().Done():
			return
		case <-ch:
			lastVersion, lastExists, lastFilesRev = h.enqueueRoomState(send, key, clientID, lastVersion, lastExists, lastFilesRev, false)
		case <-pingTicker.C:
			select {
			case send <- wsControl{ping: true}:
			default:
			}
		case <-presenceTicker.C:
			// Push pruned presence so idle/left peers drop without waiting for edits.
			// Also re-check file list in case TTL expiry dropped files without a write path.
			lastVersion, lastExists, lastFilesRev = h.enqueueRoomState(send, key, clientID, lastVersion, lastExists, lastFilesRev, false)
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

func (h *Handler) wsReadLoop(conn *websocket.Conn, send chan<- any, key, clientID string) {
	for {
		var msg wsInbound
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "cursor":
			if clientID == "" {
				continue
			}
			if msg.CursorPos < 0 || msg.SelectionEnd < 0 {
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
			h.handleWSOps(send, key, clientID, msg)
		case "pong", "ping":
			// ignore app-level heartbeats; protocol pings handled by gorilla
		default:
			// ignore unknown
		}
	}
}

func (h *Handler) handleWSOps(send chan<- any, key, clientID string, msg wsInbound) {
	if len(msg.Ops) == 0 {
		return
	}
	var ttl time.Duration
	if msg.TTLSeconds > 0 {
		d, err := model.TTLFromSeconds(msg.TTLSeconds)
		if err != nil {
			return
		}
		ttl = d
	}
	item, err := h.store.ApplyOps(key, msg.Ops, ttl, clientID)
	if err != nil {
		if h.logger != nil {
			h.logger.Debug("ws ops rejected", "key", key, "clientId", clientID, "error", err)
		}
		// Capacity / validation failures: resync so the client stays consistent.
		cur, exists := h.store.Get(key)
		select {
		case send <- h.stateMessage(key, cur, exists):
		default:
		}
		return
	}
	h.noteOps(key, item.Version, item.UpdatedBy, msg.Ops, item.Content)
	h.broker.ping(key)
}

func (h *Handler) noteOps(key string, version int64, updatedBy string, ops []crdt.Op, content string) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	cp := make([]crdt.Op, len(ops))
	copy(cp, ops)
	h.lastEvent[key] = contentEvent{
		version:   version,
		updatedBy: updatedBy,
		ops:       cp,
		content:   content,
		full:      false,
	}
}

func (h *Handler) noteFullState(key string, version int64, updatedBy, content string) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	h.lastEvent[key] = contentEvent{
		version:   version,
		updatedBy: updatedBy,
		content:   content,
		full:      true,
	}
}

func (h *Handler) takeEvent(key string, version int64) (contentEvent, bool) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	ev, ok := h.lastEvent[key]
	if !ok || ev.version != version {
		return contentEvent{}, false
	}
	return ev, true
}

func (h *Handler) enqueueRoomState(send chan<- any, key, clientID string, lastVersion int64, lastExists bool, lastFilesRev int64, forceContent bool) (int64, bool, int64) {
	item, exists := h.store.Get(key)
	version := model.VersionNotExists
	if exists {
		version = item.Version
	}

	contentChanged := forceContent || exists != lastExists || version != lastVersion
	if contentChanged {
		var msg wsOutbound
		if forceContent || !exists {
			msg = h.stateMessage(key, item, exists)
		} else if ev, ok := h.takeEvent(key, version); ok && !ev.full && len(ev.ops) > 0 {
			msg = wsOutbound{
				Type:      "ops",
				Key:       key,
				Version:   version,
				UpdatedBy: ev.updatedBy,
				Ops:       ev.ops,
				Content:   ev.content,
			}
			if exists {
				existsTrue := true
				msg.Exists = &existsTrue
				msg.TTLSeconds = int64(item.TTL.Seconds())
				msg.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
			}
		} else {
			msg = h.stateMessage(key, item, exists)
		}
		select {
		case send <- msg:
		default:
			// Drop if client is too slow; next ping will retry.
		}
		lastVersion = version
		lastExists = exists
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
	return lastVersion, lastExists, lastFilesRev
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

func (h *Handler) stateMessage(key string, item model.Clipboard, exists bool) wsOutbound {
	msg := wsOutbound{Type: "state", Key: key}
	if exists {
		resp := model.ResponseFromClipboard(key, item, true)
		msg.Content = resp.Content
		msg.TTLSeconds = resp.TTLSeconds
		msg.ExpiresAt = resp.ExpiresAt
		msg.Version = resp.Version
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
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
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
