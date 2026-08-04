package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goclipboard/internal/crdt"
	"goclipboard/internal/model"
)

// Persistence keeps rooms across restarts as per-room JSON snapshots under
// {dir}/{key}.json. The server default (see main) is data/rooms; operators can
// still opt out with PERSIST_DIR=off. Snapshots are written debounced (~250ms
// after the last mutation) so a typing burst does not hit the disk once per op
// batch; a stale write is detected and retried.
//
// The snapshot stores the full CRDT item set, so restored rooms keep their
// exact structure (ids, tombstones) — peers reconnect and merge as
// if the server never restarted.

// DefaultPersistDir is the on-disk root used when PERSIST_DIR is unset.
// Mirrors DefaultFileDir (data/files → data/rooms).
const DefaultPersistDir = "data/rooms"

const persistFlushInterval = 250 * time.Millisecond

// roomSnapshot is the on-disk representation of one live room.
type roomSnapshot struct {
	Key           string `json:"key"`
	PasswordHash  string `json:"passwordHash,omitempty"`
	PasswordSalt  string `json:"passwordSalt,omitempty"`
	PasswordScope string `json:"passwordScope,omitempty"`
	// Password / EditPassword are legacy plaintext fields (pre-hash / pre-scope).
	// Migrated to salt+hash on load and never written back.
	Password     string               `json:"password,omitempty"`
	EditPassword string               `json:"editPassword,omitempty"`
	Content      string               `json:"content"`
	TTLSeconds   int64                `json:"ttlSeconds"`
	ExpiresAt    int64                `json:"expiresAt"` // unix seconds
	Version      int64                `json:"version"`
	Generation   int64                `json:"generation"`
	UpdatedAt    int64                `json:"updatedAt"` // unix seconds
	UpdatedBy    string               `json:"updatedBy,omitempty"`
	Items        []crdt.Item          `json:"items"`
	History      []model.HistoryEntry `json:"history,omitempty"`
}

// WithPersistence enables disk snapshots under dir. Empty dir keeps the store
// purely in-memory (no flusher, no restore).
func WithPersistence(dir string) Option {
	return func(s *Store) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		s.persistDir = dir
		s.dirty = make(map[string]bool)
		s.deleted = make(map[string]bool)
		s.persistStop = make(chan struct{})
		s.persistDone = make(chan struct{})
	}
}

func (s *Store) snapshotPath(key string) string {
	return filepath.Join(s.persistDir, key+".json")
}

// markDirtyLocked flags a room for snapshot write; markDeletedLocked flags a
// removed room so its snapshot file is deleted. Both run under s.mu.
func (s *Store) markDirtyLocked(key string) {
	if s.persistDir == "" {
		return
	}
	s.dirty[key] = true
}

func (s *Store) markDeletedLocked(key string) {
	if s.persistDir == "" {
		return
	}
	delete(s.dirty, key)
	s.deleted[key] = true
}

// startPersister launches the debounced writer goroutine. Safe to call once.
func (s *Store) startPersister() {
	if s.persistDir == "" || s.persistRunning {
		return
	}
	s.persistRunning = true
	if err := os.MkdirAll(s.persistDir, 0o700); err != nil {
		// Disk problem: keep serving from memory; snapshots fail silently.
		// Drop the persist dir so Close() takes the in-memory early-return
		// path instead of blocking forever on a writer that never started.
		s.persistDir = ""
		return
	}
	s.loadPersisted()
	go s.persistLoop()
}

func (s *Store) persistLoop() {
	ticker := time.NewTicker(persistFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flushDirty()
		case <-s.persistStop:
			s.flushDirty()
			close(s.persistDone)
			return
		}
	}
}

// Close flushes pending snapshots and stops the writer. Idempotent; safe on a
// store without persistence.
func (s *Store) Close() {
	if s.persistDir == "" {
		return
	}
	select {
	case <-s.persistDone:
		return
	default:
	}
	close(s.persistStop)
	<-s.persistDone
}

func (s *Store) flushDirty() {
	s.mu.Lock()
	if len(s.dirty) == 0 && len(s.deleted) == 0 {
		s.mu.Unlock()
		return
	}
	dirty := s.dirty
	deleted := s.deleted
	s.dirty = make(map[string]bool)
	s.deleted = make(map[string]bool)
	s.mu.Unlock()

	for key := range deleted {
		_ = os.Remove(s.snapshotPath(key))
	}
	for key := range dirty {
		s.writeSnapshot(key)
	}
}

