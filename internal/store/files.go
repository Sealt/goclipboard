package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"goclipboard/internal/model"
)

// DefaultFileDir is the default on-disk root for uploaded files.
const DefaultFileDir = "data/files"

// MaxFilePasswordLen caps per-file download passwords.
const MaxFilePasswordLen = 256

var (
	ErrFileNotFound    = errors.New("file not found")
	ErrUploadDisabled  = errors.New("file upload is disabled")
	ErrFileIO          = errors.New("file storage error")
	ErrBadFilePassword = errors.New("invalid file password")
)

// diskMeta is persisted next to each blob as {id}.meta.json.
type diskMeta struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int64  `json:"size"`
	TTLSeconds   int64  `json:"ttlSeconds"`
	ExpiresAt    string `json:"expiresAt"`
	UploadedAt   string `json:"uploadedAt"`
	PasswordSalt string `json:"passwordSalt,omitempty"`
	PasswordHash string `json:"passwordHash,omitempty"`
}

// fileRec is the in-memory index entry (metadata only; blob stays on disk).
type fileRec struct {
	ID           string
	Name         string
	ContentType  string
	Size         int64
	TTL          time.Duration
	ExpiresAt    time.Time
	UploadedAt   time.Time
	PasswordSalt string
	PasswordHash string
}

// roomSettings is persisted as {root}/{roomKey}/settings.json.
type roomSettings struct {
	FileUploadEnabled bool `json:"fileUploadEnabled"`
}

// FileStore persists temporary files under a directory tree:
//
//	{root}/{roomKey}/{fileID}.bin
//	{root}/{roomKey}/{fileID}.meta.json
//	{root}/{roomKey}/settings.json
//
// Password-gated single-user use: no per-file / per-room / total size caps.
// File upload is disabled per room until an admin enables it.
type FileStore struct {
	mu       sync.Mutex
	root     string
	rooms    map[string]map[string]*fileRec // room -> id -> meta
	settings map[string]roomSettings        // room -> settings (default: upload off)
	rev      map[string]int64
	now      func() time.Time
	onExpire func(roomKey string)
}

type FileOption func(*FileStore)

func WithFileClock(now func() time.Time) FileOption {
	return func(s *FileStore) { s.now = now }
}

func WithFileRoot(root string) FileOption {
	return func(s *FileStore) {
		if strings.TrimSpace(root) != "" {
			s.root = root
		}
	}
}

// NewFileStore creates a disk-backed file store. root defaults to DefaultFileDir.
// Existing files on disk are loaded into the index.
func NewFileStore(opts ...FileOption) *FileStore {
	s := &FileStore{
		root:     DefaultFileDir,
		rooms:    make(map[string]map[string]*fileRec),
		settings: make(map[string]roomSettings),
		rev:      make(map[string]int64),
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		// Defer hard failure to first write; keep constructor usable in tests.
		_ = err
	}
	_ = s.loadAllFromDisk()
	return s
}

