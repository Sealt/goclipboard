package store

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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
	// Version-history trail (server-side, shared across browsers).
	MaxHistoryEntries    = 20
	HistoryThrottle      = 5 * time.Second
	historyEntryOverhead = 64 // rough per-entry metadata allowance

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

// Auth carries credentials for password-protected room mutations.
// Password is the presented room secret; Cred is a previously proven
// PasswordCredential (salt) from AuthCredential / a WS auth session.
// A zero Auth is only accepted when the room is unlocked (or missing).
//
// ClaimPassword, when non-empty, claim-locks an unlocked room under the same
// store lock as the write (atomic create-and-lock for CLI push -password).
// It is ignored when the room is already locked. ClaimScope is "edit" or
// "view" (default "edit").
type Auth struct {
	Password      string
	Cred          string
	ClaimPassword string
	ClaimScope    string
}

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
// Optional auth gates writes on locked rooms (same rules as SaveWithBase).
func (s *Store) Save(key, content string, ttl time.Duration, updatedBy string, auth ...Auth) (model.Clipboard, error) {
	a := Auth{}
	if len(auth) > 0 {
		a = auth[0]
	}
	return s.SaveWithBase(key, content, ttl, updatedBy, 0, a)
}

// SaveWithBase is Save with optimistic concurrency: when baseVersion > 0 the
// replace only succeeds if the stored version still equals baseVersion,
// otherwise ErrVersionConflict is returned so the caller can merge and retry.
// baseVersion <= 0 keeps the unconditional LWW behavior.
//
// auth is checked under the same lock as the write so a concurrent lock or
// rotate cannot race past a check performed only in the HTTP handler.
func (s *Store) SaveWithBase(key, content string, ttl time.Duration, updatedBy string, baseVersion int64, auth Auth) (model.Clipboard, error) {
	// Pre-verify outside the store lock so a slow KDF (bcrypt) does not block
	// every other room. Under the lock we only re-confirm the credential.
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return model.Clipboard{}, err
	}

	// Pre-hash claim password outside s.mu (bcrypt is tens of ms).
	claimSalt, claimHash, claimScope, err := prepareClaim(auth)
	if err != nil {
		return model.Clipboard{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(content) > MaxContentBytes {
		return model.Clipboard{}, ErrContentTooLarge
	}

	now := s.now()
	current, exists := s.getLiveLocked(key, now)
	if err := confirmAuthLocked(current, exists, auth, expectedSalt); err != nil {
		return model.Clipboard{}, err
	}

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
		// Same content: still apply claim-lock so CLI push -password on an
		// existing unlocked room is atomic (no unlocked window with content).
		if claimHash != "" && !current.RoomPasswordSet() {
			current.PasswordSalt = claimSalt
			current.PasswordHash = claimHash
			current.PasswordScope = claimScope
			current.Version++
			s.markDirtyLocked(key)
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
		PasswordHash:  current.PasswordHash,
		PasswordSalt:  current.PasswordSalt,
		PasswordScope: current.PasswordScope,
		UpdatedAt:     now,
		UpdatedBy:     updatedBy,
		History:       cloneHistory(current.History),
	}
	// Atomic claim-lock: content + password land in one put. Already-locked
	// rooms keep their existing hash (claim is claim-only, not rotate).
	if claimHash != "" && !item.RoomPasswordSet() {
		item.PasswordSalt = claimSalt
		item.PasswordHash = claimHash
		item.PasswordScope = claimScope
	}
	s.maybeCaptureHistoryLocked(&item, false)

	newBytes := estimateBytes(item.Content, doc.Len()) + historyBytes(item.History)
	oldBytes := int64(0)
	if exists {
		oldBytes = s.sizeBy[key]
	}
	if err := s.canAcceptLocked(!exists, oldBytes, newBytes); err != nil {
		return model.Clipboard{}, err
	}

	s.putLocked(key, item, newBytes)
	return cloneClipboard(item), nil
}

// prepareClaim pre-computes a password hash for Auth.ClaimPassword outside
// the store lock. Empty claim yields empty hash (no-op).
func prepareClaim(auth Auth) (salt, hash, scope string, err error) {
	pw := strings.TrimSpace(auth.ClaimPassword)
	if pw == "" {
		return "", "", "", nil
	}
	salt, hash, err = hashPassword(pw)
	if err != nil {
		return "", "", "", err
	}
	scope = strings.TrimSpace(auth.ClaimScope)
	if scope == "" {
		scope = model.PasswordScopeEdit
	}
	if scope != model.PasswordScopeEdit && scope != model.PasswordScopeView {
		scope = model.PasswordScopeEdit
	}
	return salt, hash, scope, nil
}

// ApplyOps integrates a CRDT op batch into the room document.
// If ttl > 0, the room TTL is updated; otherwise the existing TTL is kept
// (new rooms require ttl > 0). An empty batch is a TTL update/refresh.
// The bool result reports whether the batch changed document content
// (false for idempotent re-applies and TTL-only updates).
// A TTL change on unchanged content still bumps the version so WS
// subscribers rebroadcast the new expiry.
//
// auth is checked under the same lock as the write (see SaveWithBase).
func (s *Store) ApplyOps(key string, ops []crdt.Op, ttl time.Duration, updatedBy string, auth Auth) (model.Clipboard, bool, error) {
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return model.Clipboard{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	current, exists := s.getLiveLocked(key, now)
	if err := confirmAuthLocked(current, exists, auth, expectedSalt); err != nil {
		return model.Clipboard{}, false, err
	}

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

	oldBytes := int64(0)
	if exists {
		oldBytes = s.sizeBy[key]
	}

	if !changed {
		newBytes := estimateBytes(content, working.Len()) + historyBytes(current.History)
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
		// TTL refresh / idempotent re-apply — document size unchanged.
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
		if ttlChanged {
			s.markDirtyLocked(key)
		}
		return cloneClipboard(current), false, nil
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
		PasswordHash:  current.PasswordHash,
		PasswordSalt:  current.PasswordSalt,
		PasswordScope: current.PasswordScope,
		UpdatedAt:     now,
		UpdatedBy:     updatedBy,
		History:       cloneHistory(current.History),
	}
	s.maybeCaptureHistoryLocked(&item, false)
	newBytes := estimateBytes(item.Content, working.Len()) + historyBytes(item.History)
	if err := s.canAcceptLocked(!exists, oldBytes, newBytes); err != nil {
		return model.Clipboard{}, false, err
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
			nb := estimateBytes(current.Content, doc.Len()) + historyBytes(current.History)
			s.total += nb
			s.sizeBy[key] = nb
		}
	}
	return current, true
}

// Delete removes a room without password checks. Prefer DeleteAuth for
// user-facing paths so a concurrent lock cannot be bypassed.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(key)
}

