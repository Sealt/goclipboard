package store

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"goclipboard/internal/crdt"
	"goclipboard/internal/model"
)

// Capacity / DoS defaults. Sized for a small VPS without starving normal use:
// thousands of short-lived rooms and multi-user editing of typical snippets.
const (
	DefaultMaxRooms      = 10_000
	DefaultMaxTotalBytes = 256 << 20 // 256 MiB estimated CRDT+content budget
	MaxContentBytes      = 1 << 20   // 1 MiB materialized content (matches HTTP body cap)

	// Conservative per-atom estimate (map entry + Item + id/after strings).
	// Overestimates slightly so the hard budget trips before RSS blows up.
	bytesPerCRDTItem = 192
	roomBaseBytes    = 256
)

var (
	ErrContentTooLarge  = errors.New("content exceeds 1 MiB limit")
	ErrTooManyRooms     = errors.New("server at capacity: too many clipboards")
	ErrMemoryLimit      = errors.New("server at capacity: memory limit")
	ErrVersionConflict  = errors.New("version conflict")
	ErrRoomNotFound     = errors.New("clipboard not found")
	ErrPasswordMismatch = errors.New("edit password mismatch")
)

// newViewKey returns a random read-only access key for a room.
func newViewKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return hex.EncodeToString(b[:])
}

type Store struct {
	mu             sync.Mutex
	items          map[string]model.Clipboard
	sizeBy         map[string]int64
	nextGeneration int64
	total          int64
	maxRooms       int
	maxTotal       int64
	now            func() time.Time
	onExpire       func(key string)

	// Optional disk persistence (see persist.go).
	persistDir     string
	dirty          map[string]bool
	deleted        map[string]bool
	persistStop    chan struct{}
	persistDone    chan struct{}
	persistRunning bool
}

type Option func(*Store)

func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

func WithOnExpire(fn func(key string)) Option {
	return func(s *Store) { s.onExpire = fn }
}

// WithLimits sets hard caps on live room count and estimated in-memory bytes.
// Zero or negative values keep the previous/default limit for that field.
func WithLimits(maxRooms int, maxTotalBytes int64) Option {
	return func(s *Store) {
		if maxRooms > 0 {
			s.maxRooms = maxRooms
		}
		if maxTotalBytes > 0 {
			s.maxTotal = maxTotalBytes
		}
	}
}

func New(opts ...Option) *Store {
	s := &Store{
		items:          make(map[string]model.Clipboard),
		sizeBy:         make(map[string]int64),
		nextGeneration: time.Now().UnixMicro(),
		maxRooms:       DefaultMaxRooms,
		maxTotal:       DefaultMaxTotalBytes,
		now:            time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.startPersister()
	return s
}

func (s *Store) SetOnExpire(fn func(key string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExpire = fn
}

// Stats returns live room count and estimated byte usage (for tests / ops).
func (s *Store) Stats() (rooms int, totalBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items), s.total
}

func (s *Store) Get(key string) (model.Clipboard, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	item, ok := s.getLiveLocked(key, now)
	if !ok {
		return model.Clipboard{}, false
	}
	return cloneClipboard(item), true
}

// Peek returns the live room without cloning its document. The caller must
// treat the result as read-only: stored documents are never mutated in
// place (updates replace the room wholesale), so sharing the reference is
// safe while other goroutines write. Use for hot read paths (WS state
// polling every few seconds per connection) where the defensive deep copy
// of Get would be pure waste.
func (s *Store) Peek(key string) (model.Clipboard, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return model.Clipboard{}, false
	}
	return item, true
}

// Save is a full document replace (LWW at document level).
// Identical content+TTL refreshes expiry without bumping version.
// updatedBy is used as the CRDT site when rebuilding the document.
func (s *Store) Save(key, content string, ttl time.Duration, updatedBy string) (model.Clipboard, error) {
	return s.SaveWithBase(key, content, ttl, updatedBy, 0)
}