func (s *FileStore) SetOnExpire(fn func(roomKey string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExpire = fn
}

// Root returns the storage directory.
func (s *FileStore) Root() string {
	return s.root
}

// IsFileUploadEnabled reports whether this room accepts new file uploads.
// Default is false (admin must enable per room).
func (s *FileStore) IsFileUploadEnabled(roomKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings[roomKey].FileUploadEnabled
}

// SetFileUploadEnabled toggles upload permission for a room and persists it.
// Bumps the room's file-list revision so WS clients resync.
// Default (never set) is disabled; writing false after enable keeps the explicit value on disk.
func (s *FileStore) SetFileUploadEnabled(roomKey string, enabled bool) error {
	if _, err := model.ValidateKey(roomKey); err != nil {
		return errors.New("invalid room key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, exists := s.settings[roomKey]
	if exists && cur.FileUploadEnabled == enabled {
		return nil
	}
	if !exists && !enabled {
		// Already the default (upload off) — nothing to persist.
		return nil
	}
	cur.FileUploadEnabled = enabled
	if err := s.writeSettingsLocked(roomKey, cur); err != nil {
		return err
	}
	s.settings[roomKey] = cur
	s.bumpRevLocked(roomKey)
	return nil
}

// RoomSettings returns the public settings view for a room.
func (s *FileStore) RoomSettings(roomKey string) model.RoomSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return model.RoomSettings{
		Key:               roomKey,
		FileUploadEnabled: s.settings[roomKey].FileUploadEnabled,
	}
}

// Put stores data from a byte slice (tests / small payloads).
// filePassword is required and is used later for downloads only.
func (s *FileStore) Put(roomKey, name, contentType string, data []byte, ttl time.Duration, filePassword string) (model.File, error) {
	return s.PutReader(roomKey, name, contentType, bytes.NewReader(data), int64(len(data)), ttl, filePassword)
}

// PutReader streams r to disk. sizeHint may be -1 if unknown.
// filePassword is required; only its salt+hash are stored on disk (never plaintext).
func (s *FileStore) PutReader(roomKey, name, contentType string, r io.Reader, sizeHint int64, ttl time.Duration, filePassword string) (model.File, error) {
	if r == nil {
		return model.File{}, model.ErrEmptyFile
	}
	filePassword = strings.TrimSpace(filePassword)
	if filePassword == "" {
		return model.File{}, errors.New("file password is required")
	}
	if len(filePassword) > MaxFilePasswordLen {
		return model.File{}, errors.New("file password is too long")
	}
	salt, hash, err := hashFilePassword(filePassword)
	if err != nil {
		return model.File{}, err
	}
	safeName, err := model.SanitizeFileName(name)
	if err != nil {
		return model.File{}, err
	}
	if ttl <= 0 {
		return model.File{}, errors.New("ttlSeconds must be greater than 0")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 128 {
		contentType = contentType[:128]
	}
	if _, err := model.ValidateKey(roomKey); err != nil {
		return model.File{}, errors.New("invalid room key")
	}

	id, err := randomFileID()
	if err != nil {
		return model.File{}, err
	}

	roomDir := s.roomDir(roomKey)
	if err := os.MkdirAll(roomDir, 0o755); err != nil {
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, err)
	}

	binPath := s.binPath(roomKey, id)
	metaPath := s.metaPath(roomKey, id)
	tmpBin := binPath + ".tmp"
	tmpMeta := metaPath + ".tmp"

	// Ensure partial files are cleaned up on failure.
	cleanup := func() {
		_ = os.Remove(tmpBin)
		_ = os.Remove(tmpMeta)
		_ = os.Remove(binPath)
		_ = os.Remove(metaPath)
	}

	f, err := os.OpenFile(tmpBin, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	written, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		cleanup()
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, copyErr)
	}
	if closeErr != nil {
		cleanup()
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, closeErr)
	}
	if written == 0 {
		cleanup()
		return model.File{}, model.ErrEmptyFile
	}
	_ = sizeHint // size measured from stream

	now := s.now()
	expires := now.Add(ttl)
	meta := diskMeta{
		ID:           id,
		Name:         safeName,
		ContentType:  contentType,
		Size:         written,
		TTLSeconds:   int64(ttl / time.Second),
		ExpiresAt:    expires.UTC().Format(time.RFC3339),
		UploadedAt:   now.UTC().Format(time.RFC3339),
		PasswordSalt: salt,
		PasswordHash: hash,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		cleanup()
		return model.File{}, err
	}
	if err := os.WriteFile(tmpMeta, metaBytes, 0o644); err != nil {
		cleanup()
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	if err := os.Rename(tmpBin, binPath); err != nil {
		cleanup()
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	if err := os.Rename(tmpMeta, metaPath); err != nil {
		cleanup()
		return model.File{}, fmt.Errorf("%w: %v", ErrFileIO, err)
	}

	rec := &fileRec{
		ID:           id,
		Name:         safeName,
		ContentType:  contentType,
		Size:         written,
		TTL:          ttl,
		ExpiresAt:    expires,
		UploadedAt:   now,
		PasswordSalt: salt,
		PasswordHash: hash,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireRoomLocked(roomKey, now)
	if s.rooms[roomKey] == nil {
		s.rooms[roomKey] = make(map[string]*fileRec)
	}
	s.rooms[roomKey][id] = rec
	s.bumpRevLocked(roomKey)
	return rec.toModel(nil), nil
}

// CheckFilePassword verifies the per-file download password.
// Returns ErrFileNotFound if missing/expired, ErrBadFilePassword if wrong.
func (s *FileStore) CheckFilePassword(roomKey, fileID, password string) error {
	s.mu.Lock()
	rec, ok := s.getLiveLocked(roomKey, fileID, s.now())
	s.mu.Unlock()
	if !ok {
		return ErrFileNotFound
	}
	if !verifyFilePassword(rec.PasswordSalt, rec.PasswordHash, password) {
		return ErrBadFilePassword
	}
	return nil
}

// List returns live file metadata for a room (newest first).
func (s *FileStore) List(roomKey string) []model.FileInfo {
	files, _ := s.ListWithRevision(roomKey)
	return files
}

// ListWithRevision returns metadata plus the room's file-list revision.
func (s *FileStore) ListWithRevision(roomKey string) ([]model.FileInfo, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.expireRoomLocked(roomKey, now)

	room := s.rooms[roomKey]
	rev := s.rev[roomKey]
	if len(room) == 0 {
		return []model.FileInfo{}, rev
	}
	out := make([]model.FileInfo, 0, len(room))
	for _, rec := range room {
		out = append(out, rec.toInfo())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UploadedAt > out[j].UploadedAt
	})
	return out, rev
}

// ListWithSettings returns files, revision, and whether upload is enabled for the room.
func (s *FileStore) ListWithSettings(roomKey string) ([]model.FileInfo, int64, bool) {
	files, rev := s.ListWithRevision(roomKey)
	return files, rev, s.IsFileUploadEnabled(roomKey)
}

// Revision returns the current file-list revision for a room.
func (s *FileStore) Revision(roomKey string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev[roomKey]
}

// Get returns metadata and blob bytes (loads file into memory).
func (s *FileStore) Get(roomKey, fileID string) (model.File, bool) {
	s.mu.Lock()
	rec, ok := s.getLiveLocked(roomKey, fileID, s.now())
	s.mu.Unlock()
	if !ok {
		return model.File{}, false
	}
	data, err := os.ReadFile(s.binPath(roomKey, fileID))
	if err != nil {
		return model.File{}, false
	}
	return rec.toModel(data), true
}

// Open returns a streaming reader for the blob. Caller must Close the reader.
func (s *FileStore) Open(roomKey, fileID string) (model.File, io.ReadCloser, error) {
	s.mu.Lock()
	rec, ok := s.getLiveLocked(roomKey, fileID, s.now())
	s.mu.Unlock()
	if !ok {
		return model.File{}, nil, ErrFileNotFound
	}
	f, err := os.Open(s.binPath(roomKey, fileID))
	if err != nil {
		return model.File{}, nil, fmt.Errorf("%w: %v", ErrFileNotFound, err)
	}
	return rec.toModel(nil), f, nil
}

// Delete removes a file from disk and the index.
func (s *FileStore) Delete(roomKey, fileID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	room := s.rooms[roomKey]
	if room == nil {
		return false
	}
	if _, ok := room[fileID]; !ok {
		return false
	}
	delete(room, fileID)
	if len(room) == 0 {
		delete(s.rooms, roomKey)
	}
	s.bumpRevLocked(roomKey)
	s.removeDiskFiles(roomKey, fileID)
	s.tryRemoveRoomDirLocked(roomKey)
	return true
}

// DeleteRoom removes all files and settings for a clipboard key.
func (s *FileStore) DeleteRoom(roomKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	had := len(s.rooms[roomKey]) > 0 || s.rev[roomKey] > 0 || s.settings[roomKey].FileUploadEnabled
	s.deleteRoomLocked(roomKey)
	if had {
		s.bumpRevLocked(roomKey)
	}
}

// CleanupExpired drops expired files across all rooms.
func (s *FileStore) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	var expiredRooms []string
	// Copy keys — expire may mutate map.
	keys := make([]string, 0, len(s.rooms))
	for key := range s.rooms {
		keys = append(keys, key)
	}
	for _, key := range keys {
		before := len(s.rooms[key])
		s.expireRoomLocked(key, now)
		after := 0
		if s.rooms[key] != nil {
			after = len(s.rooms[key])
		}
		if before > 0 && after == 0 && s.onExpire != nil {
			expiredRooms = append(expiredRooms, key)
		}
	}
	for _, key := range expiredRooms {
		if s.onExpire != nil {
			s.onExpire(key)
		}
	}
}

func (s *FileStore) StartCleanup(stop <-chan struct{}, interval time.Duration) {
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

// Stats returns room count with files, total bytes, and file count.
func (s *FileStore) Stats() (rooms int, totalBytes int64, fileCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, room := range s.rooms {
		if len(room) == 0 {
			continue
		}
		rooms++
		fileCount += len(room)
		for _, rec := range room {
			totalBytes += rec.Size
		}
	}
	return rooms, totalBytes, fileCount
}

func (s *FileStore) getLiveLocked(roomKey, fileID string, now time.Time) (*fileRec, bool) {
	s.expireRoomLocked(roomKey, now)
	room := s.rooms[roomKey]
	if room == nil {
		return nil, false
	}
	rec, ok := room[fileID]
	if !ok {
		return nil, false
	}
	return rec, true
}

func (s *FileStore) expireRoomLocked(roomKey string, now time.Time) {
	room := s.rooms[roomKey]
	if room == nil {
		return
	}
	removed := false
	for id, rec := range room {
		if !rec.ExpiresAt.After(now) {
			delete(room, id)
			s.removeDiskFiles(roomKey, id)
			removed = true
		}
	}
	if len(room) == 0 {
		delete(s.rooms, roomKey)
		s.tryRemoveRoomDirLocked(roomKey)
	}
	if removed {
		s.bumpRevLocked(roomKey)
	}
}

func (s *FileStore) deleteRoomLocked(roomKey string) {
	room := s.rooms[roomKey]
	if room != nil {
		for id := range room {
			s.removeDiskFiles(roomKey, id)
		}
	}
	delete(s.rooms, roomKey)
	delete(s.settings, roomKey)
	_ = os.RemoveAll(s.roomDir(roomKey))
}

func (s *FileStore) tryRemoveRoomDirLocked(roomKey string) {
	if len(s.rooms[roomKey]) > 0 {
		return
	}
	// Keep the directory when settings still grant/deny upload for this room.
	if _, ok := s.settings[roomKey]; ok {
		return
	}
	_ = os.Remove(s.roomDir(roomKey))
}

// sweepRoomDir removes leftovers from an interrupted upload: temp files
// (*.tmp) and blobs/metadata without their counterpart. Only files whose
// pair is missing are touched, so live uploads and valid rooms are safe.
func (s *FileStore) sweepRoomDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		full := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, ".tmp"):
			_ = os.Remove(full)
		case strings.HasSuffix(name, ".bin"):
			if _, err := os.Stat(filepath.Join(dir, strings.TrimSuffix(name, ".bin")+".meta.json")); err != nil {
				_ = os.Remove(full)
			}
		case strings.HasSuffix(name, ".meta.json"):
			if _, err := os.Stat(filepath.Join(dir, strings.TrimSuffix(name, ".meta.json")+".bin")); err != nil {
				_ = os.Remove(full)
			}
		}
	}
}

