package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

const (
	defaultTTL       = time.Hour
	cleanupInterval  = time.Minute
	maxRequestBytes  = 1 << 20
	versionNotExists = int64(0)
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type clipboard struct {
	Content   string
	TTL       time.Duration
	ExpiresAt time.Time
	Version   int64
	UpdatedAt time.Time
}

type store struct {
	mu    sync.Mutex
	items map[string]clipboard
	now   func() time.Time
}

func newStore() *store {
	return &store{
		items: make(map[string]clipboard),
		now:   time.Now,
	}
}

func (s *store) get(key string) (clipboard, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[key]
	if !ok {
		return clipboard{}, false
	}
	if !item.ExpiresAt.After(s.now()) {
		delete(s.items, key)
		return clipboard{}, false
	}
	return item, true
}

func (s *store) save(key, content string, ttl time.Duration) clipboard {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	version := int64(1)
	if current, ok := s.items[key]; ok {
		version = current.Version + 1
	}

	item := clipboard{
		Content:   content,
		TTL:       ttl,
		ExpiresAt: now.Add(ttl),
		Version:   version,
		UpdatedAt: now,
	}
	s.items[key] = item
	return item
}

func (s *store) delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

func (s *store) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for key, item := range s.items {
		if !item.ExpiresAt.After(now) {
			delete(s.items, key)
		}
	}
}

func (s *store) startCleanup(stop <-chan struct{}) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupExpired()
		case <-stop:
			return
		}
	}
}

type server struct {
	store *store
}

type clipboardResponse struct {
	Key        string `json:"key"`
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Version    int64  `json:"version"`
	Exists     bool   `json:"exists"`
}

type saveRequest struct {
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
}

func main() {
	addr := ":" + envOrDefault("PORT", "8080")
	app := &server{store: newStore()}

	stopCleanup := make(chan struct{})
	defer close(stopCleanup)
	go app.store.startCleanup(stopCleanup)

	log.Printf("temporary clipboard listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, app.routes()); err != nil {
		log.Fatal(err)
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/clipboard/", s.handleClipboardAPI)
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
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

	http.ServeFileFS(w, r, staticFiles, "static/index.html")
}

func (s *server) handleClipboardAPI(w http.ResponseWriter, r *http.Request) {
	key, err := keyFromAPIPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "clipboard not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetClipboard(w, key)
	case http.MethodPut:
		s.handleSaveClipboard(w, r, key)
	case http.MethodDelete:
		s.store.delete(key)
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *server) handleGetClipboard(w http.ResponseWriter, key string) {
	item, ok := s.store.get(key)
	if !ok {
		writeJSON(w, http.StatusOK, clipboardResponse{
			Key:        key,
			Content:    "",
			TTLSeconds: int64(defaultTTL.Seconds()),
			Version:    versionNotExists,
			Exists:     false,
		})
		return
	}

	writeJSON(w, http.StatusOK, responseFromClipboard(key, item, true))
}

func (s *server) handleSaveClipboard(w http.ResponseWriter, r *http.Request, key string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	var req saveRequest
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

	ttl, err := ttlFromSeconds(req.TTLSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	item := s.store.save(key, req.Content, ttl)
	writeJSON(w, http.StatusOK, responseFromClipboard(key, item, true))
}

func responseFromClipboard(key string, item clipboard, exists bool) clipboardResponse {
	return clipboardResponse{
		Key:        key,
		Content:    item.Content,
		TTLSeconds: int64(item.TTL.Seconds()),
		ExpiresAt:  item.ExpiresAt.UTC().Format(time.RFC3339),
		Version:    item.Version,
		Exists:     exists,
	}
}

func keyFromPagePath(path string) (string, error) {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/static/") {
		return "", errors.New("reserved path")
	}
	key := strings.TrimPrefix(path, "/")
	if strings.Contains(key, "/") {
		return "", errors.New("key must be a single path segment")
	}
	return validateKey(key)
}

func keyFromAPIPath(path string) (string, error) {
	key := strings.TrimPrefix(path, "/api/clipboard/")
	if key == path || key == "" || strings.Contains(key, "/") {
		return "", errors.New("invalid API path")
	}
	return validateKey(key)
}

func validateKey(key string) (string, error) {
	if !keyPattern.MatchString(key) || strings.EqualFold(key, "api") || strings.EqualFold(key, "static") {
		return "", errors.New("invalid key")
	}
	return key, nil
}

func ttlFromSeconds(seconds int64) (time.Duration, error) {
	if seconds <= 0 {
		return 0, errors.New("ttlSeconds must be greater than 0")
	}
	duration := time.Duration(seconds) * time.Second
	if int64(duration/time.Second) != seconds {
		return 0, errors.New("ttlSeconds is too large")
	}
	return duration, nil
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func randomKey() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}