// DeleteAuth removes a room after verifying auth under the same lock.
func (s *Store) DeleteAuth(key string, auth Auth) error {
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if err := confirmAuthLocked(item, ok, auth, expectedSalt); err != nil {
		return err
	}
	s.removeLocked(key)
	return nil
}

// preAuthorize verifies auth against a peeked room state outside s.mu so a
// slow KDF does not hold the global store lock. It returns the PasswordSalt
// that was verified (empty when the room appeared unlocked/missing).
func (s *Store) preAuthorize(key string, auth Auth) (expectedSalt string, err error) {
	item, ok := s.Peek(key)
	if !ok || !item.RoomPasswordSet() {
		return "", nil
	}
	if auth.Cred != "" && secureStringEqual(auth.Cred, item.PasswordSalt) {
		return item.PasswordSalt, nil
	}
	if verifyPassword(item.PasswordSalt, item.PasswordHash, auth.Password) {
		return item.PasswordSalt, nil
	}
	return "", ErrPasswordMismatch
}

// confirmAuthLocked re-checks authorization under s.mu so a lock/rotate that
// landed between preAuthorize and the write cannot be bypassed.
func confirmAuthLocked(current model.Clipboard, exists bool, auth Auth, expectedSalt string) error {
	if !exists || !current.RoomPasswordSet() {
		// Unlocked (or missing): claim-lock semantics allow the write. A
		// concurrent SetPassword may still land after this write returns.
		return nil
	}
	if auth.Cred != "" && secureStringEqual(auth.Cred, current.PasswordSalt) {
		return nil
	}
	if expectedSalt != "" && secureStringEqual(expectedSalt, current.PasswordSalt) {
		return nil
	}
	// Room locked after precheck, or password rotated — re-verify (rare path).
	if verifyPassword(current.PasswordSalt, current.PasswordHash, auth.Password) {
		return nil
	}
	return ErrPasswordMismatch
}