func (s *FileStore) settingsPath(roomKey string) string {
	return filepath.Join(s.roomDir(roomKey), "settings.json")
}

func (s *FileStore) writeSettingsLocked(roomKey string, st roomSettings) error {
	roomDir := s.roomDir(roomKey)
	if err := os.MkdirAll(roomDir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.settingsPath(roomKey) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	if err := os.Rename(tmp, s.settingsPath(roomKey)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: %v", ErrFileIO, err)
	}
	return nil
}

func (s *FileStore) loadSettingsFromDisk(roomKey string) {
	raw, err := os.ReadFile(s.settingsPath(roomKey))
	if err != nil {
		return
	}
	var st roomSettings
	if err := json.Unmarshal(raw, &st); err != nil {
		return
	}
	s.settings[roomKey] = st
}

func (s *FileStore) bumpRevLocked(roomKey string) {
	s.rev[roomKey]++
}

func (s *FileStore) removeDiskFiles(roomKey, fileID string) {
	_ = os.Remove(s.binPath(roomKey, fileID))
	_ = os.Remove(s.metaPath(roomKey, fileID))
	_ = os.Remove(s.binPath(roomKey, fileID) + ".tmp")
	_ = os.Remove(s.metaPath(roomKey, fileID) + ".tmp")
}

func (s *FileStore) roomDir(roomKey string) string {
	return filepath.Join(s.root, roomKey)
}

func (s *FileStore) binPath(roomKey, fileID string) string {
	return filepath.Join(s.roomDir(roomKey), fileID+".bin")
}

func (s *FileStore) metaPath(roomKey, fileID string) string {
	return filepath.Join(s.roomDir(roomKey), fileID+".meta.json")
}

func (s *FileStore) loadAllFromDisk() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	now := s.now()
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		roomKey := ent.Name()
		if _, err := model.ValidateKey(roomKey); err != nil {
			continue
		}
		s.loadRoomFromDisk(roomKey, now)
	}
	return nil
}

