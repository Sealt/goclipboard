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
	UpdatedAt time.Time
	UpdatedBy string
}

type ClipboardResponse struct {
	Key        string `json:"key"`
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Version    int64  `json:"version"`
	Exists     bool   `json:"exists"`
	UpdatedBy  string `json:"updatedBy,omitempty"`
}

type SaveRequest struct {
	Content    string `json:"content"`
	TTLSeconds int64  `json:"ttlSeconds"`
	// BaseVersion > 0 requests optimistic concurrency: the save is rejected
	// with 409 (plus current state) unless the stored version still matches,
	// so offline/REST clients can merge instead of blindly overwriting.
	// 0 keeps the legacy unconditional LWW replace.
	BaseVersion int64 `json:"baseVersion,omitempty"`
	ClientID    string `json:"clientId,omitempty"`
}

const VersionNotExists = int64(0)

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
	ClientID         string `json:"clientId"`
	CursorPos        int    `json:"cursorPos"`
	SelectionEnd     int    `json:"selectionEnd"`
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

func ResponseFromClipboard(key string, item Clipboard, exists bool) ClipboardResponse {
	return ClipboardResponse{
		Key:        key,
		Content:    item.Content,
		TTLSeconds: int64(item.TTL.Seconds()),
		ExpiresAt:  item.ExpiresAt.UTC().Format(time.RFC3339),
		Version:    item.Version,
		Exists:     exists,
		UpdatedBy:  item.UpdatedBy,
	}
}