// SaveWithBase is Save with optimistic concurrency: when baseVersion > 0 the
// replace only succeeds if the stored version still equals baseVersion,
// otherwise ErrVersionConflict is returned so the caller can merge and retry.
// baseVersion <= 0 keeps the unconditional LWW behavior.
func (s *Store) SaveWithBase(key, content string, ttl time.Duration, updatedBy string, baseVersion int64) (model.Clipboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(content) > MaxContentBytes {
		return model.Clipboard{}, ErrContentTooLarge
	}

	now := s.now()
	current, exists := s.getLiveLocked(key, now)

	if baseVersion > 0 {
		if !exists || current.Version != baseVersion {
			return model.Clipboard{}, ErrVersionConflict
		}
	}

	if exists && current.Content == content && current.TTL == ttl {
		if current.Generation <= 0 {
			current.Generation = s.newGenerationLocked()
		} else if current.Generation > s.nextGeneration {
			s.nextGeneration = current.Generation
		}
		current.ExpiresAt = now.Add(ttl)
		current.UpdatedAt = now
		if updatedBy != "" {
			current.UpdatedBy = updatedBy
		}
		s.items[key] = current
		return cloneClipboard(current), nil
	}

	site := updatedBy
	if site == "" {
		site = "server"
	}
	doc, err := crdt.BuildFromString(site, content)
	if err != nil {
		doc, _ = crdt.BuildFromString("server", content)
	}

	newBytes := estimateBytes(content, doc.Len())
	oldBytes := int64(0)
	if exists {
		oldBytes = s.sizeBy[key]
	}
	if err := s.canAcceptLocked(!exists, oldBytes, newBytes); err != nil {
		return model.Clipboard{}, err
	}

	generation := current.Generation
	if exists {
		generation = s.existingGenerationLocked(generation)
	} else {
		generation = s.newGenerationLocked()
	}

	version := int64(1)
	if exists {
		version = current.Version + 1
	}

	viewKey := current.ViewKey
	if !exists || viewKey == "" {
		viewKey = newViewKey()
	}

	item := model.Clipboard{
		Doc:           doc,
		Content:       content,
		TTL:           ttl,
		ExpiresAt:     now.Add(ttl),
		Version:       version,
		Generation:    generation,
		ViewKey:       viewKey,
		Password:      current.Password,
		PasswordScope: current.PasswordScope,
		UpdatedAt:     now,
		UpdatedBy:     updatedBy,
	}
	s.putLocked(key, item, newBytes)
	return cloneClipboard(item), nil
}