func (s *FileStore) loadRoomFromDisk(roomKey string, now time.Time) {
	dir := s.roomDir(roomKey)
	// A crash between the bin/meta rename pair in PutReader leaves orphan
	// blobs and temp files that are unrecoverable; sweep them at startup so
	// restarts never leak disk.
	s.sweepRoomDir(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	s.loadSettingsFromDisk(roomKey)
	room := make(map[string]*fileRec)
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(name, ".meta.json")
		if _, err := model.ValidateFileID(id); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var meta diskMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		// Prefer id from filename.
		meta.ID = id
		rec, err := meta.toRec()
		if err != nil {
			continue
		}
		// Drop if blob missing.
		if _, err := os.Stat(s.binPath(roomKey, id)); err != nil {
			_ = os.Remove(s.metaPath(roomKey, id))
			continue
		}
		if !rec.ExpiresAt.After(now) {
			s.removeDiskFiles(roomKey, id)
			continue
		}
		// Fix size from actual blob if needed.
		if rec.Size <= 0 {
			if fi, err := os.Stat(s.binPath(roomKey, id)); err == nil {
				rec.Size = fi.Size()
			}
		}
		room[id] = rec
	}
	if len(room) > 0 {
		s.rooms[roomKey] = room
	} else if _, hasSettings := s.settings[roomKey]; !hasSettings {
		_ = os.Remove(dir)
	}
}