// secureStringEqual compares two strings in constant time when lengths match.
func secureStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// HasPassword reports whether the room is protected (writes require the
// password; with scope "view", reads do too). A missing/expired room counts
// as unprotected.
func (s *Store) HasPassword(key string) bool {
	item, ok := s.Peek(key)
	return ok && item.RoomPasswordSet()
}

// PasswordInfo reports whether the room is password-protected and what the
// password gates ("edit" | "view"). Missing/expired rooms count as
// unprotected; a protected room with an empty scope is normalized to "edit"
// (legacy rooms locked before scope support).
func (s *Store) PasswordInfo(key string) (set bool, scope string) {
	item, ok := s.Peek(key)
	if !ok || !item.RoomPasswordSet() {
		return false, ""
	}
	return true, model.PasswordScopeOf(item.PasswordScope)
}

// PasswordOK verifies a presented password against the stored salt+hash.
// Unprotected or missing rooms accept any password.
func (s *Store) PasswordOK(key, password string) bool {
	item, ok := s.Peek(key)
	if !ok || !item.RoomPasswordSet() {
		return true
	}
	return verifyPassword(item.PasswordSalt, item.PasswordHash, password)
}

// PasswordCredential returns a non-secret token for the room's current
// password. Empty means unlocked or missing. The token changes whenever the
// password is set or rotated (new random salt), so long-lived sessions can
// detect that a prior auth is no longer valid.
func (s *Store) PasswordCredential(key string) string {
	item, ok := s.Peek(key)
	if !ok || !item.RoomPasswordSet() {
		return ""
	}
	return item.PasswordSalt
}

// AuthCredential verifies a presented password and returns the room's current
// credential token in one atomic step (a single lock acquisition). Calling
// PasswordOK followed by PasswordCredential would be two separate lock
// acquisitions: a password rotation landing between them would hand the
// caller a credential that does not match the password it just verified,
// letting a stale session re-auth against a password it never presented.
func (s *Store) AuthCredential(key, password string) (cred string, ok bool) {
	item, ok := s.Peek(key)
	if !ok || !item.RoomPasswordSet() {
		// Unprotected or missing rooms accept any password; there is no
		// credential to bind to.
		return "", true
	}
	if !verifyPassword(item.PasswordSalt, item.PasswordHash, password) {
		return "", false
	}
	return item.PasswordSalt, true
}

// SetPassword locks, rotates or unlocks a room. When the room is already
// locked, current must match (ErrPasswordMismatch otherwise). An empty next
// value unlocks the room. scope decides what the password gates ("edit" or
// "view"); it is ignored when unlocking, kept on rotation when empty, and
// defaults to "edit" for a freshly locked room.
//
// Only a password KDF hash is stored — never the plaintext. An unlocked room
// can be locked by any caller that knows the room key (claim semantics: set a
// password before sharing if that is a concern). Password mutations bump the
// room version so connected peers receive a state frame with the new lock
// flags even though content is unchanged.
func (s *Store) SetPassword(key, current, next, scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return ErrRoomNotFound
	}
	// Trim like the verify side (verifyPassword trims presented input), so a
	// direct store caller cannot set a password that its own check rejects.
	// A whitespace-only value therefore unlocks, matching the handler.
	next = strings.TrimSpace(next)
	if item.RoomPasswordSet() {
		if !verifyPassword(item.PasswordSalt, item.PasswordHash, current) {
			return ErrPasswordMismatch
		}
	}
	if next == "" {
		if !item.RoomPasswordSet() {
			return nil
		}
		item.PasswordHash = ""
		item.PasswordSalt = ""
		item.PasswordScope = ""
		item.Version++
		item.UpdatedAt = s.now()
		s.items[key] = item
		s.markDirtyLocked(key)
		return nil
	}
	if scope == "" {
		scope = item.PasswordScope
	}
	if scope == "" {
		scope = model.PasswordScopeEdit
	}
	// Same password: keep salt/hash; only rewrite when scope changes.
	if item.RoomPasswordSet() && verifyPassword(item.PasswordSalt, item.PasswordHash, next) {
		if model.PasswordScopeOf(item.PasswordScope) == model.PasswordScopeOf(scope) {
			return nil
		}
		item.PasswordScope = scope
		item.Version++
		item.UpdatedAt = s.now()
		s.items[key] = item
		s.markDirtyLocked(key)
		return nil
	}
	salt, hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	item.PasswordSalt = salt
	item.PasswordHash = hash
	item.PasswordScope = scope
	item.Version++
	item.UpdatedAt = s.now()
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

