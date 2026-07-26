package store

import (
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
	ErrContentTooLarge = errors.New("content exceeds 1 MiB limit")
	ErrTooManyRooms    = errors.New("server at capacity: too many clipboards")
	ErrMemoryLimit     = errors.New("server at capacity: memory limit")
)

type Store struct {
	mu       sync.Mutex
	items    map[string]model.Clipboard
	sizeBy   map[string]int64
	total    int64
	maxRooms int
	maxTotal int64
	now      func() time.Time
	onExpire func(key string)
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
		items:    make(map[string]model.Clipboard),
		sizeBy:   make(map[string]int64),
		maxRooms: DefaultMaxRooms,
		maxTotal: DefaultMaxTotalBytes,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
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

	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return model.Clipboard{}, false
	}
	return cloneClipboard(item), true
}

// Save is a full document replace (LWW at document level).
// Identical content+TTL refreshes expiry without bumping version.
// updatedBy is used as the CRDT site when rebuilding the document.
func (s *Store) Save(key, content string, ttl time.Duration, updatedBy string) (model.Clipboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(content) > MaxContentBytes {
		return model.Clipboard{}, ErrContentTooLarge
	}

	now := s.now()
	current, exists := s.getLiveLocked(key, now)

	if exists && current.Content == content && current.TTL == ttl {
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

	version := int64(1)
	if exists {
		version = current.Version + 1
	}

	item := model.Clipboard{
		Doc:       doc,
		Content:   content,
		TTL:       ttl,
		ExpiresAt: now.Add(ttl),
		Version:   version,
		UpdatedAt: now,
		UpdatedBy: updatedBy,
	}
	s.putLocked(key, item, newBytes)
	return cloneClipboard(item), nil
}

// ApplyOps integrates a CRDT op batch into the room document.
// If ttl > 0, the room TTL is updated; otherwise the existing TTL is kept
// (new rooms require ttl > 0).
func (s *Store) ApplyOps(key string, ops []crdt.Op, ttl time.Duration, updatedBy string) (model.Clipboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	current, exists := s.getLiveLocked(key, now)

	var doc *crdt.Doc
	var version int64
	var curTTL time.Duration

	if exists {
		doc = current.Doc.Clone()
		version = current.Version
		curTTL = current.TTL
	} else {
		doc = crdt.NewDoc()
		version = 0
		if ttl <= 0 {
			return model.Clipboard{}, fmt.Errorf("ttlSeconds required for new clipboard")
		}
		curTTL = ttl
	}
	if ttl > 0 {
		curTTL = ttl
	}

	// Apply on a working copy so failures do not corrupt stored state.
	working := doc.Clone()
	changed, err := working.ApplyBatch(ops)
	if err != nil {
		return model.Clipboard{}, err
	}
	content := working.Materialize()
	if len(content) > MaxContentBytes {
		return model.Clipboard{}, ErrContentTooLarge
	}

	newBytes := estimateBytes(content, working.Len())
	oldBytes := int64(0)
	if exists {
		oldBytes = s.sizeBy[key]
	}

	if !changed {
		if !exists {
			return model.Clipboard{}, fmt.Errorf("no effect on empty clipboard")
		}
		// TTL refresh only — size unchanged.
		current.ExpiresAt = now.Add(curTTL)
		current.TTL = curTTL
		current.UpdatedAt = now
		if updatedBy != "" {
			current.UpdatedBy = updatedBy
		}
		s.items[key] = current
		return cloneClipboard(current), nil
	}

	if err := s.canAcceptLocked(!exists, oldBytes, newBytes); err != nil {
		return model.Clipboard{}, err
	}

	item := model.Clipboard{
		Doc:       working,
		Content:   content,
		TTL:       curTTL,
		ExpiresAt: now.Add(curTTL),
		Version:   version + 1,
		UpdatedAt: now,
		UpdatedBy: updatedBy,
	}
	s.putLocked(key, item, newBytes)
	return cloneClipboard(item), nil
}

func (s *Store) getLiveLocked(key string, now time.Time) (model.Clipboard, bool) {
	current, exists := s.items[key]
	if !exists {
		return model.Clipboard{}, false
	}
	if !current.ExpiresAt.After(now) {
		s.removeLocked(key)
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
}

func (s *Store) removeLocked(key string) {
	if old, ok := s.sizeBy[key]; ok {
		s.total -= old
		delete(s.sizeBy, key)
	}
	delete(s.items, key)
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