// ApplyOps integrates a CRDT op batch into the room document.
// If ttl > 0, the room TTL is updated; otherwise the existing TTL is kept
// (new rooms require ttl > 0). An empty batch is a TTL update/refresh.
// The bool result reports whether the batch changed document content
// (false for idempotent re-applies and TTL-only updates).
// A TTL change on unchanged content still bumps the version so WS
// subscribers rebroadcast the new expiry.
func (s *Store) ApplyOps(key string, ops []crdt.Op, ttl time.Duration, updatedBy string) (model.Clipboard, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	current, exists := s.getLiveLocked(key, now)

	var working *crdt.Doc
	var version int64
	var generation int64
	var curTTL time.Duration
	ttlChanged := false

	if exists {
		// Work on a single clone of the stored document so failures never
		// corrupt the committed state (and we clone only once per batch).
		working = current.Doc.Clone()
		version = current.Version
		generation = s.existingGenerationLocked(current.Generation)
		curTTL = current.TTL
	} else {
		working = crdt.NewDoc()
		version = 0
		if ttl <= 0 {
			return model.Clipboard{}, false, fmt.Errorf("ttlSeconds required for new clipboard")
		}
		generation = s.newGenerationLocked()
		curTTL = ttl
	}
	if ttl > 0 && ttl != curTTL {
		curTTL = ttl
		ttlChanged = true
	}

	changed := false
	if len(ops) > 0 {
		var err error
		changed, err = working.ApplyBatch(ops)
		if err != nil {
			return model.Clipboard{}, false, err
		}
	}
	content := working.Materialize()
	if len(content) > MaxContentBytes {
		return model.Clipboard{}, false, ErrContentTooLarge
	}

	newBytes := estimateBytes(content, working.Len())
	oldBytes := int64(0)
	if exists {
		oldBytes = s.sizeBy[key]
	}

	if !changed {
		if !exists {
			if len(ops) > 0 {
				return model.Clipboard{}, false, fmt.Errorf("no effect on empty clipboard")
			}
			// TTL-only touch on a missing room: create it empty.
			if err := s.canAcceptLocked(true, 0, newBytes); err != nil {
				return model.Clipboard{}, false, err
			}
			item := model.Clipboard{
				Doc:        working,
				Content:    content,
				TTL:        curTTL,
				ExpiresAt:  now.Add(curTTL),
				Version:    1,
				Generation: generation,
				ViewKey:    newViewKey(),
				UpdatedAt:  now,
				UpdatedBy:  updatedBy,
			}
			s.putLocked(key, item, newBytes)
			return cloneClipboard(item), false, nil
		}
		// TTL refresh / idempotent re-apply — size unchanged.
		current.ExpiresAt = now.Add(curTTL)
		current.TTL = curTTL
		current.Generation = generation
		current.UpdatedAt = now
		if ttlChanged {
			current.Version++
		}
		if updatedBy != "" {
			current.UpdatedBy = updatedBy
		}
		s.items[key] = current
		return cloneClipboard(current), false, nil
	}

	if err := s.canAcceptLocked(!exists, oldBytes, newBytes); err != nil {
		return model.Clipboard{}, false, err
	}

	viewKey := current.ViewKey
	if !exists || viewKey == "" {
		viewKey = newViewKey()
	}

	item := model.Clipboard{
		Doc:           working,
		Content:       content,
		TTL:           curTTL,
		ExpiresAt:     now.Add(curTTL),
		Version:       version + 1,
		Generation:    generation,
		ViewKey:       viewKey,
		Password:      current.Password,
		PasswordScope: current.PasswordScope,
		UpdatedAt:     now,
		UpdatedBy:     updatedBy,
	}
	s.putLocked(key, item, newBytes)
	return cloneClipboard(item), true, nil
}

// newGenerationLocked returns a process-wide monotonic room incarnation. The
// timestamp seed keeps a restarted process from immediately reusing an old
// client's generation, while staying exactly representable in JavaScript and
// avoiding an unbounded per-key tombstone map.
func (s *Store) newGenerationLocked() int64 {
	s.nextGeneration++
	if s.nextGeneration <= 0 {
		s.nextGeneration = 1
	}
	return s.nextGeneration
}

func (s *Store) existingGenerationLocked(generation int64) int64 {
	if generation <= 0 {
		return s.newGenerationLocked()
	}
	if generation > s.nextGeneration {
		s.nextGeneration = generation
	}
	return generation
}

func (s *Store) getLiveLocked(key string, now time.Time) (model.Clipboard, bool) {
	current, exists := s.items[key]
	if !exists {
		return model.Clipboard{}, false
	}
	if !current.ExpiresAt.After(now) {
		s.removeLocked(key)
		if s.onExpire != nil {
			// Keep the callback under the Store lock. Otherwise a new room with
			// the same key could be created before the old-room notification
			// clears its cached event.
			s.onExpire(key)
		}
		return model.Clipboard{}, false
	}
	if current.Doc == nil {
		// Legacy safety: rebuild doc from content cache.
		site := current.UpdatedBy
		if site == "" {
			site = "server"
		}
		doc, err := crdt.BuildFromString(site, current.Content)
		if err != nil {
			doc = crdt.NewDoc()
		}
		current.Doc = doc
		s.items[key] = current
		// Re-account if we previously had zero/unknown size.
		if s.sizeBy[key] == 0 {
			nb := estimateBytes(current.Content, doc.Len())
			s.total += nb
			s.sizeBy[key] = nb
		}
	}
	return current, true
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(key)
}