func historyBytes(history []model.HistoryEntry) int64 {
	var n int64
	for i := range history {
		n += int64(len(history[i].Text)) + historyEntryOverhead
	}
	return n
}

func cloneHistory(in []model.HistoryEntry) []model.HistoryEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.HistoryEntry, len(in))
	copy(out, in)
	return out
}

// maybeCaptureHistoryLocked appends a content snapshot when the text is new
// and (for automatic captures) the throttle window has elapsed. Manual
// captures skip the throttle. Empty content is never captured (auto or manual).
// Returns true when a new entry was appended.
func (s *Store) maybeCaptureHistoryLocked(item *model.Clipboard, manual bool) bool {
	if item == nil {
		return false
	}
	text := item.Content
	if text == "" {
		return false
	}
	nowMs := s.now().UnixMilli()
	if n := len(item.History); n > 0 {
		last := item.History[n-1]
		if last.Text == text {
			return false
		}
		if !manual && nowMs-last.At < HistoryThrottle.Milliseconds() {
			return false
		}
	}
	item.History = append(item.History, model.HistoryEntry{
		Text:    text,
		Version: item.Version,
		At:      nowMs,
		By:      item.UpdatedBy,
		Manual:  manual,
	})
	if len(item.History) > MaxHistoryEntries {
		item.History = append([]model.HistoryEntry(nil), item.History[len(item.History)-MaxHistoryEntries:]...)
	}
	return true
}

// History returns a copy of the room's version trail (oldest first).
// Protected rooms require auth (history retains prior content).
func (s *Store) History(key string, auth Auth) ([]model.HistoryEntry, bool, error) {
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return nil, false, nil
	}
	if err := confirmAuthLocked(item, true, auth, expectedSalt); err != nil {
		return nil, false, err
	}
	return cloneHistory(item.History), true, nil
}

// CaptureHistory force-snapshots the current room content (manual archive).
// Returns the updated trail (oldest first). No-ops (same text as last entry,
// or empty content) leave the room untouched and do not mark it dirty.
func (s *Store) CaptureHistory(key string, auth Auth) ([]model.HistoryEntry, error) {
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return nil, ErrRoomNotFound
	}
	if err := confirmAuthLocked(item, true, auth, expectedSalt); err != nil {
		return nil, err
	}
	item.History = cloneHistory(item.History)
	if !s.maybeCaptureHistoryLocked(&item, true) {
		return cloneHistory(item.History), nil
	}
	docLen := 0
	if item.Doc != nil {
		docLen = item.Doc.Len()
	}
	newBytes := estimateBytes(item.Content, docLen) + historyBytes(item.History)
	oldBytes := s.sizeBy[key]
	if err := s.canAcceptLocked(false, oldBytes, newBytes); err != nil {
		return nil, err
	}
	s.putLocked(key, item, newBytes)
	return cloneHistory(item.History), nil
}

// ClearHistory drops the room's version trail and frees its budget. Missing
// rooms return ErrRoomNotFound; an already-empty trail is a no-op success.
func (s *Store) ClearHistory(key string, auth Auth) error {
	expectedSalt, err := s.preAuthorize(key, auth)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		return ErrRoomNotFound
	}
	if err := confirmAuthLocked(item, true, auth, expectedSalt); err != nil {
		return err
	}
	if len(item.History) == 0 {
		return nil
	}
	item.History = nil
	docLen := 0
	if item.Doc != nil {
		docLen = item.Doc.Len()
	}
	newBytes := estimateBytes(item.Content, docLen)
	s.putLocked(key, item, newBytes)
	return nil
}

func cloneClipboard(item model.Clipboard) model.Clipboard {
	out := item
	if item.Doc != nil {
		out.Doc = item.Doc.Clone()
	}
	out.History = cloneHistory(item.History)
	return out
}