func (s *Store) writeSnapshot(key string) {
	s.mu.Lock()
	item, ok := s.getLiveLocked(key, s.now())
	if !ok {
		s.mu.Unlock()
		_ = os.Remove(s.snapshotPath(key))
		return
	}
	snap := roomSnapshot{
		Key:           key,
		PasswordHash:  item.PasswordHash,
		PasswordSalt:  item.PasswordSalt,
		PasswordScope: item.PasswordScope,
		// Never write legacy plaintext fields (Password / EditPassword).
		Content:    item.Content,
		TTLSeconds: int64(item.TTL.Seconds()),
		ExpiresAt:  item.ExpiresAt.Unix(),
		Version:    item.Version,
		Generation: item.Generation,
		UpdatedAt:  item.UpdatedAt.Unix(),
		UpdatedBy:  item.UpdatedBy,
		History:    cloneHistory(item.History),
	}
	if item.Doc != nil {
		snap.Items = item.Doc.Items()
	}
	version, generation := item.Version, item.Generation
	passSalt := item.PasswordSalt
	histLen := len(item.History)
	s.mu.Unlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	path := s.snapshotPath(key)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return
	}

	// Re-check under the lock: a concurrent mutation makes this snapshot stale
	// (rewrite next tick); a concurrent removal must delete the file outright.
	// Password-only and history-only mutations may leave Version/Generation
	// unchanged if a future path forgets to bump them — also compare salt and
	// history length so those writes are not lost against a racing snapshot.
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.items[key]
	if !exists {
		_ = os.Remove(path)
		return
	}
	if cur.Version != version || cur.Generation != generation ||
		cur.PasswordSalt != passSalt || len(cur.History) != histLen {
		s.dirty[key] = true
	}
}

// loadPersisted restores snapshots from disk. Expired rooms are dropped (and
// their files removed); capacity limits are honored by skipping further loads.
func (s *Store) loadPersisted() {
	entries, err := os.ReadDir(s.persistDir)
	if err != nil {
		return
	}
	now := s.now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".json")
		if _, err := model.ValidateKey(key); err != nil {
			_ = os.Remove(filepath.Join(s.persistDir, e.Name()))
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.persistDir, e.Name()))
		if err != nil {
			continue
		}
		var snap roomSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		if snap.Key != key || snap.TTLSeconds <= 0 {
			continue
		}
		// Migrate legacy password fields into salt+hash.
		// 1) Pre-hash snapshots stored plaintext under password / editPassword.
		// 2) Pre-scope snapshots used editPassword with no scope → edit.
		passHash := snap.PasswordHash
		passSalt := snap.PasswordSalt
		migratedPlain := false
		if passHash == "" {
			plain := snap.Password
			if plain == "" {
				plain = snap.EditPassword
			}
			if plain != "" {
				salt, hash, err := hashPassword(plain)
				if err == nil {
					passSalt, passHash = salt, hash
					migratedPlain = true
				}
			}
		}
		scope := snap.PasswordScope
		if passHash != "" && scope == "" {
			scope = model.PasswordScopeEdit
		}
		expiresAt := time.Unix(snap.ExpiresAt, 0)
		if !expiresAt.After(now) {
			_ = os.Remove(filepath.Join(s.persistDir, e.Name()))
			continue
		}

		doc := crdt.NewDoc()
		if err := doc.FromItems(snap.Items); err != nil {
			// Corrupt item set — rebuild the linear chain from the cached text.
			site := snap.UpdatedBy
			if site == "" {
				site = "server"
			}
			doc, err = crdt.BuildFromString(site, snap.Content)
			if err != nil {
				continue
			}
		}

		s.mu.Lock()
		if len(s.items) >= s.maxRooms {
			s.mu.Unlock()
			break
		}
		hist := cloneHistory(snap.History)
		if len(hist) > MaxHistoryEntries {
			hist = append([]model.HistoryEntry(nil), hist[len(hist)-MaxHistoryEntries:]...)
		}
		newBytes := estimateBytes(snap.Content, doc.Len()) + historyBytes(hist)
		if s.total+newBytes > s.maxTotal {
			// Keep the file on disk; skip loading under the current budget.
			s.mu.Unlock()
			continue
		}
		item := model.Clipboard{
			Doc:           doc,
			Content:       snap.Content,
			TTL:           time.Duration(snap.TTLSeconds) * time.Second,
			ExpiresAt:     expiresAt,
			Version:       snap.Version,
			Generation:    snap.Generation,
			PasswordHash:  passHash,
			PasswordSalt:  passSalt,
			PasswordScope: scope,
			UpdatedAt:     time.Unix(snap.UpdatedAt, 0),
			UpdatedBy:     snap.UpdatedBy,
			History:       hist,
		}
		if item.Generation > s.nextGeneration {
			s.nextGeneration = item.Generation
		}
		s.items[key] = item
		s.sizeBy[key] = newBytes
		s.total += newBytes
		// Rewrite snapshot without plaintext as soon as the flusher runs.
		if migratedPlain {
			s.markDirtyLocked(key)
		}
		s.mu.Unlock()
	}
}

// RestoredRoomCount reports how many rooms were loaded from disk at startup
// (for logging); valid right after New() with persistence enabled.
func (s *Store) RestoredRoomCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