func (m diskMeta) toRec() (*fileRec, error) {
	exp, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return nil, err
	}
	up, err := time.Parse(time.RFC3339, m.UploadedAt)
	if err != nil {
		up = exp
	}
	ttl := time.Duration(m.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Until(exp)
		if ttl < 0 {
			ttl = 0
		}
	}
	return &fileRec{
		ID:           m.ID,
		Name:         m.Name,
		ContentType:  m.ContentType,
		Size:         m.Size,
		TTL:          ttl,
		ExpiresAt:    exp,
		UploadedAt:   up,
		PasswordSalt: m.PasswordSalt,
		PasswordHash: m.PasswordHash,
	}, nil
}

func (r *fileRec) toInfo() model.FileInfo {
	return model.FileInfo{
		ID:          r.ID,
		Name:        r.Name,
		Size:        r.Size,
		ContentType: r.ContentType,
		UploadedAt:  r.UploadedAt.UTC().Format(time.RFC3339),
		ExpiresAt:   r.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func (r *fileRec) toModel(data []byte) model.File {
	return model.File{
		ID:          r.ID,
		Name:        r.Name,
		ContentType: r.ContentType,
		Data:        data,
		Size:        r.Size,
		TTL:         r.TTL,
		ExpiresAt:   r.ExpiresAt,
		UploadedAt:  r.UploadedAt,
	}
}

func randomFileID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func hashFilePassword(password string) (saltHex, hashHex string, err error) {
	return hashPassword(password)
}

func verifyFilePassword(saltHex, hashHex, password string) bool {
	if len(password) > MaxFilePasswordLen {
		return false
	}
	return verifyPassword(saltHex, hashHex, password)
}
