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

// Persistence (optional) keeps rooms across restarts as per-room JSON
// snapshots under {dir}/{key}.json. It is off by default (PERSIST_DIR unset):
// the service stays ephemeral unless the operator opts in. Snapshots are
// written debounced (~250ms after the last mutation) so a typing burst does
// not hit the disk once per op batch; a stale write is detected and retried.
//
// The snapshot stores the full CRDT item set, so restored rooms keep their
// exact structure (ids, tombstones, view keys) — peers reconnect and merge as
// if the server never restarted.

const persistFlushInterval = 250 * time.Millisecond

// roomSnapshot is the on-disk representation of one live room.
type roomSnapshot struct {
	Key          string      `json:"key"`
	ViewKey      string      `json:"viewKey,omitempty"`
	Password     string      `json:"password,omitempty"`
	PasswordScope string     `json:"passwordScope,omitempty"`
	// EditPassword is the legacy pre-scope field; migrated to Password on load.
	EditPassword string      `json:"editPassword,omitempty"`
	Content      string      `json:"content"`
	TTLSeconds int64       `json:"ttlSeconds"`
	ExpiresAt  int64       `json:"expiresAt"` // unix seconds
	Version    int64       `json:"version"`
	Generation int64       `json:"generation"`
	UpdatedAt  int64       `json:"updatedAt"` // unix seconds
	UpdatedBy  string      `json:"updatedBy,omitempty"`
	Items      []crdt.Item `json:"items"`
}

// WithPersistence enables disk snapshots under dir. Empty dir keeps the store
// purely in-memory (the default).
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
		ViewKey:       item.ViewKey,
		Password:      item.Password,
		PasswordScope: item.PasswordScope,
		Content:       item.Content,
		TTLSeconds: int64(item.TTL.Seconds()),
		ExpiresAt:  item.ExpiresAt.Unix(),
		Version:    item.Version,
		Generation: item.Generation,
		UpdatedAt:  item.UpdatedAt.Unix(),
		UpdatedBy:  item.UpdatedBy,
	}
	if item.Doc != nil {
		snap.Items = item.Doc.Items()
	}
	version, generation := item.Version, item.Generation
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
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.items[key]
	if !exists {
		_ = os.Remove(path)
		return
	}
	if cur.Version != version || cur.Generation != generation {
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
		// Legacy snapshots store the password under editPassword without a
		// scope; treat those as edit-scope (historical behavior).
		if snap.Password == "" {
			snap.Password = snap.EditPassword
		}
		if snap.Password != "" && snap.PasswordScope == "" {
			snap.PasswordScope = model.PasswordScopeEdit
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
		newBytes := estimateBytes(snap.Content, doc.Len())
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
			ViewKey:       snap.ViewKey,
			Password:      snap.Password,
			PasswordScope: snap.PasswordScope,
			UpdatedAt:     time.Unix(snap.UpdatedAt, 0),
			UpdatedBy:     snap.UpdatedBy,
		}
		if item.Generation > s.nextGeneration {
			s.nextGeneration = item.Generation
		}
		s.items[key] = item
		s.sizeBy[key] = newBytes
		s.total += newBytes
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
