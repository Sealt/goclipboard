package model

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"goclipboard/internal/crdt"
)

type Clipboard struct {
	Doc       *crdt.Doc
	Content   string
	TTL       time.Duration
	ExpiresAt time.Time
	Version   int64
	// Generation identifies the room incarnation. It increases when an
	// expired/deleted room is created again, so version 1 from a new room cannot
	// be mistaken for version 1 from an older incarnation.
	Generation int64
	// ViewKey is the read-only access key for the room, distinct from the
	// edit key (the room key in the URL path). It is generated once when the
	// room is created and never changes. View links are {key}?view=true
	// (legacy {key}?view={ViewKey} links are still honored); WebSocket
	// sessions presenting a valid view flag are read-only (ops are rejected
	// server-side). Note: the view URL contains the room key in its path, so
	// this guards against accidental edits, not against a determined holder
	// of the view link.
	ViewKey string
	// PasswordHash + PasswordSalt store a password KDF hash (bcrypt) and a
	// random salt used as a non-secret session credential (never the
	// plaintext). Legacy snapshots may still carry SHA-256(salt:password)
	// digests, which remain verifiable. PasswordScope decides what the
	// password gates: PasswordScopeEdit locks writes only (reads stay open —
	// legacy behavior); PasswordScopeView locks reads and writes. The
	// plaintext is chosen by the client that locks the room and never
	// returned in any response.
	PasswordHash  string
	PasswordSalt  string
	PasswordScope string // "" | "edit" | "view" ("" on a locked room = "edit")
	UpdatedAt     time.Time
	UpdatedBy     string
	// History is a server-side trail of content snapshots so any browser
	// can restore prior states. Newest is last; capped by the store.
	History []HistoryEntry
}

// HistoryEntry is one point-in-time content snapshot for a room.
type HistoryEntry struct {
	Text    string `json:"text"`
	Version int64  `json:"version"`
	// At is unix milliseconds (matches Date.now() on the client).
	At     int64  `json:"at"`
	By     string `json:"by,omitempty"`
	Manual bool   `json:"manual,omitempty"`
}

// Password scopes.
const (
	// PasswordScopeEdit gates writes only (reads stay open).
	PasswordScopeEdit = "edit"
	// PasswordScopeView gates reads and writes (the password is required to
	// view the content at all).
	PasswordScopeView = "view"
)

// PasswordScopeOf normalizes a stored scope. Legacy rooms (locked before
// scope support) have an empty scope, which means the password gates edits —
// the historical behavior.
func PasswordScopeOf(scope string) string {
	if scope == "" {
		return PasswordScopeEdit
	}
	return scope
}

type ClipboardResponse struct {
	Key        string `json:"key"`
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Version    int64  `json:"version"`
	Generation int64  `json:"generation,omitempty"`
	ViewKey    string `json:"viewKey,omitempty"`
	// EditPasswordSet reports whether the room is locked (never the password
	// itself, so it is safe for any reader of the room to see).
	EditPasswordSet bool `json:"editPasswordSet,omitempty"`
	// PasswordScope reports what the room password gates: "edit" (writes
	// only, legacy) or "view" (reads and writes). Empty when unlocked.
	PasswordScope string `json:"passwordScope,omitempty"`
	Exists        bool   `json:"exists"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
}

type SaveRequest struct {
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
	// Password is required when the room is locked with an edit password.
	// With SetPassword, it is also the secret used to claim-lock an unlocked room.
	Password string `json:"password,omitempty"`
	// SetPassword asks the server to claim-lock an unlocked room with Password
	// under the same write as the content (atomic create-and-lock). Ignored
	// when Password is empty or the room is already locked. Web clients leave
	// this false so a remembered password cannot re-lock after an unlock.
	SetPassword bool `json:"setPassword,omitempty"`
	// PasswordScope is used only when SetPassword is true: "edit" | "view".
	// Empty defaults to "edit".
	PasswordScope string `json:"passwordScope,omitempty"`
	// BaseVersion > 0 requests optimistic concurrency: the save is rejected
	// with 409 (plus current state) unless the stored version still matches,
	// so offline/REST clients can merge instead of blindly overwriting.
	// 0 keeps the legacy unconditional LWW replace.
	BaseVersion int64  `json:"baseVersion,omitempty"`
	ClientID    string `json:"clientId,omitempty"`
}

const VersionNotExists = int64(0)

// EditPasswordRequest sets, rotates or clears a room password.
// CurrentPassword must match when the room is already locked.
type EditPasswordRequest struct {
	Password        string `json:"password"`
	CurrentPassword string `json:"currentPassword,omitempty"`
	// Scope selects what the password gates: "edit" (writes only) or
	// "view" (reads and writes). Empty keeps the current scope (defaults
	// to "edit" for a freshly locked room).
	Scope string `json:"scope,omitempty"`
}

var KeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var reservedKeys = map[string]bool{
	"api":     true,
	"static":  true,
	"healthz": true,
}

func ValidateKey(key string) (string, error) {
	if !KeyPattern.MatchString(key) || reservedKeys[strings.ToLower(key)] {
		return "", errors.New("invalid key")
	}
	return key, nil
}

func TTLFromSeconds(seconds int64) (time.Duration, error) {
	if seconds <= 0 {
		return 0, errors.New("ttlSeconds must be greater than 0")
	}
	duration := time.Duration(seconds) * time.Second
	if int64(duration/time.Second) != seconds {
		return 0, errors.New("ttlSeconds is too large")
	}
	return duration, nil
}

type CursorUpdateRequest struct {
	ClientID     string `json:"clientId"`
	CursorPos    int    `json:"cursorPos"`
	SelectionEnd int    `json:"selectionEnd"`
	// AfterID is the CRDT item id immediately left of the caret (empty = doc start).
	AfterID          string `json:"afterId,omitempty"`
	SelectionAfterID string `json:"selectionAfterId,omitempty"`
	Color            string `json:"color"`
}

type CursorInfo struct {
	ClientID     string `json:"clientId"`
	CursorPos    int    `json:"cursorPos"`
	SelectionEnd int    `json:"selectionEnd"`
	// AfterID is the CRDT id left of the caret. Empty / omitted = document start
	// or "anchor not provided" (client treats missing field as no wire anchor).
	AfterID          string `json:"afterId,omitempty"`
	SelectionAfterID string `json:"selectionAfterId,omitempty"`
	Color            string `json:"color"`
	Timestamp        int64  `json:"timestamp"`
}

type CursorEvent struct {
	Cursors []CursorInfo `json:"cursors"`
}

// RoomPasswordSet reports whether the room has a lock (hash present).
func (item Clipboard) RoomPasswordSet() bool {
	return item.PasswordHash != ""
}

func ResponseFromClipboard(key string, item Clipboard, exists bool) ClipboardResponse {
	scope := ""
	if item.RoomPasswordSet() {
		scope = PasswordScopeOf(item.PasswordScope)
	}
	return ClipboardResponse{
		Key:             key,
		Content:         item.Content,
		TTLSeconds:      int64(item.TTL.Seconds()),
		ExpiresAt:       item.ExpiresAt.UTC().Format(time.RFC3339),
		Version:         item.Version,
		Generation:      item.Generation,
		ViewKey:         item.ViewKey,
		EditPasswordSet: item.RoomPasswordSet(),
		PasswordScope:   scope,
		Exists:          exists,
		UpdatedBy:       item.UpdatedBy,
	}
}