// HasPassword reports whether the room is protected (writes require the
// password; with scope "view", reads do too). A missing/expired room counts
// as unprotected.
func (s *Store) HasPassword(key string) bool {
	item, ok := s.Peek(key)
	return ok && item.Password != ""
}

// PasswordInfo reports whether the room is password-protected and what the
// password gates ("edit" | "view"). Missing/expired rooms count as
// unprotected; a protected room with an empty scope is normalized to "edit"
// (legacy rooms locked before scope support).
func (s *Store) PasswordInfo(key string) (set bool, scope string) {
	item, ok := s.Peek(key)
	if !ok || item.Password == "" {
		return false, ""
	}
	return true, model.PasswordScopeOf(item.PasswordScope)
}

// PasswordOK compares a presented password against the room's password in
// constant time. Unprotected or missing rooms accept any password.
func (s *Store) PasswordOK(key, password string) bool {
	item, ok := s.Peek(key)
	if !ok || item.Password == "" {
		return true
	}
	if len(password) != len(item.Password) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(item.Password)) == 1
}

// SetPassword locks, rotates or unlocks a room. When the room is already
// locked, current must match (ErrPasswordMismatch otherwise). An empty next
// value unlocks the room. scope decides what the password gates ("edit" or
// "view"); it is ignored when unlocking, kept on rotation when empty, and
// defaults to "edit" for a freshly locked room.
func (s *Store) SetPassword(key, current, next, scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return ErrRoomNotFound
	}
	if item.Password != "" {
		if len(current) != len(item.Password) ||
			subtle.ConstantTimeCompare([]byte(current), []byte(item.Password)) != 1 {
			return ErrPasswordMismatch
		}
	}
	if item.Password == next && item.PasswordScope == scope {
		return nil
	}
	item.Password = next
	if next == "" {
		item.PasswordScope = ""
	} else {
		if scope == "" {
			scope = item.PasswordScope
		}
		if scope == "" {
			scope = model.PasswordScopeEdit
		}
		item.PasswordScope = scope
	}
	s.items[key] = item
	s.markDirtyLocked(key)
	return nil
}

func (s *Store) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for key, item := range s.items {
		if !item.ExpiresAt.After(now) {
			s.removeLocked(key)
			if s.onExpire != nil {
				s.onExpire(key)
			}
		}
	}
}

func (s *Store) StartCleanup(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CleanupExpired()
		case <-stop:
			return
		}
	}
}

func (s *Store) canAcceptLocked(isNew bool, oldBytes, newBytes int64) error {
	if isNew && len(s.items) >= s.maxRooms {
		return ErrTooManyRooms
	}
	delta := newBytes - oldBytes
	if delta <= 0 {
		return nil
	}
	if s.total+delta > s.maxTotal {
		return ErrMemoryLimit
	}
	return nil
}

func (s *Store) putLocked(key string, item model.Clipboard, newBytes int64) {
	if old, ok := s.sizeBy[key]; ok {
		s.total -= old
	}
	s.items[key] = item
	s.sizeBy[key] = newBytes
	s.total += newBytes
	s.markDirtyLocked(key)
}

func (s *Store) removeLocked(key string) {
	if old, ok := s.sizeBy[key]; ok {
		s.total -= old
		delete(s.sizeBy, key)
	}
	delete(s.items, key)
	s.markDeletedLocked(key)
	if s.total < 0 {
		s.total = 0
	}
}

func estimateBytes(content string, itemCount int) int64 {
	if itemCount <= 0 {
		// Empty CRDT still has map overhead; use rune count as lower bound for Save.
		if n := utf8.RuneCountInString(content); n > 0 {
			itemCount = n
		}
	}
	return int64(roomBaseBytes) + int64(len(content)) + int64(itemCount)*bytesPerCRDTItem
}

func cloneClipboard(item model.Clipboard) model.Clipboard {
	out := item
	if item.Doc != nil {
		out.Doc = item.Doc.Clone()
	}
	return out
}
